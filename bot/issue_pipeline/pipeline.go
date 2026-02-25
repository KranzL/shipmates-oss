package issue_pipeline

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	goshared "github.com/KranzL/shipmates-oss/go-shared"
)

const (
	pollInterval     = 30 * time.Second
	maxConcurrent    = 3
	executionTimeout = 30 * time.Minute
)

type ExecuteFunc func(ctx context.Context, req WorkRequest) error

type PipelineService struct {
	db               *goshared.DB
	executeFn        ExecuteFunc
	running          atomic.Int32
	stopped          atomic.Bool
	isPolling        atomic.Bool
	activeExecutions sync.WaitGroup
	pollerWg         sync.WaitGroup
	ticker           *time.Ticker
	cancel           context.CancelFunc
	semaphore        chan struct{}
}

type WorkRequest struct {
	UserID      string
	AgentID     string
	Task        string
	Repo        string
	Provider    string
	APIKey      string
	GithubToken string
	AgentScope  interface{}
	BaseURL     string
	ModelID     string
}

func NewPipelineService(db *goshared.DB, executeFn ExecuteFunc) *PipelineService {
	return &PipelineService{
		db:        db,
		executeFn: executeFn,
		semaphore: make(chan struct{}, maxConcurrent),
	}
}

func (s *PipelineService) Start(ctx context.Context) {
	if s.stopped.Load() {
		return
	}

	ctx, cancel := context.WithCancel(ctx)
	s.cancel = cancel

	fmt.Println("[issue-pipeline] Starting issue pipeline service")

	s.poll(ctx)

	jitter := time.Duration(rand.Intn(3000)) * time.Millisecond
	s.ticker = time.NewTicker(pollInterval + jitter)
	s.pollerWg.Add(1)
	go func() {
		defer s.pollerWg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case <-s.ticker.C:
				s.poll(ctx)
			}
		}
	}()
}

func (s *PipelineService) Stop() {
	s.stopped.Store(true)

	if s.cancel != nil {
		s.cancel()
	}

	if s.ticker != nil {
		s.ticker.Stop()
	}

	s.pollerWg.Wait()

	activeCount := s.running.Load()
	if activeCount > 0 {
		fmt.Printf("[issue-pipeline] Draining %d active executions...\n", activeCount)
		s.activeExecutions.Wait()
	}

	fmt.Println("[issue-pipeline] Stopped issue pipeline service")
}

func (s *PipelineService) poll(ctx context.Context) {
	if s.isPolling.Swap(true) {
		return
	}
	if s.stopped.Load() {
		s.isPolling.Store(false)
		return
	}
	defer s.isPolling.Store(false)

	slots := maxConcurrent - int(s.running.Load())
	if slots <= 0 {
		return
	}

	pollCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	pendingPipelines, err := s.db.GetPendingIssuePipelines(pollCtx, slots)
	if err != nil {
		fmt.Printf("[issue-pipeline] Poll error: %v\n", err)
		return
	}

	for i, pipeline := range pendingPipelines {
		if s.stopped.Load() {
			return
		}

		if i >= slots {
			break
		}

		claimed := s.claimPipeline(pollCtx, pipeline.ID)
		if !claimed {
			continue
		}

		select {
		case s.semaphore <- struct{}{}:
			s.running.Add(1)
			s.activeExecutions.Add(1)

			go func(p goshared.IssuePipeline) {
				defer func() {
					<-s.semaphore
					s.running.Add(-1)
					s.activeExecutions.Done()
				}()

				s.executeWithTimeout(ctx, p)
			}(pipeline)
		default:
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
			s.db.UpdateIssuePipelineStatus(cleanupCtx, pipeline.ID, pipeline.UserID, "error", ptrStr("Failed to acquire execution slot"))
			cleanupCancel()
			break
		}
	}
}

func (s *PipelineService) claimPipeline(ctx context.Context, pipelineID string) bool {
	claimCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	result, err := s.db.ExecContext(claimCtx, `
		UPDATE issue_pipelines
		SET status = 'running', started_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND status = 'pending'
	`, pipelineID)

	if err != nil {
		return false
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return false
	}

	return rowsAffected > 0
}

