package scheduler

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
	pollInterval      = 60 * time.Second
	maxConcurrent     = 5
	executionTimeout  = 30 * time.Minute
	claimExtensionMs  = 2 * 60 * 1000
)

type ExecuteFunc func(ctx context.Context, req WorkRequest) error

type Scheduler struct {
	db                *goshared.DB
	executeFn         ExecuteFunc
	running           atomic.Int32
	stopped           atomic.Bool
	isPolling         atomic.Bool
	activeExecutions  sync.WaitGroup
	ticker            *time.Ticker
	cancel            context.CancelFunc
	semaphore         chan struct{}
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

func NewScheduler(db *goshared.DB, executeFn ExecuteFunc) *Scheduler {
	return &Scheduler{
		db:        db,
		executeFn: executeFn,
		semaphore: make(chan struct{}, maxConcurrent),
	}
}

func (s *Scheduler) Start(ctx context.Context) {
	if s.stopped.Load() {
		return
	}

	ctx, cancel := context.WithCancel(ctx)
	s.cancel = cancel

	fmt.Println("[scheduler] Starting scheduler service")

	s.poll(ctx)

	jitter := time.Duration(rand.Intn(5000)) * time.Millisecond
	s.ticker = time.NewTicker(pollInterval + jitter)
	go func() {
		defer s.ticker.Stop()
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

func (s *Scheduler) Stop() {
	s.stopped.Store(true)

	if s.cancel != nil {
		s.cancel()
	}

	if s.ticker != nil {
		s.ticker.Stop()
	}

	activeCount := s.running.Load()
	if activeCount > 0 {
		fmt.Printf("[scheduler] Draining %d active executions...\n", activeCount)
		s.activeExecutions.Wait()
	}

	fmt.Println("[scheduler] Stopped scheduler service")
}

func (s *Scheduler) poll(ctx context.Context) {
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

	now := time.Now()
	dueSchedules, err := s.db.GetDueSchedules(pollCtx, now)
	if err != nil {
		fmt.Printf("[scheduler] Poll error: %v\n", err)
		return
	}

	for i, schedule := range dueSchedules {
		if s.stopped.Load() {
			return
		}

		if i >= slots {
			break
		}

		claimed := s.claimSchedule(pollCtx, schedule.ID, schedule.NextRunAt)
		if !claimed {
			continue
		}

		select {
		case s.semaphore <- struct{}{}:
			s.running.Add(1)
			s.activeExecutions.Add(1)

			go func(sch goshared.AgentSchedule) {
				defer func() {
					<-s.semaphore
					s.running.Add(-1)
					s.activeExecutions.Done()
				}()

				s.executeWithTimeout(ctx, sch)
			}(schedule)
		default:
			nextRunAt, _ := goshared.ComputeNextRunAt(schedule.CronExpression, schedule.Timezone, nil)
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
			s.db.UpdateScheduleAfterRun(cleanupCtx, schedule.ID, "error", nextRunAt, true)
			cleanupCancel()
			break
		}
	}
}

func (s *Scheduler) claimSchedule(ctx context.Context, scheduleID string, expectedNextRunAt time.Time) bool {
	claimCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	newNextRunAt := time.Now().Add(time.Duration(claimExtensionMs) * time.Millisecond)

	result, err := s.db.ExecContext(claimCtx, `
		UPDATE agent_schedules
		SET next_run_at = $1
		WHERE id = $2 AND next_run_at = $3 AND is_active = true
	`, newNextRunAt, scheduleID, expectedNextRunAt)

	if err != nil {
		return false
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return false
	}

	return rowsAffected > 0
}

func (s *Scheduler) executeWithTimeout(ctx context.Context, schedule goshared.AgentSchedule) {
	timeoutCtx, cancel := context.WithTimeout(ctx, executionTimeout)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		select {
		case done <- s.executeSchedule(timeoutCtx, schedule):
		case <-timeoutCtx.Done():
		}
	}()

	select {
	case err := <-done:
		if err != nil {
			fmt.Printf("[scheduler] Execution error for schedule %s: %v\n", schedule.ID, err)
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
			s.markFailure(cleanupCtx, schedule)
			cleanupCancel()
		}
	case <-timeoutCtx.Done():
		fmt.Printf("[scheduler] Execution timeout for schedule %s\n", schedule.ID)
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		s.markFailure(cleanupCtx, schedule)
		cleanupCancel()
	}
}

func (s *Scheduler) executeSchedule(ctx context.Context, schedule goshared.AgentSchedule) error {
	startTime := time.Now()

	sessionCtx, sessionCancel := context.WithTimeout(ctx, 10*time.Second)
	session, err := s.db.CreateSession(sessionCtx, &goshared.Session{
		UserID:        schedule.UserID,
		AgentID:       schedule.AgentID,
		Task:          schedule.Task,
		Status:        goshared.SessionStatusPending,
		ExecutionMode: goshared.ExecutionModeContainer,
		ScheduleID:    &schedule.ID,
	})
	sessionCancel()

	if err != nil {
		fmt.Printf("[scheduler] Failed to create session for schedule %s: %v\n", schedule.ID, err)
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		nextRunAt, _ := goshared.ComputeNextRunAt(schedule.CronExpression, schedule.Timezone, nil)
		s.db.UpdateScheduleAfterRun(cleanupCtx, schedule.ID, "error", nextRunAt, true)
		cleanupCancel()
		return err
	}

	execCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	agent, apiKey, githubConn, repo, scopeConfig, validationErr := validateAndFetchScheduleData(execCtx, s.db, schedule)
	if validationErr != nil {
		errMsg := validationErr.Error()
		fmt.Printf("[scheduler] Validation/fetch error for schedule %s: %v\n", schedule.ID, validationErr)

		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		s.db.UpdateSessionResult(cleanupCtx, session.ID, goshared.SessionStatusError, ptrStr(errMsg), nil, nil)
		nextRunAt, _ := goshared.ComputeNextRunAt(schedule.CronExpression, schedule.Timezone, nil)
		s.db.UpdateScheduleAfterRun(cleanupCtx, schedule.ID, "error", nextRunAt, true)
		cleanupCancel()

		agentName := ""
		if agent != nil {
			agentName = agent.Name
		} else {
			agentName = schedule.AgentID
		}

		notifyScheduleResult(context.Background(), s.db, schedule.UserID, ScheduleResult{
			ScheduleName: schedule.Name,
			AgentName:    agentName,
			Task:         schedule.Task,
			Status:       "error",
			DurationMs:   time.Since(startTime).Milliseconds(),
			BranchName:   "",
			SessionID:    session.ID,
		})

		return fmt.Errorf("validation failed")
	}

	updateRepoCtx, updateRepoCancel := context.WithTimeout(ctx, 5*time.Second)
	s.db.ExecContext(updateRepoCtx, "UPDATE sessions SET repo = $1 WHERE id = $2", repo, session.ID)
	updateRepoCancel()

	decryptedAPIKey, err := goshared.DecryptWithKey(apiKey.APIKey)
	if err != nil {
		fmt.Printf("[scheduler] Failed to decrypt API key for schedule %s: %v\n", schedule.ID, err)
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		s.db.UpdateSessionResult(cleanupCtx, session.ID, goshared.SessionStatusError, ptrStr("Failed to decrypt API key"), nil, nil)
		nextRunAt, _ := goshared.ComputeNextRunAt(schedule.CronExpression, schedule.Timezone, nil)
		s.db.UpdateScheduleAfterRun(cleanupCtx, schedule.ID, "error", nextRunAt, true)
		cleanupCancel()
		return err
	}

	decryptedGithubToken, err := goshared.DecryptWithKey(githubConn.GithubToken)
	if err != nil {
		fmt.Printf("[scheduler] Failed to decrypt GitHub token for schedule %s: %v\n", schedule.ID, err)
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		s.db.UpdateSessionResult(cleanupCtx, session.ID, goshared.SessionStatusError, ptrStr("Failed to decrypt GitHub token"), nil, nil)
		nextRunAt, _ := goshared.ComputeNextRunAt(schedule.CronExpression, schedule.Timezone, nil)
		s.db.UpdateScheduleAfterRun(cleanupCtx, schedule.ID, "error", nextRunAt, true)
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
		UserID:      schedule.UserID,
		AgentID:     schedule.AgentID,
		Task:        schedule.Task,
		Repo:        repo,
		Provider:    string(apiKey.Provider),
		APIKey:      decryptedAPIKey,
		GithubToken: decryptedGithubToken,
		AgentScope:  scopeConfig,
		BaseURL:     baseURL,
		ModelID:     modelID,
	}

	if err := s.executeFn(ctx, workRequest); err != nil {
		fmt.Printf("[scheduler] Execution failed for schedule %s: %v\n", schedule.ID, err)
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		s.db.UpdateSessionResult(cleanupCtx, session.ID, goshared.SessionStatusError, ptrStr(err.Error()), nil, nil)
		nextRunAt, _ := goshared.ComputeNextRunAt(schedule.CronExpression, schedule.Timezone, nil)
		s.db.UpdateScheduleAfterRun(cleanupCtx, schedule.ID, "error", nextRunAt, true)
		cleanupCancel()
		return err
	}

	finishCtx, finishCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer finishCancel()

	finishedSession, err := s.db.GetSessionByID(finishCtx, session.ID)
	if err != nil {
		fmt.Printf("[scheduler] Failed to get finished session for schedule %s: %v\n", schedule.ID, err)
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		nextRunAt, _ := goshared.ComputeNextRunAt(schedule.CronExpression, schedule.Timezone, nil)
		s.db.UpdateScheduleAfterRun(cleanupCtx, schedule.ID, "error", nextRunAt, true)
		cleanupCancel()
		return err
	}

	status := "error"
	if finishedSession != nil && finishedSession.Status == goshared.SessionStatusDone {
		status = "done"
	}

	durationMs := time.Since(startTime).Milliseconds()

	nextRunAt, err := goshared.ComputeNextRunAt(schedule.CronExpression, schedule.Timezone, nil)
	if err != nil {
		fmt.Printf("[scheduler] Failed to compute next run for schedule %s: %v\n", schedule.ID, err)
		nextRunAt = time.Now().Add(24 * time.Hour)
	}

	failed := status == "error"
	updateCtx, updateCancel := context.WithTimeout(context.Background(), 10*time.Second)
	if err := s.db.UpdateScheduleAfterRun(updateCtx, schedule.ID, status, nextRunAt, failed); err != nil {
		fmt.Printf("[scheduler] Failed to update schedule after run %s: %v\n", schedule.ID, err)
	}
	updateCancel()

	branchName := ""
	if finishedSession != nil && finishedSession.BranchName != nil {
		branchName = *finishedSession.BranchName
	}

	notifyScheduleResult(context.Background(), s.db, schedule.UserID, ScheduleResult{
		ScheduleName: schedule.Name,
		AgentName:    agent.Name,
		Task:         schedule.Task,
		Status:       status,
		DurationMs:   durationMs,
		BranchName:   branchName,
		SessionID:    session.ID,
	})

	return nil
}

func (s *Scheduler) markFailure(ctx context.Context, schedule goshared.AgentSchedule) {
	markCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	nextRunAt, err := goshared.ComputeNextRunAt(schedule.CronExpression, schedule.Timezone, nil)
	if err != nil {
		fmt.Printf("[scheduler] Failed to compute next run for failed schedule %s: %v\n", schedule.ID, err)
		nextRunAt = time.Now().Add(24 * time.Hour)
	}

	if err := s.db.UpdateScheduleAfterRun(markCtx, schedule.ID, "error", nextRunAt, true); err != nil {
		fmt.Printf("[scheduler] Failed to mark failure for schedule %s: %v\n", schedule.ID, err)
		return
	}

	disableCtx, disableCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer disableCancel()

	result, err := s.db.ExecContext(disableCtx, "UPDATE agent_schedules SET is_active = false WHERE id = $1 AND failure_count >= 5", schedule.ID)
	if err != nil {
		fmt.Printf("[scheduler] Failed to check/disable schedule %s after failures: %v\n", schedule.ID, err)
		return
	}

	rowsAffected, err := result.RowsAffected()
	if err == nil && rowsAffected > 0 {
		fmt.Printf("[scheduler] Disabled schedule %s after 5 consecutive failures\n", schedule.ID)

		agentName := ""
		agentCtx, agentCancel := context.WithTimeout(context.Background(), 5*time.Second)
		agent, err := s.db.GetAgent(agentCtx, schedule.AgentID, schedule.UserID)
		agentCancel()
		if err == nil && agent != nil {
			agentName = agent.Name
		} else {
			agentName = schedule.AgentID
		}

		notifyScheduleResult(context.Background(), s.db, schedule.UserID, ScheduleResult{
			ScheduleName: schedule.Name,
			AgentName:    agentName,
			Task:         schedule.Task,
			Status:       "disabled",
			DurationMs:   0,
			BranchName:   "",
			SessionID:    "",
		})
	}
}

func ptrStr(s string) *string {
	return &s
}
