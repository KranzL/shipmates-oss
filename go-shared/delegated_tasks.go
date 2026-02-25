package goshared

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/lib/pq"
)

func (d *DB) CreateDelegatedTask(ctx context.Context, t *DelegatedTask) (*DelegatedTask, error) {
	id := GenerateID()
	now := time.Now()

	var parentSessionID sql.NullString
	if t.ParentSessionID != nil {
		parentSessionID = sql.NullString{String: *t.ParentSessionID, Valid: true}
	}

	var childSessionID sql.NullString
	if t.ChildSessionID != nil {
		childSessionID = sql.NullString{String: *t.ChildSessionID, Valid: true}
	}

	var maxCostUSD sql.NullFloat64
	if t.MaxCostUSD != nil {
		maxCostUSD = sql.NullFloat64{Float64: *t.MaxCostUSD, Valid: true}
	}

	var actualCostUSD sql.NullFloat64
	if t.ActualCostUSD != nil {
		actualCostUSD = sql.NullFloat64{Float64: *t.ActualCostUSD, Valid: true}
	}

	var errorMessage sql.NullString
	if t.ErrorMessage != nil {
		errorMessage = sql.NullString{String: *t.ErrorMessage, Valid: true}
	}

	_, err := d.db.ExecContext(ctx, `
		INSERT INTO delegated_tasks (
			id, user_id, parent_session_id, child_session_id, from_agent_id, to_agent_id,
			description, priority, status, error_message, blocked_by, context,
			max_cost_usd, actual_cost_usd, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
	`,
		id,
		t.UserID,
		parentSessionID,
		childSessionID,
		t.FromAgentID,
		t.ToAgentID,
		t.Description,
		t.Priority,
		t.Status,
		errorMessage,
		pq.Array(t.BlockedBy),
		t.Context,
		maxCostUSD,
		actualCostUSD,
		now,
	)
	if err != nil {
		return nil, fmt.Errorf("create delegated task: %w", err)
	}

	created := *t
	created.ID = id
	created.CreatedAt = now
	return &created, nil
}

func (d *DB) GetDelegatedTask(ctx context.Context, id, userID string) (*DelegatedTask, error) {
	var task DelegatedTask
	var parentSessionID sql.NullString
	var childSessionID sql.NullString
	var errorMessage sql.NullString
	var maxCostUSD sql.NullFloat64
	var actualCostUSD sql.NullFloat64
	var claimedAt sql.NullTime
	var completedAt sql.NullTime

	err := d.db.QueryRowContext(ctx, `
		SELECT id, user_id, parent_session_id, child_session_id, from_agent_id, to_agent_id,
			description, priority, status, error_message, blocked_by, context,
			max_cost_usd, actual_cost_usd, claimed_at, completed_at, created_at
		FROM delegated_tasks
		WHERE id = $1 AND user_id = $2
	`, id, userID).Scan(
		&task.ID,
		&task.UserID,
		&parentSessionID,
		&childSessionID,
		&task.FromAgentID,
		&task.ToAgentID,
		&task.Description,
		&task.Priority,
		&task.Status,
		&errorMessage,
		pq.Array(&task.BlockedBy),
		&task.Context,
		&maxCostUSD,
		&actualCostUSD,
		&claimedAt,
		&completedAt,
		&task.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get delegated task: %w", err)
	}

	if parentSessionID.Valid {
		task.ParentSessionID = &parentSessionID.String
	}
	if childSessionID.Valid {
		task.ChildSessionID = &childSessionID.String
	}
	if errorMessage.Valid {
		task.ErrorMessage = &errorMessage.String
	}
	if maxCostUSD.Valid {
		task.MaxCostUSD = &maxCostUSD.Float64
	}
	if actualCostUSD.Valid {
		task.ActualCostUSD = &actualCostUSD.Float64
	}
	if claimedAt.Valid {
		task.ClaimedAt = &claimedAt.Time
	}
	if completedAt.Valid {
		task.CompletedAt = &completedAt.Time
	}

	return &task, nil
}

