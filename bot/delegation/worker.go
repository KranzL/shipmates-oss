package delegation

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"regexp"
	"sync"
	"sync/atomic"
	"time"

	goshared "github.com/KranzL/shipmates-oss/go-shared"
)

const (
	pollInterval                    = 10 * time.Second
	maxConcurrentSessionsPerUser    = 3
	maxAgentsPerPoll                = 20
	jitterMs                        = 2000
	maxDecryptionCacheSize          = 1000
	decryptCacheTTL                 = 5 * time.Minute
)

type Worker struct {
	db             *goshared.DB
	executeFn      ExecuteFunc
	cancel         context.CancelFunc
	running        atomic.Bool
	mu             sync.Mutex
	decryptCache   map[string]map[string]*decryptCacheEntry
	decryptCacheMu sync.RWMutex
	isProcessing   atomic.Bool
	activeTasksWg  sync.WaitGroup
}

type decryptCacheEntry struct {
	key       string
	lastUsed  time.Time
	expiresAt time.Time
}

type ExecuteFunc func(ctx context.Context, req WorkRequest) error

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
}

func NewWorker(db *goshared.DB, executeFn ExecuteFunc) *Worker {
	return &Worker{
		db:           db,
		executeFn:    executeFn,
		decryptCache: make(map[string]map[string]*decryptCacheEntry),
	}
}

func (w *Worker) Start(ctx context.Context) {
	if w.running.Swap(true) {
		return
	}

	ctx, cancel := context.WithCancel(ctx)
	w.mu.Lock()
	w.cancel = cancel
	w.mu.Unlock()

	jitter := time.Duration(rand.Intn(jitterMs)) * time.Millisecond
	time.Sleep(jitter)

	go func() {
		if err := w.processTaskQueue(ctx); err != nil {
			fmt.Printf("[delegation-worker] error processing queue: %v\n", err)
		}

		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := w.processTaskQueue(ctx); err != nil {
					fmt.Printf("[delegation-worker] error processing queue: %v\n", err)
				}
			}
		}
	}()
}

func (w *Worker) Stop() {
	if !w.running.Swap(false) {
		return
	}

	if w.cancel != nil {
		w.cancel()
	}

	w.activeTasksWg.Wait()

	w.decryptCacheMu.Lock()
	w.decryptCache = make(map[string]map[string]*decryptCacheEntry)
	w.decryptCacheMu.Unlock()
}

