package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/KranzL/shipmates-oss/bot/orchestrator"
	"github.com/KranzL/shipmates-oss/go-llm"
	goshared "github.com/KranzL/shipmates-oss/go-shared"
)

const (
	maxOutputBytes  = 512 * 1024
	maxContextBytes = 200 * 1024
	maxSketchTokens = 8192
	maxIterations   = 3
)

var discordMentionRegex = regexp.MustCompile(`@(everyone|here)`)

func stripDiscordMentions(text string) string {
	return discordMentionRegex.ReplaceAllStringFunc(text, func(match string) string {
		return "@\u200b" + match[1:]
	})
}

type DirectEngineRequest struct {
	UserID           string
	AgentID          string
	Task             string
	Repo             string
	Provider         string
	APIKey           string
	GithubToken      string
	LogFn            func(ctx context.Context, msg string) (string, error)
	AgentScope       *AgentScope
	BaseURL          string
	ModelID          string
	PlatformUserID   string
	Mode             string
	MaxIterations    int
}

func CollectFiles(repoDir string, scope *AgentScope) ([]string, error) {
	files := make([]string, 0, 100)

	err := filepath.Walk(repoDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			base := filepath.Base(path)
			if base == ".git" || base == "node_modules" || base == "dist" || base == ".next" {
				return filepath.SkipDir
			}
			return nil
		}

		relPath, err := filepath.Rel(repoDir, path)
		if err != nil {
			return err
		}

		if scope != nil {
			if len(scope.Directories) > 0 {
				matched := false
				for _, dir := range scope.Directories {
					if strings.HasPrefix(relPath, dir) {
						matched = true
						break
					}
				}
				if !matched {
					return nil
				}
			}

			if len(scope.Languages) > 0 {
				if !fileMatchesLanguages(relPath, scope.Languages) {
					return nil
				}
			}
		}

		files = append(files, relPath)
		return nil
	})

	return files, err
}

func ReadSourceFiles(repoDir string, files []string, maxBytes int) ([]FileWithContent, error) {
	var result []FileWithContent
	totalBytes := 0

	for _, file := range files {
		if totalBytes >= maxBytes {
			break
		}

		absolutePath := filepath.Join(repoDir, file)
		data, err := os.ReadFile(absolutePath)
		if err != nil {
			continue
		}

		content := string(data)
		if totalBytes+len(content) > maxBytes {
			remaining := maxBytes - totalBytes
			if remaining > 0 {
				content = content[:remaining]
			} else {
				break
			}
		}

		result = append(result, FileWithContent{
			Path:    file,
			Content: content,
		})

		totalBytes += len(content)
	}

	return result, nil
}

func BuildFileTree(files []string) string {
	return strings.Join(files, "\n")
}