func (d *DB) GetDelegatedTaskByID(ctx context.Context, id, userID string) (*DelegatedTask, error) {
	var task DelegatedTask
	var parentSessionID sql.NullString
	var childSessionID sql.NullString
	var errorMessage sql.NullString
	var maxCostUSD sql.NullFloat64
	var actualCostUSD sql.NullFloat64
	var claimedAt sql.NullTime
	var completedAt sql.NullTime

	err := d.db.QueryRowContext(ctx, `
		SELECT id, user_id, parent_session_id, child_session_id, from_agent_id, to_agent_id,
			description, priority, status, error_message, blocked_by, context,
			max_cost_usd, actual_cost_usd, claimed_at, completed_at, created_at
		FROM delegated_tasks
		WHERE id = $1 AND user_id = $2
	`, id, userID).Scan(
		&task.ID,
		&task.UserID,
		&parentSessionID,
		&childSessionID,
		&task.FromAgentID,
		&task.ToAgentID,
		&task.Description,
		&task.Priority,
		&task.Status,
		&errorMessage,
		pq.Array(&task.BlockedBy),
		&task.Context,
		&maxCostUSD,
		&actualCostUSD,
		&claimedAt,
		&completedAt,
		&task.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get delegated task by id: %w", err)
	}

	if parentSessionID.Valid {
		task.ParentSessionID = &parentSessionID.String
	}
	if childSessionID.Valid {
		task.ChildSessionID = &childSessionID.String
	}
	if errorMessage.Valid {
		task.ErrorMessage = &errorMessage.String
	}
	if maxCostUSD.Valid {
		task.MaxCostUSD = &maxCostUSD.Float64
	}
	if actualCostUSD.Valid {
		task.ActualCostUSD = &actualCostUSD.Float64
	}
	if claimedAt.Valid {
		task.ClaimedAt = &claimedAt.Time
	}
	if completedAt.Valid {
		task.CompletedAt = &completedAt.Time
	}

	return &task, nil
}

func (d *DB) ListDelegatedTasks(ctx context.Context, userID string, status *DelegatedTaskStatus, limit, offset int) ([]DelegatedTask, int, error) {
	query := `
		SELECT id, user_id, parent_session_id, child_session_id, from_agent_id, to_agent_id,
			description, priority, status, error_message, blocked_by, context,
			max_cost_usd, actual_cost_usd, claimed_at, completed_at, created_at,
			COUNT(*) OVER() AS total_count
		FROM delegated_tasks
		WHERE user_id = $1
	`
	args := []interface{}{userID}
	argIndex := 2

	if status != nil {
		query += fmt.Sprintf(` AND status = $%d`, argIndex)
		args = append(args, string(*status))
		argIndex++
	}

	query += ` ORDER BY created_at DESC`
	query += fmt.Sprintf(` LIMIT $%d OFFSET $%d`, argIndex, argIndex+1)
	args = append(args, limit, offset)

	rows, err := d.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list delegated tasks: %w", err)
	}
	defer rows.Close()

	var tasks []DelegatedTask
	var totalCount int
	for rows.Next() {
		var task DelegatedTask
		var parentSessionID sql.NullString
		var childSessionID sql.NullString
		var errorMessage sql.NullString
		var maxCostUSD sql.NullFloat64
		var actualCostUSD sql.NullFloat64
		var claimedAt sql.NullTime
		var completedAt sql.NullTime

		err := rows.Scan(
			&task.ID,
			&task.UserID,
			&parentSessionID,
			&childSessionID,
			&task.FromAgentID,
			&task.ToAgentID,
			&task.Description,
			&task.Priority,
			&task.Status,
			&errorMessage,
			pq.Array(&task.BlockedBy),
			&task.Context,
			&maxCostUSD,
			&actualCostUSD,
			&claimedAt,
			&completedAt,
			&task.CreatedAt,
			&totalCount,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("scan delegated task: %w", err)
		}

		if parentSessionID.Valid {
			task.ParentSessionID = &parentSessionID.String
		}
		if childSessionID.Valid {
			task.ChildSessionID = &childSessionID.String
		}
		if errorMessage.Valid {
			task.ErrorMessage = &errorMessage.String
		}
		if maxCostUSD.Valid {
			task.MaxCostUSD = &maxCostUSD.Float64
		}
		if actualCostUSD.Valid {
			task.ActualCostUSD = &actualCostUSD.Float64
		}
		if claimedAt.Valid {
			task.ClaimedAt = &claimedAt.Time
		}
		if completedAt.Valid {
			task.CompletedAt = &completedAt.Time
		}

		tasks = append(tasks, task)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate delegated tasks: %w", err)
	}

	if len(tasks) == 0 {
		totalCount = 0
	}

	return tasks, totalCount, nil
}

