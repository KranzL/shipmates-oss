package goshared

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

func (d *DB) CreateEngineExecution(ctx context.Context, e *EngineExecution) error {
	id := GenerateID()
	now := time.Now()

	var taskType sql.NullString
	if e.TaskType != nil {
		taskType = sql.NullString{String: *e.TaskType, Valid: true}
	}

	_, err := d.db.ExecContext(ctx, `
		INSERT INTO engine_executions (
			id, session_id, provider, model_id, strategy, edit_format, context_strategy,
			input_tokens, output_tokens, cost_cents, latency_ms, retry_count,
			files_read, files_modified, task_type, success, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
	`,
		id,
		e.SessionID,
		e.Provider,
		e.ModelID,
		e.Strategy,
		e.EditFormat,
		e.ContextStrategy,
		e.InputTokens,
		e.OutputTokens,
		e.CostCents,
		e.LatencyMs,
		e.RetryCount,
		e.FilesRead,
		e.FilesModified,
		taskType,
		e.Success,
		now,
	)
	if err != nil {
		return fmt.Errorf("create engine execution: %w", err)
	}

	return nil
}

func (d *DB) GetEngineExecutions(ctx context.Context, sessionID string) ([]EngineExecution, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT id, session_id, provider, model_id, strategy, edit_format, context_strategy,
			input_tokens, output_tokens, cost_cents, latency_ms, retry_count,
			files_read, files_modified, task_type, success, created_at
		FROM engine_executions
		WHERE session_id = $1
		ORDER BY created_at
	`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("get engine executions: %w", err)
	}
	defer rows.Close()

	var executions []EngineExecution
	for rows.Next() {
		var execution EngineExecution
		err := rows.Scan(
			&execution.ID,
			&execution.SessionID,
			&execution.Provider,
			&execution.ModelID,
			&execution.Strategy,
			&execution.EditFormat,
			&execution.ContextStrategy,
			&execution.InputTokens,
			&execution.OutputTokens,
			&execution.CostCents,
			&execution.LatencyMs,
			&execution.RetryCount,
			&execution.FilesRead,
			&execution.FilesModified,
			&execution.TaskType,
			&execution.Success,
			&execution.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan engine execution: %w", err)
		}
		executions = append(executions, execution)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate engine executions: %w", err)
	}

	return executions, nil
}