func ExecuteDirectLLM(ctx context.Context, db *goshared.DB, relayURL, relaySecret string, req DirectEngineRequest) error {
	logFn := req.LogFn
	if logFn == nil {
		logFn = func(ctx context.Context, msg string) (string, error) {
			fmt.Printf("[engine] %s\n", msg)
			return "", nil
		}
	}

	sanitizedLogFn := func(ctx context.Context, msg string) (string, error) {
		return logFn(ctx, stripDiscordMentions(msg))
	}
	logFn = sanitizedLogFn

	session := &goshared.Session{
		UserID:        req.UserID,
		AgentID:       req.AgentID,
		Task:          req.Task,
		Status:        goshared.SessionStatusRunning,
		ExecutionMode: goshared.ExecutionModeDirect,
	}
	repoStr := req.Repo
	session.Repo = &repoStr
	now := time.Now()
	session.StartedAt = &now
	if req.PlatformUserID != "" {
		session.PlatformUserID = &req.PlatformUserID
	}

	session, err := db.CreateSession(ctx, session)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}

	relay := orchestrator.NewRelayConnection(session.ID, relayURL, relaySecret)
	defer relay.Close()

	emitter := NewProgressEmitter(relay, logFn)
	defer emitter.Close()

	branchName := fmt.Sprintf("shipmates/%s", session.ID)
	tmpDir, err := os.MkdirTemp("", "shipmates-engine-")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	client, err := llm.NewClient(llm.Provider(req.Provider), req.APIKey, &llm.ClientOptions{
		BaseURL: req.BaseURL,
		ModelID: req.ModelID,
	})
	if err != nil {
		emitter.EmitError(fmt.Sprintf("Failed to create LLM client: %v", err), false)
		emitter.PushStatus("error")
		outputStr := fmt.Sprintf("LLM client creation failed: %v", err)
		db.UpdateSessionResult(ctx, session.ID, goshared.SessionStatusError, &outputStr, nil, nil)
		return err
	}

	if ctx.Err() != nil {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		db.UpdateSessionResult(cleanupCtx, session.ID, goshared.SessionStatusCancelled, nil, nil, nil)
		return ctx.Err()
	}

	emitter.EmitPhase("cloning", "Cloning repository...")
	logFn(ctx, fmt.Sprintf("Cloning %s...", req.Repo))

	repoDir, err := CloneRepo(ctx, CloneOptions{
		Repo:        req.Repo,
		GithubToken: req.GithubToken,
		TmpDir:      tmpDir,
	})
	if err != nil {
		emitter.EmitError(fmt.Sprintf("Failed to clone repository: %v", err), false)
		emitter.PushStatus("error")
		outputStr := fmt.Sprintf("Clone failed: %v", err)
		db.UpdateSessionResult(ctx, session.ID, goshared.SessionStatusError, &outputStr, nil, nil)
		logFn(ctx, "Failed to clone the repository. Check your GitHub token and repo access.")
		return err
	}

	emitter.EmitPhase("reading", "Reading codebase...")

	allFiles, err := CollectFiles(repoDir, req.AgentScope)
	if err != nil {
		emitter.EmitError(fmt.Sprintf("Failed to collect files: %v", err), false)
		emitter.PushStatus("error")
		outputStr := fmt.Sprintf("File collection failed: %v", err)
		db.UpdateSessionResult(ctx, session.ID, goshared.SessionStatusError, &outputStr, nil, nil)
		return err
	}

	fileTree := BuildFileTree(allFiles)
	sourceFiles, err := ReadSourceFiles(repoDir, allFiles, maxContextBytes)
	if err != nil {
		emitter.EmitError(fmt.Sprintf("Failed to read files: %v", err), false)
		emitter.PushStatus("error")
		outputStr := fmt.Sprintf("File reading failed: %v", err)
		db.UpdateSessionResult(ctx, session.ID, goshared.SessionStatusError, &outputStr, nil, nil)
		return err
	}

	for _, file := range sourceFiles {
		emitter.EmitFileRead(file.Path)
	}

	if ctx.Err() != nil {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		db.UpdateSessionResult(cleanupCtx, session.ID, goshared.SessionStatusCancelled, nil, nil, nil)
		return ctx.Err()
	}

	emitter.EmitPhase("planning", "Planning changes...")

	maxIter := req.MaxIterations
	if maxIter == 0 {
		maxIter = maxIterations
	}

	iterResult, err := IterateSketchApply(IterationOptions{
		Client:           client,
		RepoDir:          repoDir,
		Task:             req.Task,
		Repo:             req.Repo,
		FileTree:         fileTree,
		FullFiles:        sourceFiles,
		AgentScope:       req.AgentScope,
		AgentPersonality: "",
		MaxIterations:    maxIter,
		MaxSketchTokens:  maxSketchTokens,
		OnEvent: func(event map[string]interface{}) {
			emitter.EmitEvent(event)
		},
		Ctx: ctx,
	})

	if err != nil {
		emitter.EmitError(fmt.Sprintf("Iteration failed: %v", err), false)
		emitter.PushStatus("error")
		outputStr := fmt.Sprintf("Iteration failed: %v", err)
		db.UpdateSessionResult(ctx, session.ID, goshared.SessionStatusError, &outputStr, nil, nil)
		logFn(ctx, "Task failed during planning/application phase.")
		return err
	}

	if iterResult.TotalApplied == 0 {
		emitter.EmitError("All edits failed to apply. The AI-generated changes could not be matched to the codebase.", false)
		emitter.PushStatus("error")
		outputStr := fmt.Sprintf("All %d edit(s) failed. The changes could not be matched to the codebase.", iterResult.TotalFailed)
		db.UpdateSessionResult(ctx, session.ID, goshared.SessionStatusError, &outputStr, nil, nil)
		logFn(ctx, "All edits failed. Try rephrasing the task or using a different model.")
		return fmt.Errorf("all edits failed")
	}

	if ctx.Err() != nil {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		db.UpdateSessionResult(cleanupCtx, session.ID, goshared.SessionStatusCancelled, nil, nil, nil)
		return ctx.Err()
	}

	emitter.EmitPhase("pushing", "Pushing branch...")

	if err := CreateBranch(ctx, repoDir, branchName); err != nil {
		emitter.EmitError(fmt.Sprintf("Failed to create branch: %v", err), false)
		emitter.PushStatus("error")
		outputStr := fmt.Sprintf("Branch creation failed: %v", err)
		db.UpdateSessionResult(ctx, session.ID, goshared.SessionStatusError, &outputStr, nil, nil)
		return err
	}

	changedFilePaths := append(iterResult.AppliedFiles, iterResult.CreatedFiles...)
	commitResult, err := CommitAndPush(ctx, CommitOptions{
		RepoDir:    repoDir,
		BranchName: branchName,
		Message:    fmt.Sprintf("shipmates: %s", req.Task),
		Files:      changedFilePaths,
	})

	if err != nil {
		emitter.EmitError(fmt.Sprintf("Commit/push failed: %v", err), false)
		emitter.PushStatus("error")
		outputStr := fmt.Sprintf("Commit/push failed: %v", err)
		db.UpdateSessionResult(ctx, session.ID, goshared.SessionStatusError, &outputStr, nil, nil)
		return err
	}

	var prUrl *string
	if commitResult.Pushed {
		url := fmt.Sprintf("https://github.com/%s/compare/%s", req.Repo, branchName)
		prUrl = &url
	}

	diffStats, _ := GetFileDiffStats(ctx, repoDir)
	diffStatsMap := make(map[string]FileDiffStat)
	for _, stat := range diffStats {
		diffStatsMap[stat.Path] = stat
	}

	for _, file := range iterResult.AppliedFiles {
		stats, exists := diffStatsMap[file]
		additions := 0
		deletions := 0
		if exists {
			additions = stats.Additions
			deletions = stats.Deletions
		}
		emitter.EmitFileEdit(file, additions, deletions)
	}

	for _, file := range iterResult.CreatedFiles {
		emitter.EmitFileCreate(file)
	}

	var outputLines []string
	if iterResult.LastApplyResult != nil && iterResult.LastApplyResult.TotalApplied > 0 {
		outputLines = append(outputLines, fmt.Sprintf("Applied %d change(s).", iterResult.TotalApplied))
	}
	if iterResult.LastApplyResult != nil && iterResult.LastApplyResult.TotalFailed > 0 {
		outputLines = append(outputLines, fmt.Sprintf("%d edit(s) failed.", iterResult.TotalFailed))
	}
	if commitResult.Pushed {
		outputLines = append(outputLines, fmt.Sprintf("Pushed to branch: %s", branchName))
	} else if commitResult.Committed {
		outputLines = append(outputLines, "Committed locally but push failed.")
	}

	output := strings.Join(outputLines, "\n")
	if len(output) > maxOutputBytes {
		output = output[len(output)-maxOutputBytes:]
	}

	emitter.PushStatus("done")
	db.UpdateSessionResult(ctx, session.ID, goshared.SessionStatusDone, &output, &branchName, prUrl)

	var prUrlStr string
	if prUrl != nil {
		prUrlStr = *prUrl
	}
	emitter.LogCompletion(ctx, branchName, iterResult.AppliedFiles, iterResult.CreatedFiles, prUrlStr)

	CleanupCredentials(tmpDir)
	return nil
}