func (d *DB) UpdateDelegatedTaskStatus(ctx context.Context, id, userID string, status DelegatedTaskStatus, errorMessage *string) error {
	now := time.Now()

	var errorMsg sql.NullString
	if errorMessage != nil {
		errorMsg = sql.NullString{String: *errorMessage, Valid: true}
	}

	var claimedAt sql.NullTime
	var completedAt sql.NullTime

	if status == DelegatedTaskStatusClaimed {
		claimedAt = sql.NullTime{Time: now, Valid: true}
	}

	if status == DelegatedTaskStatusDone || status == DelegatedTaskStatusFailed || status == DelegatedTaskStatusCancelled {
		completedAt = sql.NullTime{Time: now, Valid: true}
	}

	query := `
		UPDATE delegated_tasks
		SET status = $1, error_message = $2
	`
	args := []interface{}{status, errorMsg}
	argIndex := 3

	if claimedAt.Valid {
		query += fmt.Sprintf(`, claimed_at = $%d`, argIndex)
		args = append(args, claimedAt)
		argIndex++
	}

	if completedAt.Valid {
		query += fmt.Sprintf(`, completed_at = $%d`, argIndex)
		args = append(args, completedAt)
		argIndex++
	}

	query += fmt.Sprintf(` WHERE id = $%d AND user_id = $%d`, argIndex, argIndex+1)
	args = append(args, id, userID)

	_, err := d.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("update delegated task status: %w", err)
	}
	return nil
}

func (d *DB) UpdateDelegatedTaskChild(ctx context.Context, id, userID, childSessionID string) error {
	_, err := d.db.ExecContext(ctx, `
		UPDATE delegated_tasks
		SET child_session_id = $1
		WHERE id = $2 AND user_id = $3
	`, childSessionID, id, userID)
	if err != nil {
		return fmt.Errorf("update delegated task child: %w", err)
	}
	return nil
}

func (d *DB) UpdateDelegatedTaskCost(ctx context.Context, id, userID string, costUSD float64) error {
	_, err := d.db.ExecContext(ctx, `
		UPDATE delegated_tasks
		SET actual_cost_usd = $1
		WHERE id = $2 AND user_id = $3
	`, costUSD, id, userID)
	if err != nil {
		return fmt.Errorf("update delegated task cost: %w", err)
	}
	return nil
}

func (d *DB) UpdateDelegatedTaskPriority(ctx context.Context, id string, priority int) error {
	_, err := d.db.ExecContext(ctx, `
		UPDATE delegated_tasks
		SET priority = $1
		WHERE id = $2
	`, priority, id)
	if err != nil {
		return fmt.Errorf("update delegated task priority: %w", err)
	}
	return nil
}