func (s *PipelineService) executeWithTimeout(ctx context.Context, pipeline goshared.IssuePipeline) {
	timeoutCtx, cancel := context.WithTimeout(ctx, executionTimeout)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- s.executePipeline(timeoutCtx, pipeline)
	}()

	select {
	case err := <-done:
		if err != nil {
			fmt.Printf("[issue-pipeline] Execution error for pipeline %s: %v\n", pipeline.ID, err)
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
			s.markFailure(cleanupCtx, pipeline, err.Error())
			cleanupCancel()
		}
	case <-timeoutCtx.Done():
		fmt.Printf("[issue-pipeline] Execution timeout for pipeline %s\n", pipeline.ID)
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		s.markFailure(cleanupCtx, pipeline, "Execution timeout after 30 minutes")
		cleanupCancel()
	}
}

func (s *PipelineService) executePipeline(ctx context.Context, pipeline goshared.IssuePipeline) error {
	startTime := time.Now()

	execCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	agent, apiKey, githubConn, scopeConfig, validationErr := validateAndFetchPipelineData(execCtx, s.db, pipeline)
	if validationErr != nil {
		fmt.Printf("[issue-pipeline] Validation/fetch error for pipeline %s: %v\n", pipeline.ID, validationErr)

		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		s.db.UpdateIssuePipelineStatus(cleanupCtx, pipeline.ID, pipeline.UserID, "error", ptrStr(validationErr.Error()))
		cleanupCancel()

		return fmt.Errorf("validation failed")
	}

	truncatedBody := pipeline.IssueBody
	if len(truncatedBody) > 10000 {
		truncatedBody = truncatedBody[:10000]
	}
	task := fmt.Sprintf("GitHub Issue #%d: %s\n\n%s", pipeline.IssueNumber, pipeline.IssueTitle, truncatedBody)

	sessionCtx, sessionCancel := context.WithTimeout(ctx, 10*time.Second)
	session, err := s.db.CreateSession(sessionCtx, &goshared.Session{
		UserID:        pipeline.UserID,
		AgentID:       pipeline.AgentID,
		Task:          task,
		Repo:          &pipeline.Repo,
		Status:        goshared.SessionStatusPending,
		ExecutionMode: goshared.ExecutionModeContainer,
	})
	sessionCancel()

	if err != nil {
		fmt.Printf("[issue-pipeline] Failed to create session for pipeline %s: %v\n", pipeline.ID, err)
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		s.db.UpdateIssuePipelineStatus(cleanupCtx, pipeline.ID, pipeline.UserID, "error", ptrStr("Failed to create session"))
		cleanupCancel()
		return err
	}

	updateSessionCtx, updateSessionCancel := context.WithTimeout(ctx, 5*time.Second)
	sessionID := session.ID
	_, err = s.db.ExecContext(updateSessionCtx, `
		UPDATE issue_pipelines
		SET session_id = $1, started_at = NOW(), updated_at = NOW()
		WHERE id = $2
	`, sessionID, pipeline.ID)
	updateSessionCancel()
	if err != nil {
		fmt.Printf("[issue-pipeline] Failed to link session for pipeline %s: %v\n", pipeline.ID, err)
	}

	decryptedAPIKey, err := goshared.DecryptWithKey(apiKey.APIKey)
	if err != nil {
		fmt.Printf("[issue-pipeline] Failed to decrypt API key for pipeline %s: %v\n", pipeline.ID, err)
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		s.db.UpdateSessionResult(cleanupCtx, session.ID, goshared.SessionStatusError, ptrStr("Failed to decrypt API key"), nil, nil)
		s.db.UpdateIssuePipelineStatus(cleanupCtx, pipeline.ID, pipeline.UserID, "error", ptrStr("Failed to decrypt API key"))
		cleanupCancel()
		return err
	}

	decryptedGithubToken, err := goshared.DecryptWithKey(githubConn.GithubToken)
	if err != nil {
		fmt.Printf("[issue-pipeline] Failed to decrypt GitHub token for pipeline %s: %v\n", pipeline.ID, err)
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		s.db.UpdateSessionResult(cleanupCtx, session.ID, goshared.SessionStatusError, ptrStr("Failed to decrypt GitHub token"), nil, nil)
		s.db.UpdateIssuePipelineStatus(cleanupCtx, pipeline.ID, pipeline.UserID, "error", ptrStr("Failed to decrypt GitHub token"))
		cleanupCancel()
		return err
	}

	baseURL := ""
	if apiKey.BaseURL != nil {
		baseURL = *apiKey.BaseURL
	}

	modelID := ""
	if apiKey.ModelID != nil {
		modelID = *apiKey.ModelID
	}

	workRequest := WorkRequest{
		UserID:      pipeline.UserID,
		AgentID:     pipeline.AgentID,
		Task:        task,
		Repo:        pipeline.Repo,
		Provider:    string(apiKey.Provider),
		APIKey:      decryptedAPIKey,
		GithubToken: decryptedGithubToken,
		AgentScope:  scopeConfig,
		BaseURL:     baseURL,
		ModelID:     modelID,
	}

	if err := s.executeFn(ctx, workRequest); err != nil {
		fmt.Printf("[issue-pipeline] Execution failed for pipeline %s: %v\n", pipeline.ID, err)
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		s.db.UpdateSessionResult(cleanupCtx, session.ID, goshared.SessionStatusError, ptrStr(err.Error()), nil, nil)
		s.markFailure(cleanupCtx, pipeline, err.Error())
		cleanupCancel()
		return err
	}

	finishCtx, finishCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer finishCancel()

	finishedSession, err := s.db.GetSessionByID(finishCtx, session.ID)
	if err != nil {
		fmt.Printf("[issue-pipeline] Failed to get finished session for pipeline %s: %v\n", pipeline.ID, err)
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		s.markFailure(cleanupCtx, pipeline, fmt.Sprintf("Failed to get finished session: %v", err))
		cleanupCancel()
		return err
	}

	if finishedSession == nil {
		fmt.Printf("[issue-pipeline] Finished session is nil for pipeline %s\n", pipeline.ID)
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		s.markFailure(cleanupCtx, pipeline, "Finished session not found")
		cleanupCancel()
		return fmt.Errorf("finished session is nil")
	}

	sessionStatus := finishedSession.Status
	branchName := finishedSession.BranchName

	if sessionStatus == goshared.SessionStatusDone && branchName != nil && *branchName != "" {
		defaultBranch, err := getDefaultBranch(finishCtx, decryptedGithubToken, pipeline.Repo)
		if err != nil {
			errMsg := fmt.Sprintf("Failed to get default branch: %v", err)
			fmt.Printf("[issue-pipeline] %s for pipeline %s\n", errMsg, pipeline.ID)

			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
			escapedAgentName := escapeMarkdown(agent.Name)
			commentErr := commentOnIssue(cleanupCtx, decryptedGithubToken, pipeline.Repo, pipeline.IssueNumber, fmt.Sprintf("**%s** attempted to work on this issue but encountered an error creating the pull request.\n\nThe team has been notified. You can check the session details in the Shipmates dashboard.", escapedAgentName))
			if commentErr != nil {
				fmt.Printf("[issue-pipeline] Failed to post issue comment: %v\n", commentErr)
			}
			s.markFailure(cleanupCtx, pipeline, errMsg)
			cleanupCancel()
			return fmt.Errorf("get default branch: %w", err)
		}

		prTitle := fmt.Sprintf("Fix #%d: %s", pipeline.IssueNumber, pipeline.IssueTitle)
		prBody := fmt.Sprintf("Resolves #%d\n\n%s\n\n---\nAutomated by Shipmates", pipeline.IssueNumber, pipeline.IssueTitle)

		pr, err := createPullRequest(finishCtx, decryptedGithubToken, pipeline.Repo, *branchName, defaultBranch, prTitle, prBody)
		if err != nil {
			errMsg := fmt.Sprintf("Failed to create PR: %v", err)
			fmt.Printf("[issue-pipeline] %s for pipeline %s\n", errMsg, pipeline.ID)

			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
			escapedAgentName := escapeMarkdown(agent.Name)
			commentErr := commentOnIssue(cleanupCtx, decryptedGithubToken, pipeline.Repo, pipeline.IssueNumber, fmt.Sprintf("**%s** attempted to work on this issue but encountered an error creating the pull request.\n\nThe team has been notified. You can check the session details in the Shipmates dashboard.", escapedAgentName))
			if commentErr != nil {
				fmt.Printf("[issue-pipeline] Failed to post issue comment: %v\n", commentErr)
			}
			s.markFailure(cleanupCtx, pipeline, errMsg)
			cleanupCancel()
			return fmt.Errorf("create PR: %w", err)
		}

		escapedAgentName := escapeMarkdown(agent.Name)
		escapedBranchName := escapeMarkdown(*branchName)
		issueComment := fmt.Sprintf("**%s** created a pull request to address this issue: #%d\n\nBranch: `%s`", escapedAgentName, pr.Number, escapedBranchName)
		commentCtx, commentCancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := commentOnIssue(commentCtx, decryptedGithubToken, pipeline.Repo, pipeline.IssueNumber, issueComment); err != nil {
			fmt.Printf("[issue-pipeline] Failed to post success comment: %v\n", err)
		}
		commentCancel()

		updateCtx, updateCancel := context.WithTimeout(context.Background(), 10*time.Second)
		_, err = s.db.ExecContext(updateCtx, `
			UPDATE issue_pipelines
			SET status = 'done', pr_number = $1, pr_url = $2, branch_name = $3, finished_at = NOW(), updated_at = NOW()
			WHERE id = $4
		`, pr.Number, pr.URL, *branchName, pipeline.ID)
		updateCancel()
		if err != nil {
			fmt.Printf("[issue-pipeline] Failed to update pipeline status to done: %v\n", err)
		}

		durationMs := time.Since(startTime).Milliseconds()
		notifyScheduleResult(context.Background(), s.db, pipeline.UserID, ScheduleResult{
			ScheduleName: fmt.Sprintf("Issue #%d", pipeline.IssueNumber),
			AgentName:    agent.Name,
			Task:         pipeline.IssueTitle,
			Status:       "done",
			DurationMs:   durationMs,
			BranchName:   *branchName,
			SessionID:    session.ID,
		})

		fmt.Printf("[issue-pipeline] Pipeline %s completed successfully. PR: %s\n", pipeline.ID, pr.URL)
	} else {
		errorMessage := "Agent execution failed"
		if sessionStatus != goshared.SessionStatusError {
			errorMessage = "No branch created"
		}

		escapedAgentName := escapeMarkdown(agent.Name)
		issueComment := fmt.Sprintf("**%s** attempted to work on this issue but encountered an error.\n\nThe team has been notified. You can check the session details in the Shipmates dashboard.", escapedAgentName)
		commentCtx, commentCancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := commentOnIssue(commentCtx, decryptedGithubToken, pipeline.Repo, pipeline.IssueNumber, issueComment); err != nil {
			fmt.Printf("[issue-pipeline] Failed to post error comment: %v\n", err)
		}
		commentCancel()

		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		s.db.UpdateIssuePipelineStatus(cleanupCtx, pipeline.ID, pipeline.UserID, "error", ptrStr(errorMessage))
		cleanupCancel()
	}

	return nil
}

func (s *PipelineService) markFailure(ctx context.Context, pipeline goshared.IssuePipeline, errMsg string) {
	markCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := s.db.UpdateIssuePipelineStatus(markCtx, pipeline.ID, pipeline.UserID, "error", ptrStr(errMsg)); err != nil {
		fmt.Printf("[issue-pipeline] Failed to mark failure for pipeline %s: %v\n", pipeline.ID, err)
	}
}

func ptrStr(s string) *string {
	return &s
}