func (w *Worker) processTaskQueue(ctx context.Context) error {
	if w.isProcessing.Swap(true) {
		return nil
	}
	defer w.isProcessing.Store(false)

	pollCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	query := `
		SELECT DISTINCT t.to_agent_id, t.user_id
		FROM delegated_tasks t
		WHERE t.status = 'pending'
		AND (t.blocked_by IS NULL OR cardinality(t.blocked_by) = 0)
		ORDER BY t.to_agent_id
		LIMIT $1
	`

	rows, err := w.db.QueryContext(pollCtx, query, maxAgentsPerPoll)
	if err != nil {
		return fmt.Errorf("query agents with pending tasks: %w", err)
	}
	defer rows.Close()

	type agentWithUser struct {
		toAgentID string
		userID    string
	}

	var agents []agentWithUser
	for rows.Next() {
		var a agentWithUser
		if err := rows.Scan(&a.toAgentID, &a.userID); err != nil {
			return fmt.Errorf("scan agent: %w", err)
		}
		agents = append(agents, a)
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate agents: %w", err)
	}

	for _, a := range agents {
		opCtx, opCancel := context.WithTimeout(pollCtx, 10*time.Second)

		canStart, err := w.canStartNewSession(opCtx, a.userID)
		if err != nil {
			opCancel()
			fmt.Printf("[delegation-worker] failed to check session count: %v\n", err)
			continue
		}
		if !canStart {
			opCancel()
			continue
		}

		task, err := w.db.ClaimNextTask(opCtx, a.toAgentID)
		if err != nil {
			opCancel()
			fmt.Printf("[delegation-worker] failed to claim task: %v\n", err)
			continue
		}
		if task == nil {
			opCancel()
			continue
		}

		agent, err := w.db.GetAgent(opCtx, a.toAgentID, a.userID)
		if err != nil {
			opCancel()
			fmt.Printf("[delegation-worker] failed to get agent: %v\n", err)
			OnTaskFailed(context.Background(), w.db, task.ID, task.UserID, fmt.Sprintf("Failed to fetch agent details: %v", err))
			continue
		}

		if agent.UserID != task.UserID {
			opCancel()
			OnTaskFailed(context.Background(), w.db, task.ID, task.UserID, "Agent does not belong to task owner")
			continue
		}

		if agent.APIKeyID == nil {
			opCancel()
			OnTaskFailed(context.Background(), w.db, task.ID, task.UserID, fmt.Sprintf("Agent %s is missing an API key. Add one in the dashboard.", agent.Name))
			continue
		}

		apiKey, err := w.db.GetAPIKey(opCtx, *agent.APIKeyID, a.userID)
		if err != nil || apiKey == nil {
			opCancel()
			OnTaskFailed(context.Background(), w.db, task.ID, task.UserID, "Agent API key not found. Please configure an API key in the dashboard.")
			continue
		}

		githubConn, err := w.db.GetGithubConnection(opCtx, a.userID)
		if err != nil || githubConn == nil {
			opCancel()
			OnTaskFailed(context.Background(), w.db, task.ID, task.UserID, "GitHub account not connected. Connect your GitHub account in the dashboard.")
			continue
		}
		opCancel()

		var delegationContext goshared.DelegationContext
		if err := json.Unmarshal(task.Context, &delegationContext); err != nil {
			OnTaskFailed(ctx, w.db, task.ID, task.UserID, "Task context is corrupted. This is a system error, not your fault. Please try creating a new task.")
			continue
		}

		var parentSession *goshared.Session
		if task.ParentSessionID != nil {
			sessionCtx, sessionCancel := context.WithTimeout(pollCtx, 5*time.Second)
			parentSession, err = w.db.GetSession(sessionCtx, *task.ParentSessionID, a.userID)
			sessionCancel()
			if err != nil {
				fmt.Printf("[delegation-worker] failed to get parent session: %v\n", err)
			}
		}

		taskRepo := delegationContext.Repo
		if taskRepo == nil && parentSession != nil && parentSession.Repo != nil {
			taskRepo = parentSession.Repo
		}

		if taskRepo == nil {
			OnTaskFailed(ctx, w.db, task.ID, task.UserID, "No repository specified. The delegating agent must specify which repo to work on.")
			continue
		}

		repoPattern := regexp.MustCompile(`^[a-zA-Z0-9_-]+/[a-zA-Z0-9_.-]+$`)
		if !repoPattern.MatchString(*taskRepo) {
			OnTaskFailed(ctx, w.db, task.ID, task.UserID, fmt.Sprintf("Invalid repository format: %s. Expected format: owner/repo", *taskRepo))
			continue
		}

		delegationPrefix := task.Description
		if delegationContext.Summary != "" {
			delegationPrefix = fmt.Sprintf("Context from delegating agent:\n%s\n\nTask: %s", delegationContext.Summary, task.Description)
		}

		var scopeConfig interface{}
		if len(agent.ScopeConfig) > 0 {
			var scope map[string]interface{}
			if err := json.Unmarshal(agent.ScopeConfig, &scope); err == nil {
				scopeConfig = scope
			}
		}

		baseURL := ""
		if apiKey.BaseURL != nil {
			baseURL = *apiKey.BaseURL
		}

		modelID := ""
		if apiKey.ModelID != nil {
			modelID = *apiKey.ModelID
		}

		w.activeTasksWg.Add(1)
		go func(taskID string) {
			defer w.activeTasksWg.Done()
			taskCtx, taskCancel := context.WithTimeout(context.Background(), 2*time.Hour)
			defer taskCancel()
			if err := w.runDelegatedTask(taskCtx, runDelegatedTaskOptions{
				delegatedTaskID: taskID,
				userID:          a.userID,
				agentID:         agent.ID,
				task:            delegationPrefix,
				repo:            *taskRepo,
				provider:        string(apiKey.Provider),
				apiKey:          apiKey.APIKey,
				githubToken:     githubConn.GithubToken,
				agentScope:      scopeConfig,
				baseURL:         baseURL,
				modelID:         modelID,
			}); err != nil {
				fmt.Printf("[delegation-worker] failed to run task %s: %v\n", taskID, err)
			}
		}(task.ID)
	}

	return nil
}

func (w *Worker) canStartNewSession(ctx context.Context, userID string) (bool, error) {
	count, err := w.db.CountActiveSessions(ctx, userID)
	if err != nil {
		return false, err
	}
	return count < maxConcurrentSessionsPerUser, nil
}

type runDelegatedTaskOptions struct {
	delegatedTaskID string
	userID          string
	agentID         string
	task            string
	repo            string
	provider        string
	apiKey          string
	githubToken     string
	agentScope      interface{}
	baseURL         string
	modelID         string
}