func (d *DB) GetDelegationChain(ctx context.Context, taskID, userID string) ([]DelegatedTask, error) {
	rows, err := d.db.QueryContext(ctx, `
		WITH RECURSIVE chain AS (
			SELECT id, user_id, parent_session_id, child_session_id, from_agent_id, to_agent_id,
				description, priority, status, error_message, blocked_by, context,
				max_cost_usd, actual_cost_usd, claimed_at, completed_at, created_at,
				0 as depth
			FROM delegated_tasks
			WHERE id = $1 AND user_id = $2

			UNION ALL

			SELECT dt.id, dt.user_id, dt.parent_session_id, dt.child_session_id, dt.from_agent_id, dt.to_agent_id,
				dt.description, dt.priority, dt.status, dt.error_message, dt.blocked_by, dt.context,
				dt.max_cost_usd, dt.actual_cost_usd, dt.claimed_at, dt.completed_at, dt.created_at,
				c.depth + 1
			FROM delegated_tasks dt
			INNER JOIN chain c ON (dt.parent_session_id = c.child_session_id OR dt.child_session_id = c.parent_session_id)
			WHERE dt.user_id = $2 AND c.depth < 20
		)
		SELECT DISTINCT ON (id) id, user_id, parent_session_id, child_session_id, from_agent_id, to_agent_id,
			description, priority, status, error_message, blocked_by, context,
			max_cost_usd, actual_cost_usd, claimed_at, completed_at, created_at
		FROM chain
		ORDER BY id, depth
	`, taskID, userID)
	if err != nil {
		return nil, fmt.Errorf("get delegation chain: %w", err)
	}
	defer rows.Close()

	var tasks []DelegatedTask
	for rows.Next() {
		var task DelegatedTask
		var parentSessionID sql.NullString
		var childSessionID sql.NullString
		var errorMessage sql.NullString
		var maxCostUSD sql.NullFloat64
		var actualCostUSD sql.NullFloat64
		var claimedAt sql.NullTime
		var completedAt sql.NullTime

		err := rows.Scan(
			&task.ID,
			&task.UserID,
			&parentSessionID,
			&childSessionID,
			&task.FromAgentID,
			&task.ToAgentID,
			&task.Description,
			&task.Priority,
			&task.Status,
			&errorMessage,
			pq.Array(&task.BlockedBy),
			&task.Context,
			&maxCostUSD,
			&actualCostUSD,
			&claimedAt,
			&completedAt,
			&task.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan delegation chain task: %w", err)
		}

		if parentSessionID.Valid {
			task.ParentSessionID = &parentSessionID.String
		}
		if childSessionID.Valid {
			task.ChildSessionID = &childSessionID.String
		}
		if errorMessage.Valid {
			task.ErrorMessage = &errorMessage.String
		}
		if maxCostUSD.Valid {
			task.MaxCostUSD = &maxCostUSD.Float64
		}
		if actualCostUSD.Valid {
			task.ActualCostUSD = &actualCostUSD.Float64
		}
		if claimedAt.Valid {
			task.ClaimedAt = &claimedAt.Time
		}
		if completedAt.Valid {
			task.CompletedAt = &completedAt.Time
		}

		tasks = append(tasks, task)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate delegation chain: %w", err)
	}

	return tasks, nil
}

func (d *DB) GetDelegationStats(ctx context.Context, userID string) (*DelegationStats, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT
			status,
			COUNT(*) as count,
			COALESCE(SUM(actual_cost_usd), 0) as total_cost,
			AVG(EXTRACT(EPOCH FROM (completed_at - claimed_at)) * 1000) as avg_duration_ms
		FROM delegated_tasks
		WHERE user_id = $1
		GROUP BY status
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("get delegation stats: %w", err)
	}
	defer rows.Close()

	byStatus := make(map[DelegatedTaskStatus]int)
	var totalTasks int
	var totalCostUSD float64
	var avgDurationMs *float64
	var durationSum float64
	var durationCount int

	for rows.Next() {
		var status string
		var count int
		var totalCost float64
		var avgDuration sql.NullFloat64

		if err := rows.Scan(&status, &count, &totalCost, &avgDuration); err != nil {
			return nil, fmt.Errorf("scan delegation stats: %w", err)
		}

		byStatus[DelegatedTaskStatus(status)] = count
		totalTasks += count
		totalCostUSD += totalCost

		if avgDuration.Valid {
			durationSum += avgDuration.Float64 * float64(count)
			durationCount += count
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate delegation stats: %w", err)
	}

	if durationCount > 0 {
		avg := durationSum / float64(durationCount)
		avgDurationMs = &avg
	}

	return &DelegationStats{
		TotalTasks:        totalTasks,
		ByStatus:          byStatus,
		TotalCostUSD:      totalCostUSD,
		AverageDurationMs: avgDurationMs,
	}, nil
}

