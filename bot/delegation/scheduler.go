package delegation

import (
	"context"
	"encoding/json"
	"fmt"

	goshared "github.com/KranzL/shipmates-oss/go-shared"
)

const (
	maxTasksPerUser                  = 100
	maxDependencyResolutionBatch     = 500
	dependencyBatchSize              = 50
)

type EnqueueTaskOptions struct {
	UserID          string
	FromAgentID     string
	ToAgentID       string
	ParentSessionID *string
	Description     string
	Priority        int
	BlockedBy       []string
	Context         *goshared.DelegationContext
	MaxCostUSD      *float64
}

func EnqueueTask(ctx context.Context, db *goshared.DB, opts EnqueueTaskOptions) (*goshared.DelegatedTask, error) {
	var existingCount int
	query := `SELECT COUNT(*) FROM delegated_tasks WHERE user_id = $1 AND status IN ('pending', 'claimed', 'running')`
	err := db.QueryRowContext(ctx, query, opts.UserID).Scan(&existingCount)
	if err != nil {
		return nil, fmt.Errorf("count active tasks: %w", err)
	}

	if existingCount >= maxTasksPerUser {
		return nil, fmt.Errorf("task limit reached. You have %d active delegated tasks (maximum %d). Wait for tasks to complete or cancel existing tasks in the dashboard", existingCount, maxTasksPerUser)
	}

	contextJSON, err := json.Marshal(opts.Context)
	if err != nil {
		return nil, fmt.Errorf("marshal context: %w", err)
	}

	task := &goshared.DelegatedTask{
		UserID:          opts.UserID,
		FromAgentID:     opts.FromAgentID,
		ToAgentID:       opts.ToAgentID,
		ParentSessionID: opts.ParentSessionID,
		Description:     opts.Description,
		Priority:        opts.Priority,
		Status:          string(goshared.DelegatedTaskStatusPending),
		BlockedBy:       opts.BlockedBy,
		Context:         contextJSON,
		MaxCostUSD:      opts.MaxCostUSD,
	}

	if task.BlockedBy == nil {
		task.BlockedBy = []string{}
	}

	created, err := db.CreateDelegatedTask(ctx, task)
	if err != nil {
		return nil, fmt.Errorf("create delegated task: %w", err)
	}

	return created, nil
}

func ResolveDependencies(ctx context.Context, db *goshared.DB, completedTaskID, userID string) error {
	query := `
		UPDATE delegated_tasks
		SET blocked_by = array_remove(blocked_by, $1)
		WHERE user_id = $2
		AND status = $3
		AND $1 = ANY(blocked_by)
	`
	_, err := db.ExecContext(ctx, query, completedTaskID, userID, string(goshared.DelegatedTaskStatusPending))
	if err != nil {
		return fmt.Errorf("remove completed task from blocked_by arrays: %w", err)
	}
	return nil
}

func CancelTask(ctx context.Context, db *goshared.DB, taskID, userID string) error {
	task, err := db.GetDelegatedTaskByID(ctx, taskID, userID)
	if err != nil {
		return fmt.Errorf("get task: %w", err)
	}
	if task == nil {
		return nil
	}

	cancellableStatuses := map[goshared.DelegatedTaskStatus]bool{
		goshared.DelegatedTaskStatusPending: true,
		goshared.DelegatedTaskStatusClaimed: true,
		goshared.DelegatedTaskStatusRunning: true,
	}

	if !cancellableStatuses[goshared.DelegatedTaskStatus(task.Status)] {
		return nil
	}

	if err := db.UpdateDelegatedTaskStatus(ctx, taskID, userID, goshared.DelegatedTaskStatusCancelled, nil); err != nil {
		return fmt.Errorf("update task status: %w", err)
	}

	if task.ChildSessionID != nil {
		query := `
			UPDATE sessions
			SET status = $1
			WHERE id = $2 AND status IN ('pending', 'running', 'waiting_for_clarification')
		`
		_, execErr := db.ExecContext(ctx, query, string(goshared.SessionStatusCancelling), *task.ChildSessionID)
		if execErr != nil {
			fmt.Printf("[delegation] failed to cancel child session: %v\n", execErr)
		}
	}

	if err := ResolveDependencies(ctx, db, taskID, task.UserID); err != nil {
		return fmt.Errorf("resolve dependencies: %w", err)
	}

	return nil
}
