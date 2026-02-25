package orchestrator

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	goshared "github.com/KranzL/shipmates-oss/go-shared"
)

const (
	containerTimeoutMs          = 10 * 60 * 1000
	stallThresholdMs            = 5000
	clarificationMaxWaitMs      = 60 * 60 * 1000
	clarificationWarningTimeMs  = 50 * 60 * 1000
	rollingBufferLines          = 20
	maxClarificationsPerSession = 5
	maxOutput                   = 4000
	throttleMs                  = 4000
	maxLogChars                 = 800
	bufferSize                  = 64 * 1024
)

var (
	envKeyMap = map[string]string{
		"anthropic": "ANTHROPIC_API_KEY",
		"openai":    "OPENAI_API_KEY",
		"google":    "GEMINI_API_KEY",
		"venice":    "OPENAI_API_KEY",
		"groq":      "GROQ_API_KEY",
		"together":  "TOGETHER_API_KEY",
		"fireworks": "FIREWORKS_API_KEY",
		"custom":    "OPENAI_API_KEY",
	}

	chatOnlyProviders = map[string]bool{
		"venice":    true,
		"groq":      true,
		"together":  true,
		"fireworks": true,
		"custom":    true,
	}

	interactiveProviders = map[string]bool{
		"anthropic": true,
		"google":    true,
	}

	providerCLIMap = map[string]string{
		"anthropic": "claude",
		"openai":    "codex",
		"google":    "gemini",
	}

	providerImageMap = map[string]string{
		"anthropic": "shipmates-claude:latest",
		"openai":    "shipmates-codex:latest",
		"google":    "shipmates-gemini:latest",
	}

	repoPattern   = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*/[a-zA-Z0-9][a-zA-Z0-9._-]*$`)
	branchPattern = regexp.MustCompile(`^[a-zA-Z0-9_.\-/]+$`)
)

type outputBuffer struct {
	data []byte
	max  int
}

func newOutputBuffer(max int) *outputBuffer {
	return &outputBuffer{
		data: make([]byte, 0, max*2),
		max:  max,
	}
}

func (b *outputBuffer) Write(p []byte) (int, error) {
	b.data = append(b.data, p...)
	if len(b.data) > b.max*2 {
		b.data = b.data[len(b.data)-b.max:]
	}
	return len(p), nil
}

func (b *outputBuffer) String() string {
	if len(b.data) > b.max {
		return string(b.data[len(b.data)-b.max:])
	}
	return string(b.data)
}

type Orchestrator struct {
	db            *goshared.DB
	dockerClient  *client.Client
	registry      *SessionRegistry
	cancelWatcher context.CancelFunc
	relayURL      string
	relaySecret   string
}

type WorkRequest struct {
	UserID         string
	AgentID        string
	Task           string
	Repo           string
	Provider       string
	APIKey         string
	GithubToken    string
	AgentScope     interface{}
	BaseURL        string
	ModelID        string
	PlatformUserID string
	Mode           string
	LogFn          func(ctx context.Context, msg string) (string, error)
}

func NewOrchestrator(db *goshared.DB) (*Orchestrator, error) {
	dockerClient, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("create docker client: %w", err)
	}

	relayURL := os.Getenv("RELAY_URL")
	if relayURL == "" {
		relayURL = "http://localhost:8080"
	}

	relaySecret := os.Getenv("RELAY_SECRET")
	if relaySecret == "" {
		return nil, fmt.Errorf("RELAY_SECRET environment variable is required")
	}

	o := &Orchestrator{
		db:           db,
		dockerClient: dockerClient,
		registry:     NewSessionRegistry(),
		relayURL:     relayURL,
		relaySecret:  relaySecret,
	}

	ctx, cancel := context.WithCancel(context.Background())
	o.cancelWatcher = cancel
	go o.runCancelWatcher(ctx)

	if err := o.ReconcileStuckSessions(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "[orchestrator] failed to reconcile stuck sessions: %v\n", err)
	}

	return o, nil
}

func (o *Orchestrator) Close() {
	if o.cancelWatcher != nil {
		o.cancelWatcher()
	}
}

func validateRepoName(repo string) error {
	if len(repo) > 200 {
		return fmt.Errorf("invalid repository name: %s", repo)
	}
	if !repoPattern.MatchString(repo) {
		return fmt.Errorf("invalid repository name: %s", repo)
	}
	if strings.Contains(repo, "..") {
		return fmt.Errorf("invalid repository name: %s", repo)
	}
	parts := strings.Split(repo, "/")
	if len(parts) != 2 {
		return fmt.Errorf("invalid repository name: %s", repo)
	}
	for _, part := range parts {
		if strings.HasPrefix(part, "-") || strings.HasPrefix(part, ".") || strings.HasSuffix(part, "-") || strings.HasSuffix(part, ".") {
			return fmt.Errorf("invalid repository name: %s", repo)
		}
	}
	return nil
}

func validateBranchName(branch string) error {
	if !branchPattern.MatchString(branch) {
		return fmt.Errorf("invalid branch name: %s", branch)
	}
	return nil
}

func (o *Orchestrator) ReconcileStuckSessions(ctx context.Context) error {
	cutoffTime := time.Now().Add(-30 * time.Second)
	rows, err := o.db.QueryContext(ctx, `
		SELECT id FROM sessions
		WHERE status IN ($1, $2)
		AND updated_at < $3
	`, goshared.SessionStatusRunning, goshared.SessionStatusWaitingForClarification, cutoffTime)
	if err != nil {
		return fmt.Errorf("query stuck sessions: %w", err)
	}
	defer rows.Close()

	stuckIDs := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("scan stuck session id: %w", err)
		}
		if _, exists := o.registry.Get(id); !exists {
			stuckIDs = append(stuckIDs, id)
		}
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("stuck sessions rows: %w", err)
	}

	if len(stuckIDs) == 0 {
		return nil
	}

	output := "Bot restarted, session lost."
	for _, id := range stuckIDs {
		if err := o.db.UpdateSessionResult(ctx, id, goshared.SessionStatusError, &output, nil, nil); err != nil {
			fmt.Fprintf(os.Stderr, "[orchestrator] failed to mark session %s as error: %v\n", id, err)
		}

		oneHourAgo := time.Now().Add(-1 * time.Hour)
		if _, err := o.db.ExpireStaleClarifications(ctx, oneHourAgo); err != nil {
			fmt.Fprintf(os.Stderr, "[orchestrator] failed to expire clarifications for session %s: %v\n", id, err)
		}
	}

	fmt.Printf("[orchestrator] Reconciled %d stuck sessions\n", len(stuckIDs))
	return nil
}

func (o *Orchestrator) runCancelWatcher(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	expiryTicker := time.NewTicker(5 * time.Minute)
	defer expiryTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case <-ticker.C:
			sessionIDs := o.registry.ActiveSessionIDs()
			if len(sessionIDs) == 0 {
				continue
			}

			queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			cancellingSessions, err := o.db.GetSessionsByStatus(queryCtx, sessionIDs, goshared.SessionStatusCancelling)
			cancel()

			if err != nil {
				continue
			}

			for _, sessionID := range cancellingSessions {
				containerID, ok := o.registry.Get(sessionID)
				if !ok {
					continue
				}

				if err := o.dockerClient.ContainerKill(ctx, containerID, "SIGKILL"); err != nil {
					fmt.Fprintf(os.Stderr, "[orchestrator] failed to kill container %s: %v\n", containerID, err)
				}
			}

		case <-expiryTicker.C:
			oneHourAgo := time.Now().Add(-1 * time.Hour)
			expired, err := o.db.ExpireStaleClarifications(ctx, oneHourAgo)
			if err != nil {
				fmt.Fprintf(os.Stderr, "[orchestrator] failed to expire clarifications: %v\n", err)
			}
			if len(expired) > 0 {
				fmt.Printf("[orchestrator] Expired %d stale clarifications\n", len(expired))
			}
		}
	}
}

func (o *Orchestrator) ExecuteInContainer(ctx context.Context, req WorkRequest) error {
	logFn := req.LogFn
	if logFn == nil {
		logFn = func(ctx context.Context, msg string) (string, error) {
			fmt.Printf("[orchestrator] %s\n", msg)
			return "", nil
		}
	}

	if err := validateRepoName(req.Repo); err != nil {
		logFn(ctx, "Invalid repository name. Skipping task.")
		return err
	}

	if chatOnlyProviders[req.Provider] {
		logFn(ctx, "This provider does not support container execution.")
		return fmt.Errorf("provider %s is chat-only", req.Provider)
	}

	image, imageExists := providerImageMap[req.Provider]
	if !imageExists {
		logFn(ctx, "Unknown provider. No container image available.")
		return fmt.Errorf("no image mapping for provider: %s", req.Provider)
	}

	sanitizedTask := SanitizeTask(req.Task)

	session := &goshared.Session{
		UserID:         req.UserID,
		AgentID:        req.AgentID,
		Task:           sanitizedTask,
		Status:         goshared.SessionStatusRunning,
		ExecutionMode:  goshared.ExecutionModeContainer,
		PlatformUserID: &req.PlatformUserID,
	}
	repoStr := req.Repo
	session.Repo = &repoStr
	now := time.Now()
	session.StartedAt = &now

	session, err := o.db.CreateSession(ctx, session)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}

	relay := NewRelayConnection(session.ID, o.relayURL, o.relaySecret)
	defer relay.Close()

	logFn(ctx, fmt.Sprintf("Started working on: %s", sanitizedTask))

	envKey := envKeyMap[req.Provider]
	cliCmd := providerCLIMap[req.Provider]
	isInteractive := interactiveProviders[req.Provider]

	branchName := fmt.Sprintf("shipmates/%s", session.ID)
	if err := validateBranchName(branchName); err != nil {
		relay.PushStatus("error")
		o.db.UpdateSessionResult(ctx, session.ID, goshared.SessionStatusError, ptrStr(err.Error()), nil, nil)
		logFn(ctx, "Invalid branch name. Skipping task.")
		return err
	}

	var scriptBuilder strings.Builder
	scriptBuilder.Grow(1024)
	fmt.Fprintf(&scriptBuilder, `printenv %s > /run/secrets/api_key && `, envKey)
	scriptBuilder.WriteString(`printenv GITHUB_TOKEN > /run/secrets/github_token && `)
	scriptBuilder.WriteString(`chmod 600 /run/secrets/api_key /run/secrets/github_token && `)
	fmt.Fprintf(&scriptBuilder, `export %s=$(cat /run/secrets/api_key) && `, envKey)
	scriptBuilder.WriteString(`export GITHUB_TOKEN=$(cat /run/secrets/github_token) && `)
	scriptBuilder.WriteString(`git config --global credential.helper '!f() { echo "password=$(cat /run/secrets/github_token)"; echo "username=x-access-token"; }; f' && `)
	scriptBuilder.WriteString(`git clone --depth 1 "https://github.com/'$REPO'.git" . && `)
	scriptBuilder.WriteString(`git checkout -b '$BRANCH_NAME' && `)
	fmt.Fprintf(&scriptBuilder, `%s -p "$TASK" && `, cliCmd)
	scriptBuilder.WriteString(`git add -A && `)
	scriptBuilder.WriteString(`git commit -m "shipmates: $TASK" && `)
	scriptBuilder.WriteString(`git push origin '$BRANCH_NAME'`)

	containerConfig := &container.Config{
		Image:      image,
		WorkingDir: "/workspace",
		Entrypoint: []string{"/bin/sh", "-c"},
		Cmd:        []string{scriptBuilder.String()},
		Env: []string{
			fmt.Sprintf("%s=%s", envKey, req.APIKey),
			fmt.Sprintf("GITHUB_TOKEN=%s", req.GithubToken),
			fmt.Sprintf("BRANCH_NAME=%s", branchName),
			fmt.Sprintf("REPO=%s", req.Repo),
			fmt.Sprintf("TASK=%s", sanitizedTask),
		},
	}

	if isInteractive {
		containerConfig.OpenStdin = true
		containerConfig.StdinOnce = false
		containerConfig.AttachStdin = true
		containerConfig.AttachStdout = true
		containerConfig.AttachStderr = true
		containerConfig.Tty = false
	}

	memoryLimit := int64(512 * 1024 * 1024)
	hostConfig := &container.HostConfig{
		AutoRemove:  true,
		NetworkMode: "shipmates-agent",
		Resources: container.Resources{
			Memory:     memoryLimit,
			MemorySwap: memoryLimit,
			CPUShares:  256,
			PidsLimit:  ptrInt64(256),
		},
		SecurityOpt: []string{"no-new-privileges"},
		CapDrop:     []string{"ALL"},
		CapAdd:      []string{"CHOWN", "DAC_OVERRIDE", "FOWNER"},
		Tmpfs:       map[string]string{"/run/secrets": "rw,noexec,nosuid,size=1m"},
	}

	createResp, err := o.dockerClient.ContainerCreate(ctx, containerConfig, hostConfig, nil, nil, "")
	if err != nil {
		relay.PushStatus("error")
		o.db.UpdateSessionResult(ctx, session.ID, goshared.SessionStatusError, ptrStr(err.Error()), nil, nil)
		logFn(ctx, "Failed to create container. Check server logs for details.")
		return fmt.Errorf("create container: %w", err)
	}

	containerID := createResp.ID
	o.registry.Register(session.ID, containerID)
	defer o.registry.Deregister(session.ID)

	if err := o.dockerClient.ContainerStart(ctx, containerID, container.StartOptions{}); err != nil {
		relay.PushStatus("error")
		o.db.UpdateSessionResult(ctx, session.ID, goshared.SessionStatusError, ptrStr(err.Error()), nil, nil)
		logFn(ctx, "Failed to start container. Check server logs for details.")
		return fmt.Errorf("start container: %w", err)
	}

	outputBuf := newOutputBuffer(maxOutput)
	var lineBuffer strings.Builder
	lastLogTime := time.Now()

	rollingLines := NewRollingBuffer(rollingBufferLines)
	timer := NewContainerTimer(containerTimeoutMs)
	defer timer.Cancel()

	go func() {
		<-timer.Done()
		o.dockerClient.ContainerKill(context.Background(), containerID, "SIGKILL")
	}()

	if isInteractive {
		o.handleInteractiveContainer(ctx, session, containerID, relay, outputBuf, &lineBuffer, &lastLogTime, rollingLines, timer, logFn)
	} else {
		o.handleNonInteractiveContainer(ctx, session, containerID, relay, outputBuf, &lineBuffer, &lastLogTime, logFn)
	}

	statusChan, errChan := o.dockerClient.ContainerWait(ctx, containerID, container.WaitConditionNotRunning)
	var exitCode int64
	select {
	case result := <-statusChan:
		exitCode = result.StatusCode
	case err := <-errChan:
		relay.PushStatus("error")
		o.db.UpdateSessionResult(ctx, session.ID, goshared.SessionStatusError, ptrStr(err.Error()), nil, nil)
		logFn(ctx, "Container wait error. Check server logs for details.")
		return fmt.Errorf("container wait: %w", err)
	}

	timer.Cancel()

	output := outputBuf.String()

	currentSession, _ := o.db.GetSessionByID(ctx, session.ID)
	if currentSession != nil && (currentSession.Status == goshared.SessionStatusCancelling || currentSession.Status == goshared.SessionStatusWaitingForClarification) {
		finalStatus := goshared.SessionStatusCancelled
		if currentSession.Status == goshared.SessionStatusWaitingForClarification {
			finalStatus = goshared.SessionStatusError
		}

		outputSlice := output
		if len(outputSlice) > 2000 {
			outputSlice = outputSlice[len(outputSlice)-2000:]
		}
		updated, err := o.db.UpdateSessionResultIfNotTerminal(ctx, session.ID, finalStatus, &outputSlice, nil, nil)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[orchestrator] failed to update session result: %v\n", err)
		}

		if updated {
			if finalStatus == goshared.SessionStatusCancelled {
				logFn(ctx, "Task was cancelled.")
			} else {
				logFn(ctx, "Agent timed out waiting for clarification.")
			}
		}
		return nil
	}

	if exitCode == 0 {
		prURL := fmt.Sprintf("https://github.com/%s/compare/%s", req.Repo, branchName)
		outputSlice := output
		if len(outputSlice) > 2000 {
			outputSlice = outputSlice[len(outputSlice)-2000:]
		}
		updated, err := o.db.UpdateSessionResultIfNotTerminal(ctx, session.ID, goshared.SessionStatusDone, &outputSlice, &branchName, &prURL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[orchestrator] failed to update session result: %v\n", err)
		}

		if updated {
			summary := output
			if len(summary) > 1500 {
				summary = summary[len(summary)-1500:]
			}
			logFn(ctx, fmt.Sprintf("Finished the task. Branch: `%s`\n```\n%s\n```", branchName, summary))
		}
	} else {
		outputSlice := output
		if len(outputSlice) > 2000 {
			outputSlice = outputSlice[len(outputSlice)-2000:]
		}
		updated, err := o.db.UpdateSessionResultIfNotTerminal(ctx, session.ID, goshared.SessionStatusError, &outputSlice, nil, nil)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[orchestrator] failed to update session result: %v\n", err)
		}

		if updated {
			logFn(ctx, "Hit an error while working on the task. Check logs for details.")
		}
	}

	return nil
}

func (o *Orchestrator) handleNonInteractiveContainer(
	ctx context.Context,
	session *goshared.Session,
	containerID string,
	relay *RelayConnection,
	outputBuf *outputBuffer,
	lineBuffer *strings.Builder,
	lastLogTime *time.Time,
	logFn func(ctx context.Context, msg string) (string, error),
) {
	logReader, err := o.dockerClient.ContainerLogs(ctx, containerID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "[orchestrator] failed to get container logs: %v\n", err)
		return
	}
	defer logReader.Close()

	bufferedReader := bufio.NewReaderSize(logReader, bufferSize)
	buf := make([]byte, 8192)
	for {
		n, err := bufferedReader.Read(buf)
		if n > 0 {
			text := string(buf[:n])
			sanitized := SanitizeOutput(text)

			outputBuf.Write([]byte(sanitized))
			relay.Push(sanitized)

			lineBuffer.WriteString(sanitized)
			now := time.Now()
			if now.Sub(*lastLogTime) >= throttleMs*time.Millisecond {
				lastNewline := strings.LastIndex(lineBuffer.String(), "\n")
				if lastNewline >= 0 {
					currentContent := lineBuffer.String()
					toSend := currentContent[:lastNewline+1]
					if len(toSend) > maxLogChars {
						toSend = toSend[len(toSend)-maxLogChars:]
					}
					logFn(ctx, fmt.Sprintf("```\n%s\n```", toSend))
					lineBuffer.Reset()
					lineBuffer.WriteString(currentContent[lastNewline+1:])
					*lastLogTime = now
				}
			}
		}

		if err != nil {
			if err != io.EOF {
				fmt.Fprintf(os.Stderr, "[orchestrator] log read error: %v\n", err)
			}
			break
		}
	}
}

func (o *Orchestrator) handleInteractiveContainer(
	ctx context.Context,
	session *goshared.Session,
	containerID string,
	relay *RelayConnection,
	outputBuf *outputBuffer,
	lineBuffer *strings.Builder,
	lastLogTime *time.Time,
	rollingLines *RollingBuffer,
	timer *ContainerTimer,
	logFn func(ctx context.Context, msg string) (string, error),
) {
	hijackedResp, err := o.dockerClient.ContainerAttach(ctx, containerID, container.AttachOptions{
		Stream: true,
		Stdin:  true,
		Stdout: true,
		Stderr: true,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "[orchestrator] failed to attach to container: %v\n", err)
		return
	}
	defer hijackedResp.Close()

	var wg sync.WaitGroup
	wg.Add(2)

	lastOutputTime := time.Now()
	var lastOutputMu sync.Mutex
	clarificationPending := false
	clarificationCount := 0
	done := make(chan struct{})

	go func() {
		defer wg.Done()
		bufferedReader := bufio.NewReaderSize(hijackedResp.Reader, bufferSize)
		buf := make([]byte, 8192)
		for {
			n, err := bufferedReader.Read(buf)
			if n > 0 {
				text := string(buf[:n])
				sanitized := SanitizeOutput(text)

				outputBuf.Write([]byte(sanitized))
				relay.Push(sanitized)
				lastOutputMu.Lock()
				lastOutputTime = time.Now()
				lastOutputMu.Unlock()

				lines := strings.Split(sanitized, "\n")
				for _, line := range lines {
					if len(line) > 0 {
						rollingLines.Push(line)
					}
				}

				lineBuffer.WriteString(sanitized)
				now := time.Now()
				if now.Sub(*lastLogTime) >= throttleMs*time.Millisecond {
					lastNewline := strings.LastIndex(lineBuffer.String(), "\n")
					if lastNewline >= 0 {
						currentContent := lineBuffer.String()
						toSend := currentContent[:lastNewline+1]
						if len(toSend) > maxLogChars {
							toSend = toSend[len(toSend)-maxLogChars:]
						}
						logFn(ctx, fmt.Sprintf("```\n%s\n```", toSend))
						lineBuffer.Reset()
						lineBuffer.WriteString(currentContent[lastNewline+1:])
						*lastLogTime = now
					}
				}
			}

			if err != nil {
				if err != io.EOF {
					fmt.Fprintf(os.Stderr, "[orchestrator] attach read error: %v\n", err)
				}
				break
			}
		}
		close(done)
	}()

	go func() {
		defer wg.Done()
		ticker := time.NewTicker(time.Duration(stallThresholdMs) * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case <-ticker.C:
				if clarificationPending || clarificationCount >= maxClarificationsPerSession {
					continue
				}

				lastOutputMu.Lock()
				stallDuration := time.Since(lastOutputTime)
				lastOutputMu.Unlock()

				if stallDuration < stallThresholdMs*time.Millisecond {
					continue
				}

				strippedLines := make([]string, 0)
				for _, line := range rollingLines.ToArray() {
					strippedLines = append(strippedLines, StripANSI(line))
				}

				if !LooksLikeQuestion(strippedLines) {
					continue
				}

				clarificationPending = true
				clarificationCount++

				questionText := ExtractQuestion(strippedLines)
				clarification, err := o.db.CreateClarification(ctx, session.ID, questionText)
				if err != nil {
					fmt.Fprintf(os.Stderr, "[orchestrator] failed to create clarification: %v\n", err)
					clarificationPending = false
					continue
				}

				mention := ""
				if session.PlatformUserID != nil && *session.PlatformUserID != "" {
					mention = fmt.Sprintf("<@%s> ", *session.PlatformUserID)
				}

				threadID, _ := logFn(ctx, fmt.Sprintf("%sAgent has a question:\n```\n%s\n```", mention, questionText))
				if threadID != "" {
					o.db.UpdateSessionPlatformThreadID(ctx, session.ID, threadID)
				}

				relay.Push(fmt.Sprintf("\n--- Agent is waiting for clarification ---\n%s\n", questionText))
				relay.PushStatus("waiting_for_clarification")
				timer.Pause()

				answered := o.awaitClarificationAnswer(ctx, clarification.ID, hijackedResp.Conn, logFn)
				if answered {
					timer.Resume()
					clarificationPending = false
					rollingLines.Clear()
					lastOutputMu.Lock()
					lastOutputTime = time.Now()
					lastOutputMu.Unlock()
					relay.PushStatus("running")
					logFn(ctx, "Got your answer, passing it to the agent.")
				} else {
					o.dockerClient.ContainerKill(context.Background(), containerID, "SIGKILL")
					return
				}
			}
		}
	}()

	wg.Wait()
}

func (o *Orchestrator) awaitClarificationAnswer(
	ctx context.Context,
	clarificationID string,
	stdinConn io.Writer,
	logFn func(ctx context.Context, msg string) (string, error),
) bool {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	deadline := time.Now().Add(clarificationMaxWaitMs * time.Millisecond)
	warningTime := time.Now().Add(clarificationWarningTimeMs * time.Millisecond)
	warningSent := false

	for {
		select {
		case <-ctx.Done():
			return false

		case <-ticker.C:
			now := time.Now()

			if !warningSent && now.After(warningTime) {
				logFn(ctx, "The agent is still waiting for your answer. 10 minutes remaining before timeout.")
				warningSent = true
			}

			if now.After(deadline) {
				o.db.UpdateClarificationStatus(ctx, clarificationID, goshared.ClarificationStatusExpired)
				return false
			}

			clarification, err := o.db.GetClarification(ctx, clarificationID)
			if err != nil {
				return false
			}

			if clarification == nil {
				return false
			}

			if clarification.Status == goshared.ClarificationStatusAnswered && clarification.Answer != nil {
				sanitizedAnswer := strings.ReplaceAll(*clarification.Answer, "\r\n", " ")
				sanitizedAnswer = strings.ReplaceAll(sanitizedAnswer, "\n", " ")
				sanitizedAnswer = controlPattern.ReplaceAllString(sanitizedAnswer, "")
				if len(sanitizedAnswer) > 2000 {
					sanitizedAnswer = sanitizedAnswer[:2000]
				}

				stdinConn.Write([]byte(sanitizedAnswer + "\n"))
				return true
			}

			if clarification.Status == goshared.ClarificationStatusExpired {
				return false
			}
		}
	}
}

func ptrStr(s string) *string {
	return &s
}

func ptrInt64(i int64) *int64 {
	return &i
}