func (d *DB) ClaimNextTask(ctx context.Context, toAgentID string) (*DelegatedTask, error) {
	var task DelegatedTask
	var parentSessionID sql.NullString
	var childSessionID sql.NullString
	var errorMessage sql.NullString
	var maxCostUSD sql.NullFloat64
	var actualCostUSD sql.NullFloat64
	var claimedAt sql.NullTime
	var completedAt sql.NullTime

	err := d.db.QueryRowContext(ctx, `
		UPDATE delegated_tasks
		SET status = 'claimed', claimed_at = NOW()
		WHERE id = (
			SELECT id FROM delegated_tasks
			WHERE to_agent_id = $1 AND status = 'pending'
			AND NOT EXISTS (
				SELECT 1 FROM unnest(blocked_by) AS bid
				WHERE EXISTS (
					SELECT 1 FROM delegated_tasks
					WHERE id = bid AND status != 'done'
				)
			)
			ORDER BY priority DESC, created_at ASC
			LIMIT 1
			FOR UPDATE SKIP LOCKED
		)
		RETURNING id, user_id, parent_session_id, child_session_id, from_agent_id, to_agent_id,
			description, priority, status, error_message, blocked_by, context,
			max_cost_usd, actual_cost_usd, claimed_at, completed_at, created_at
	`, toAgentID).Scan(
		&task.ID,
		&task.UserID,
		&parentSessionID,
		&childSessionID,
		&task.FromAgentID,
		&task.ToAgentID,
		&task.Description,
		&task.Priority,
		&task.Status,
		&errorMessage,
		pq.Array(&task.BlockedBy),
		&task.Context,
		&maxCostUSD,
		&actualCostUSD,
		&claimedAt,
		&completedAt,
		&task.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claim next task: %w", err)
	}

	if parentSessionID.Valid {
		task.ParentSessionID = &parentSessionID.String
	}
	if childSessionID.Valid {
		task.ChildSessionID = &childSessionID.String
	}
	if errorMessage.Valid {
		task.ErrorMessage = &errorMessage.String
	}
	if maxCostUSD.Valid {
		task.MaxCostUSD = &maxCostUSD.Float64
	}
	if actualCostUSD.Valid {
		task.ActualCostUSD = &actualCostUSD.Float64
	}
	if claimedAt.Valid {
		task.ClaimedAt = &claimedAt.Time
	}
	if completedAt.Valid {
		task.CompletedAt = &completedAt.Time
	}

	return &task, nil
}

func (d *DB) CompleteTask(ctx context.Context, id, userID string, actualCostUSD *float64) error {
	now := time.Now()

	var costUSD sql.NullFloat64
	if actualCostUSD != nil {
		costUSD = sql.NullFloat64{Float64: *actualCostUSD, Valid: true}
	}

	_, err := d.db.ExecContext(ctx, `
		UPDATE delegated_tasks
		SET status = 'done', completed_at = $1, actual_cost_usd = $2
		WHERE id = $3 AND user_id = $4
	`, now, costUSD, id, userID)
	if err != nil {
		return fmt.Errorf("complete task: %w", err)
	}
	return nil
}