func (w *Worker) runDelegatedTask(ctx context.Context, opts runDelegatedTaskOptions) error {
	if w.executeFn == nil {
		OnTaskFailed(ctx, w.db, opts.delegatedTaskID, opts.userID, "System not ready to execute tasks. This is a configuration error. Please contact support.")
		return fmt.Errorf("execute function not set")
	}

	var sessionID string

	defer func() {
		if sessionID != "" {
			w.db.ReleaseSessionLocks(ctx, sessionID, opts.userID)
		}
	}()

	decryptedAPIKey := w.getCachedDecryptedKey(opts.userID, opts.apiKey)
	decryptedGithubToken := w.getCachedDecryptedKey(opts.userID, opts.githubToken)

	if err := w.db.UpdateDelegatedTaskStatus(ctx, opts.delegatedTaskID, opts.userID, goshared.DelegatedTaskStatusRunning, nil); err != nil {
		return fmt.Errorf("update task to running: %w", err)
	}

	err := w.executeFn(ctx, WorkRequest{
		UserID:      opts.userID,
		AgentID:     opts.agentID,
		Task:        opts.task,
		Repo:        opts.repo,
		Provider:    opts.provider,
		APIKey:      decryptedAPIKey,
		GithubToken: decryptedGithubToken,
		AgentScope:  opts.agentScope,
		BaseURL:     opts.baseURL,
		ModelID:     opts.modelID,
	})

	if err != nil {
		errorMessage := err.Error()
		if len(errorMessage) > 500 {
			errorMessage = errorMessage[:500]
		}
		OnTaskFailed(ctx, w.db, opts.delegatedTaskID, opts.userID, errorMessage)
		return err
	}

	query := `
		SELECT id, status FROM sessions
		WHERE user_id = $1 AND agent_id = $2 AND task = $3
		AND status IN ('done', 'error', 'cancelled')
		ORDER BY created_at DESC
		LIMIT 1
	`
	var session struct {
		id     string
		status string
	}
	err = w.db.QueryRowContext(ctx, query, opts.userID, opts.agentID, opts.task).Scan(&session.id, &session.status)
	if err != nil {
		OnTaskFailed(ctx, w.db, opts.delegatedTaskID, opts.userID, "No session found after execution")
		return fmt.Errorf("find session: %w", err)
	}

	sessionID = session.id
	if err := w.db.UpdateDelegatedTaskChild(ctx, opts.delegatedTaskID, opts.userID, sessionID); err != nil {
		fmt.Printf("[delegation-worker] failed to link child session: %v\n", err)
	}

	if session.status == "done" {
		usageQuery := `
			SELECT metadata FROM usage_records
			WHERE session_id = $1 AND type = 'llm_tokens'
		`
		usageRows, err := w.db.QueryContext(ctx, usageQuery, sessionID)
		if err != nil {
			fmt.Printf("[delegation-worker] failed to query usage: %v\n", err)
		} else {
			defer usageRows.Close()

			var totalCostUSD float64
			for usageRows.Next() {
				var metadataJSON []byte
				if err := usageRows.Scan(&metadataJSON); err != nil {
					continue
				}

				var metadata map[string]interface{}
				if err := json.Unmarshal(metadataJSON, &metadata); err != nil {
					continue
				}

				if costCents, ok := metadata["costCents"].(float64); ok {
					totalCostUSD += costCents / 100
				}
			}

			var actualCost *float64
			if totalCostUSD > 0 {
				actualCost = &totalCostUSD
			}
			OnTaskComplete(ctx, w.db, opts.delegatedTaskID, opts.userID, actualCost)
		}
	} else if session.status == "cancelled" {
		OnTaskFailed(ctx, w.db, opts.delegatedTaskID, opts.userID, "Task was cancelled")
	} else {
		OnTaskFailed(ctx, w.db, opts.delegatedTaskID, opts.userID, "Task execution failed")
	}

	return nil
}

func (w *Worker) getCachedDecryptedKey(userID, encryptedKey string) string {
	now := time.Now()

	w.decryptCacheMu.RLock()
	if userCache, exists := w.decryptCache[userID]; exists {
		if entry, found := userCache[encryptedKey]; found {
			if now.Before(entry.expiresAt) {
				entry.lastUsed = now
				w.decryptCacheMu.RUnlock()
				return entry.key
			}
		}
	}
	w.decryptCacheMu.RUnlock()

	decrypted, err := goshared.DecryptWithKey(encryptedKey)
	if err != nil {
		return ""
	}

	w.decryptCacheMu.Lock()
	defer w.decryptCacheMu.Unlock()

	if _, exists := w.decryptCache[userID]; !exists {
		w.decryptCache[userID] = make(map[string]*decryptCacheEntry)
	}
	w.decryptCache[userID][encryptedKey] = &decryptCacheEntry{
		key:       decrypted,
		lastUsed:  now,
		expiresAt: now.Add(decryptCacheTTL),
	}

	totalEntries := 0
	for _, cache := range w.decryptCache {
		totalEntries += len(cache)
	}

	for totalEntries > maxDecryptionCacheSize {
		w.evictOldestDecryptEntry()
		totalEntries = 0
		for _, cache := range w.decryptCache {
			totalEntries += len(cache)
		}
	}

	return decrypted
}

func (w *Worker) evictOldestDecryptEntry() {
	var oldestUserID, oldestKey string
	var oldestTime time.Time
	first := true

	for userID, userCache := range w.decryptCache {
		for key, entry := range userCache {
			if first || entry.lastUsed.Before(oldestTime) {
				oldestUserID = userID
				oldestKey = key
				oldestTime = entry.lastUsed
				first = false
			}
		}
	}

	if oldestUserID != "" {
		delete(w.decryptCache[oldestUserID], oldestKey)
		if len(w.decryptCache[oldestUserID]) == 0 {
			delete(w.decryptCache, oldestUserID)
		}
	}
}
