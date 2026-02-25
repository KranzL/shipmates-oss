package delegation

import (
	"context"
	"fmt"

	goshared "github.com/KranzL/shipmates-oss/go-shared"
	"github.com/KranzL/shipmates-oss/go-llm"
)

type DelegateOptions struct {
	FromSessionID string
	FromAgentID   string
	ToAgentID     string
	UserID        string
	Description   string
	Priority      *int
	BlockedBy     []string
	MaxCostUSD    *float64
	ContextMode   string
}

func Delegate(ctx context.Context, db *goshared.DB, llmClient llm.Client, opts DelegateOptions) (*goshared.DelegationResponse, error) {
	fromAgent, err := db.GetAgent(ctx, opts.FromAgentID, opts.UserID)
	if err != nil {
		return nil, fmt.Errorf("get from agent: %w", err)
	}
	if fromAgent == nil {
		return nil, fmt.Errorf("delegating agent (%s) not found", opts.FromAgentID)
	}

	toAgent, err := db.GetAgent(ctx, opts.ToAgentID, opts.UserID)
	if err != nil {
		return nil, fmt.Errorf("get to agent: %w", err)
	}
	if toAgent == nil {
		return nil, fmt.Errorf("target agent (%s) not found", opts.ToAgentID)
	}

	if fromAgent.UserID != opts.UserID {
		return nil, fmt.Errorf("you don't have access to the delegating agent")
	}
	if toAgent.UserID != opts.UserID {
		return nil, fmt.Errorf("you don't have access to the target agent")
	}

	fromAgentForValidation := AgentForValidation{
		ID:                 fromAgent.ID,
		Role:               fromAgent.Role,
		MaxCostPerTask:     fromAgent.MaxCostPerTask,
		MaxDelegationDepth: fromAgent.MaxDelegationDepth,
		CanDelegateTo:      fromAgent.CanDelegateTo,
	}

	toAgentForValidation := AgentForValidation{
		ID:                 toAgent.ID,
		Role:               toAgent.Role,
		MaxCostPerTask:     toAgent.MaxCostPerTask,
		MaxDelegationDepth: toAgent.MaxDelegationDepth,
		CanDelegateTo:      toAgent.CanDelegateTo,
	}

	validationResult := ValidateDelegation(fromAgentForValidation, toAgentForValidation, opts.Description, opts.MaxCostUSD)
	if !validationResult.Allowed {
		return nil, fmt.Errorf("%s", validationResult.Reason)
	}

	if opts.FromSessionID != "" {
		session, err := db.GetSession(ctx, opts.FromSessionID, opts.UserID)
		if err != nil || session == nil {
			return nil, fmt.Errorf("parent session not found")
		}
		if session.UserID != opts.UserID {
			return nil, fmt.Errorf("not authorized to delegate from this session")
		}

		depthResult, err := ValidateDelegationDepth(ctx, db, opts.FromSessionID, opts.UserID, fromAgent.MaxDelegationDepth)
		if err != nil {
			return nil, fmt.Errorf("validate delegation depth: %w", err)
		}
		if !depthResult.Allowed {
			return nil, fmt.Errorf("%s", depthResult.Reason)
		}
	}

	if len(opts.BlockedBy) > 0 {
		pendingStatus := goshared.DelegatedTaskStatusPending
		claimedStatus := goshared.DelegatedTaskStatusClaimed
		runningStatus := goshared.DelegatedTaskStatusRunning

		pendingTasks, _, err := db.ListDelegatedTasks(ctx, opts.UserID, &pendingStatus, 500, 0)
		if err != nil {
			return nil, fmt.Errorf("list pending tasks: %w", err)
		}
		claimedTasks, _, err := db.ListDelegatedTasks(ctx, opts.UserID, &claimedStatus, 500, 0)
		if err != nil {
			return nil, fmt.Errorf("list claimed tasks: %w", err)
		}
		runningTasks, _, err := db.ListDelegatedTasks(ctx, opts.UserID, &runningStatus, 500, 0)
		if err != nil {
			return nil, fmt.Errorf("list running tasks: %w", err)
		}

		var tasksForCycle []TaskForCycle
		for _, t := range pendingTasks {
			tasksForCycle = append(tasksForCycle, TaskForCycle{
				ID:        t.ID,
				BlockedBy: t.BlockedBy,
			})
		}
		for _, t := range claimedTasks {
			tasksForCycle = append(tasksForCycle, TaskForCycle{
				ID:        t.ID,
				BlockedBy: t.BlockedBy,
			})
		}
		for _, t := range runningTasks {
			tasksForCycle = append(tasksForCycle, TaskForCycle{
				ID:        t.ID,
				BlockedBy: t.BlockedBy,
			})
		}

		if DetectCyclesInBlockedBy("new-task", opts.BlockedBy, tasksForCycle) {
			return nil, fmt.Errorf("circular dependency detected in task dependencies. This would create a deadlock")
		}
	}

	contextMode := opts.ContextMode
	if contextMode == "" {
		contextMode = string(goshared.DelegationContextModeSummary)
	}

	context, err := BuildDelegationContext(ctx, db, llmClient, ContextBuildOptions{
		FromSessionID: opts.FromSessionID,
		ToAgentID:     opts.ToAgentID,
		Description:   opts.Description,
		Mode:          goshared.DelegationContextMode(contextMode),
	})
	if err != nil {
		return nil, fmt.Errorf("build delegation context: %w", err)
	}

	priority := 50
	if opts.Priority != nil {
		priority = *opts.Priority
	}

	var parentSessionID *string
	if opts.FromSessionID != "" {
		parentSessionID = &opts.FromSessionID
	}

	task, err := EnqueueTask(ctx, db, EnqueueTaskOptions{
		UserID:          opts.UserID,
		FromAgentID:     opts.FromAgentID,
		ToAgentID:       opts.ToAgentID,
		ParentSessionID: parentSessionID,
		Description:     opts.Description,
		Priority:        priority,
		BlockedBy:       opts.BlockedBy,
		Context:         context,
		MaxCostUSD:      opts.MaxCostUSD,
	})
	if err != nil {
		return nil, fmt.Errorf("enqueue task: %w", err)
	}

	reason := opts.Description
	if len(reason) > 500 {
		reason = reason[:500]
	}

	logErr := LogDelegation(ctx, db, LogDelegationOptions{
		UserID:        opts.UserID,
		FromSessionID: opts.FromSessionID,
		FromAgentID:   opts.FromAgentID,
		ToAgentID:     opts.ToAgentID,
		Action:        "delegated",
		Reason:        reason,
	})
	if logErr != nil {
		fmt.Printf("[delegation] failed to log delegation event: %v\n", logErr)
	}

	return &goshared.DelegationResponse{
		TaskID:      task.ID,
		Status:      goshared.DelegatedTaskStatus(task.Status),
		ToAgentID:   opts.ToAgentID,
		Description: opts.Description,
		Priority:    task.Priority,
	}, nil
}