func (d *DB) DetectDelegationCycle(ctx context.Context, userID, fromAgentID, toAgentID string) (bool, error) {
	var hasCycle bool
	err := d.db.QueryRowContext(ctx, `
		WITH RECURSIVE delegation_chain AS (
			SELECT
				from_agent_id,
				to_agent_id,
				ARRAY[from_agent_id] AS path,
				1 AS depth
			FROM delegated_tasks
			WHERE user_id = $1 AND to_agent_id = $2 AND status IN ('pending', 'claimed', 'running')

			UNION ALL

			SELECT
				dt.from_agent_id,
				dt.to_agent_id,
				dc.path || dt.from_agent_id,
				dc.depth + 1
			FROM delegated_tasks dt
			INNER JOIN delegation_chain dc ON dt.to_agent_id = dc.from_agent_id
			WHERE dt.user_id = $1
				AND dt.status IN ('pending', 'claimed', 'running')
				AND NOT (dt.from_agent_id = ANY(dc.path))
				AND dc.depth < 20
		)
		SELECT EXISTS (
			SELECT 1 FROM delegation_chain WHERE from_agent_id = $3
		)
	`, userID, toAgentID, fromAgentID).Scan(&hasCycle)

	if err != nil {
		return false, fmt.Errorf("detect delegation cycle: %w", err)
	}

	return hasCycle, nil
}

