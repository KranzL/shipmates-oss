package goshared

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

const MaxSessionOutputsLimit = 500

func (d *DB) CreateSessionOutput(ctx context.Context, sessionID string, sequence int, content string) error {
	id := GenerateID()
	now := time.Now()

	_, err := d.db.ExecContext(ctx, `
		INSERT INTO session_outputs (id, session_id, sequence, content, created_at)
		VALUES ($1, $2, $3, $4, $5)
	`, id, sessionID, sequence, content, now)
	if err != nil {
		return fmt.Errorf("create session output: %w", err)
	}

	return nil
}

func (d *DB) GetSessionOutputs(ctx context.Context, sessionID string, afterSequence int, limit int) ([]SessionOutput, error) {
	if limit <= 0 {
		limit = 1000
	}
	if limit > MaxSessionOutputsLimit {
		limit = MaxSessionOutputsLimit
	}

	rows, err := d.db.QueryContext(ctx, `
		SELECT id, session_id, sequence, content, created_at
		FROM session_outputs
		WHERE session_id = $1 AND sequence > $2
		ORDER BY sequence ASC
		LIMIT $3
	`, sessionID, afterSequence, limit)
	if err != nil {
		return nil, fmt.Errorf("get session outputs: %w", err)
	}
	defer rows.Close()

	outputs := []SessionOutput{}
	for rows.Next() {
		var output SessionOutput
		err := rows.Scan(&output.ID, &output.SessionID, &output.Sequence, &output.Content, &output.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("scan session output: %w", err)
		}
		outputs = append(outputs, output)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("get session outputs rows: %w", err)
	}

	return outputs, nil
}

func (d *DB) GetLatestSessionOutput(ctx context.Context, sessionID string) (*SessionOutput, error) {
	var output SessionOutput
	err := d.db.QueryRowContext(ctx, `
		SELECT id, session_id, sequence, content, created_at
		FROM session_outputs
		WHERE session_id = $1
		ORDER BY sequence DESC
		LIMIT 1
	`, sessionID).Scan(&output.ID, &output.SessionID, &output.Sequence, &output.Content, &output.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get latest session output: %w", err)
	}

	return &output, nil
}

func (d *DB) DeleteSessionOutputs(ctx context.Context, sessionID string) error {
	_, err := d.db.ExecContext(ctx, `
		DELETE FROM session_outputs
		WHERE session_id = $1
	`, sessionID)
	if err != nil {
		return fmt.Errorf("delete session outputs: %w", err)
	}

	return nil
}