func OnTaskComplete(ctx context.Context, db *goshared.DB, taskID, userID string, actualCostUSD *float64) error {
	task, err := db.GetDelegatedTaskByID(ctx, taskID, userID)
	if err != nil {
		return fmt.Errorf("get task: %w", err)
	}
	if task == nil {
		return nil
	}

	if err := db.CompleteTask(ctx, taskID, userID, actualCostUSD); err != nil {
		return fmt.Errorf("complete task: %w", err)
	}

	if err := ResolveDependencies(ctx, db, taskID, task.UserID); err != nil {
		return fmt.Errorf("resolve dependencies: %w", err)
	}

	var fromSessionID string
	if task.ParentSessionID != nil {
		fromSessionID = *task.ParentSessionID
	}

	logErr := LogDelegation(ctx, db, LogDelegationOptions{
		UserID:        task.UserID,
		FromSessionID: fromSessionID,
		ToSessionID:   task.ChildSessionID,
		FromAgentID:   task.FromAgentID,
		ToAgentID:     task.ToAgentID,
		Action:        "completed",
		CostUSD:       actualCostUSD,
	})
	if logErr != nil {
		fmt.Printf("[delegation] failed to log completion event: %v\n", logErr)
	}

	return nil
}

func OnTaskFailed(ctx context.Context, db *goshared.DB, taskID, userID string, errorMsg string) error {
	task, err := db.GetDelegatedTaskByID(ctx, taskID, userID)
	if err != nil {
		return fmt.Errorf("get task: %w", err)
	}
	if task == nil {
		return nil
	}

	truncatedError := errorMsg
	if len(truncatedError) > 500 {
		truncatedError = truncatedError[:500]
	}

	if err := db.UpdateDelegatedTaskStatus(ctx, taskID, userID, goshared.DelegatedTaskStatusFailed, &truncatedError); err != nil {
		return fmt.Errorf("update task status: %w", err)
	}

	if err := ResolveDependencies(ctx, db, taskID, task.UserID); err != nil {
		return fmt.Errorf("resolve dependencies: %w", err)
	}

	var fromSessionID string
	if task.ParentSessionID != nil {
		fromSessionID = *task.ParentSessionID
	}

	logErr := LogDelegation(ctx, db, LogDelegationOptions{
		UserID:        task.UserID,
		FromSessionID: fromSessionID,
		ToSessionID:   task.ChildSessionID,
		FromAgentID:   task.FromAgentID,
		ToAgentID:     task.ToAgentID,
		Action:        "failed",
		Reason:        truncatedError,
	})
	if logErr != nil {
		fmt.Printf("[delegation] failed to log failure event: %v\n", logErr)
	}

	return nil
}