func (d *DB) ListDelegatedTasksWithDetails(ctx context.Context, userID string, filter DelegatedTaskFilter, limit, offset int) ([]DelegatedTaskListItem, int, error) {
	var orderByClause string
	switch filter.SortBy {
	case "priority":
		if filter.SortOrder == "asc" {
			orderByClause = " ORDER BY dt.priority ASC"
		} else {
			orderByClause = " ORDER BY dt.priority DESC"
		}
	case "completedAt":
		if filter.SortOrder == "asc" {
			orderByClause = " ORDER BY dt.completed_at ASC"
		} else {
			orderByClause = " ORDER BY dt.completed_at DESC"
		}
	default:
		if filter.SortOrder == "asc" {
			orderByClause = " ORDER BY dt.created_at ASC"
		} else {
			orderByClause = " ORDER BY dt.created_at DESC"
		}
	}

	query := `
		SELECT
			dt.id, dt.user_id, dt.parent_session_id, dt.child_session_id, dt.from_agent_id, dt.to_agent_id,
			dt.description, dt.priority, dt.status, dt.error_message, dt.blocked_by, dt.context,
			dt.max_cost_usd, dt.actual_cost_usd, dt.claimed_at, dt.completed_at, dt.created_at,
			from_agent.name as from_agent_name, from_agent.role as from_agent_role,
			to_agent.name as to_agent_name, to_agent.role as to_agent_role,
			EXISTS (
				SELECT 1 FROM unnest(dt.blocked_by) as blocker_id
				WHERE EXISTS (
					SELECT 1 FROM delegated_tasks dt2
					WHERE dt2.id = blocker_id AND dt2.status != 'done'
				)
			) as is_blocked,
			CASE
				WHEN dt.claimed_at IS NOT NULL AND dt.completed_at IS NOT NULL
				THEN EXTRACT(EPOCH FROM (dt.completed_at - dt.claimed_at)) * 1000
				ELSE NULL
			END as duration_ms,
			ps.task as parent_session_task,
			ps.status as parent_session_status,
			cs.task as child_session_task,
			cs.status as child_session_status,
			cs.branch_name as child_branch_name,
			cs.pr_url as child_pr_url,
			COUNT(*) OVER() AS total_count
		FROM delegated_tasks dt
		JOIN agents from_agent ON dt.from_agent_id = from_agent.id
		JOIN agents to_agent ON dt.to_agent_id = to_agent.id
		LEFT JOIN sessions ps ON ps.id = dt.parent_session_id
		LEFT JOIN sessions cs ON cs.id = dt.child_session_id
		WHERE dt.user_id = $1
	`
	args := []interface{}{userID}
	argIndex := 2

	if filter.Status != nil {
		query += fmt.Sprintf(` AND dt.status = $%d`, argIndex)
		args = append(args, string(*filter.Status))
		argIndex++
	}
	if filter.FromAgentID != nil {
		query += fmt.Sprintf(` AND dt.from_agent_id = $%d`, argIndex)
		args = append(args, *filter.FromAgentID)
		argIndex++
	}
	if filter.ToAgentID != nil {
		query += fmt.Sprintf(` AND dt.to_agent_id = $%d`, argIndex)
		args = append(args, *filter.ToAgentID)
		argIndex++
	}

	query += orderByClause
	query += fmt.Sprintf(` LIMIT $%d OFFSET $%d`, argIndex, argIndex+1)
	args = append(args, limit, offset)

	rows, err := d.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list delegated tasks with details: %w", err)
	}
	defer rows.Close()

	var tasks []DelegatedTaskListItem
	var totalCount int
	for rows.Next() {
		var item DelegatedTaskListItem
		var parentSessionID sql.NullString
		var childSessionID sql.NullString
		var errorMessage sql.NullString
		var maxCostUSD sql.NullFloat64
		var actualCostUSD sql.NullFloat64
		var claimedAt sql.NullTime
		var completedAt sql.NullTime
		var durationMs sql.NullFloat64
		var parentSessionTask sql.NullString
		var parentSessionStatus sql.NullString
		var childSessionTask sql.NullString
		var childSessionStatus sql.NullString
		var childBranchName sql.NullString
		var childPrURL sql.NullString

		err := rows.Scan(
			&item.ID,
			&item.UserID,
			&parentSessionID,
			&childSessionID,
			&item.FromAgentID,
			&item.ToAgentID,
			&item.Description,
			&item.Priority,
			&item.Status,
			&errorMessage,
			pq.Array(&item.BlockedBy),
			&item.Context,
			&maxCostUSD,
			&actualCostUSD,
			&claimedAt,
			&completedAt,
			&item.CreatedAt,
			&item.FromAgentName,
			&item.FromAgentRole,
			&item.ToAgentName,
			&item.ToAgentRole,
			&item.IsBlocked,
			&durationMs,
			&parentSessionTask,
			&parentSessionStatus,
			&childSessionTask,
			&childSessionStatus,
			&childBranchName,
			&childPrURL,
			&totalCount,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("scan delegated task with details: %w", err)
		}

		if parentSessionID.Valid {
			item.ParentSessionID = &parentSessionID.String
		}
		if childSessionID.Valid {
			item.ChildSessionID = &childSessionID.String
		}
		if errorMessage.Valid {
			item.ErrorMessage = &errorMessage.String
		}
		if maxCostUSD.Valid {
			item.MaxCostUSD = &maxCostUSD.Float64
		}
		if actualCostUSD.Valid {
			item.ActualCostUSD = &actualCostUSD.Float64
		}
		if claimedAt.Valid {
			item.ClaimedAt = &claimedAt.Time
		}
		if completedAt.Valid {
			item.CompletedAt = &completedAt.Time
		}
		if durationMs.Valid {
			durationMsInt := int64(durationMs.Float64)
			item.DurationMs = &durationMsInt
		}
		if parentSessionTask.Valid {
			item.ParentSessionTask = &parentSessionTask.String
		}
		if parentSessionStatus.Valid {
			item.ParentSessionStatus = &parentSessionStatus.String
		}
		if childSessionTask.Valid {
			item.ChildSessionTask = &childSessionTask.String
		}
		if childSessionStatus.Valid {
			item.ChildSessionStatus = &childSessionStatus.String
		}
		if childBranchName.Valid {
			item.ChildBranchName = &childBranchName.String
		}
		if childPrURL.Valid {
			item.ChildPrURL = &childPrURL.String
		}

		tasks = append(tasks, item)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate delegated tasks with details: %w", err)
	}

	if len(tasks) == 0 {
		totalCount = 0
	}

	return tasks, totalCount, nil
}
