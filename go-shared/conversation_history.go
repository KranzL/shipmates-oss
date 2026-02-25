package goshared

import (
	"context"
	"fmt"
	"time"
)

func (d *DB) AddConversationMessage(ctx context.Context, workspaceID, agentID, role, content string) error {
	id := GenerateID()
	now := time.Now()

	_, err := d.db.ExecContext(ctx, `
		INSERT INTO conversation_history (id, workspace_id, agent_id, role, content, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, id, workspaceID, agentID, role, content, now)
	if err != nil {
		return fmt.Errorf("add conversation message: %w", err)
	}

	return nil
}

func (d *DB) GetConversationHistory(ctx context.Context, workspaceID, agentID string, limit int) ([]ConversationHistory, error) {
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}

	rows, err := d.db.QueryContext(ctx, `
		SELECT id, workspace_id, agent_id, role, content, created_at
		FROM (
			SELECT id, workspace_id, agent_id, role, content, created_at
			FROM conversation_history
			WHERE workspace_id = $1 AND agent_id = $2
			ORDER BY created_at DESC
			LIMIT $3
		) sub
		ORDER BY created_at ASC
	`, workspaceID, agentID, limit)
	if err != nil {
		return nil, fmt.Errorf("get conversation history: %w", err)
	}
	defer rows.Close()

	var messages []ConversationHistory
	for rows.Next() {
		var message ConversationHistory
		err := rows.Scan(
			&message.ID,
			&message.WorkspaceID,
			&message.AgentID,
			&message.Role,
			&message.Content,
			&message.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan conversation message: %w", err)
		}
		messages = append(messages, message)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate conversation history: %w", err)
	}

	return messages, nil
}

func (d *DB) ClearConversationHistory(ctx context.Context, workspaceID, agentID string) error {
	_, err := d.db.ExecContext(ctx, `
		DELETE FROM conversation_history
		WHERE workspace_id = $1 AND agent_id = $2
	`, workspaceID, agentID)
	if err != nil {
		return fmt.Errorf("clear conversation history: %w", err)
	}

	return nil
}

func (d *DB) TrimConversationHistory(ctx context.Context, workspaceID, agentID string, keepCount int) error {
	_, err := d.db.ExecContext(ctx, `
		DELETE FROM conversation_history
		WHERE workspace_id = $1 AND agent_id = $2
		AND id NOT IN (
			SELECT id FROM conversation_history
			WHERE workspace_id = $1 AND agent_id = $2
			ORDER BY created_at DESC
			LIMIT $3
		)
	`, workspaceID, agentID, keepCount)
	if err != nil {
		return fmt.Errorf("trim conversation history: %w", err)
	}

	return nil
}
