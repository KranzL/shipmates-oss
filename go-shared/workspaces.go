package goshared

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

func (d *DB) ListWorkspaces(ctx context.Context, userID string) ([]Workspace, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT id, user_id, platform, platform_id, bot_token, bot_user_id, team_name, connected_at
		FROM workspaces
		WHERE user_id = $1
		ORDER BY connected_at DESC
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("list workspaces: %w", err)
	}
	defer rows.Close()

	var workspaces []Workspace
	for rows.Next() {
		var workspace Workspace
		err := rows.Scan(
			&workspace.ID,
			&workspace.UserID,
			&workspace.Platform,
			&workspace.PlatformID,
			&workspace.BotToken,
			&workspace.BotUserID,
			&workspace.TeamName,
			&workspace.ConnectedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan workspace: %w", err)
		}
		workspaces = append(workspaces, workspace)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate workspaces: %w", err)
	}

	return workspaces, nil
}

func (d *DB) GetWorkspace(ctx context.Context, id, userID string) (*Workspace, error) {
	var workspace Workspace
	err := d.db.QueryRowContext(ctx, `
		SELECT id, user_id, platform, platform_id, bot_token, bot_user_id, team_name, connected_at
		FROM workspaces
		WHERE id = $1 AND user_id = $2
	`, id, userID).Scan(
		&workspace.ID,
		&workspace.UserID,
		&workspace.Platform,
		&workspace.PlatformID,
		&workspace.BotToken,
		&workspace.BotUserID,
		&workspace.TeamName,
		&workspace.ConnectedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get workspace: %w", err)
	}
	return &workspace, nil
}

func (d *DB) GetWorkspaceByPlatform(ctx context.Context, platform Platform, platformID string) (*Workspace, error) {
	var workspace Workspace
	err := d.db.QueryRowContext(ctx, `
		SELECT id, user_id, platform, platform_id, bot_token, bot_user_id, team_name, connected_at
		FROM workspaces
		WHERE platform = $1 AND platform_id = $2
	`, platform, platformID).Scan(
		&workspace.ID,
		&workspace.UserID,
		&workspace.Platform,
		&workspace.PlatformID,
		&workspace.BotToken,
		&workspace.BotUserID,
		&workspace.TeamName,
		&workspace.ConnectedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get workspace by platform: %w", err)
	}
	return &workspace, nil
}

func (d *DB) CreateWorkspace(ctx context.Context, w *Workspace) (*Workspace, error) {
	id := GenerateID()
	now := time.Now()

	_, err := d.db.ExecContext(ctx, `
		INSERT INTO workspaces (id, user_id, platform, platform_id, bot_token, bot_user_id, team_name, connected_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`,
		id,
		w.UserID,
		w.Platform,
		w.PlatformID,
		w.BotToken,
		w.BotUserID,
		w.TeamName,
		now,
	)
	if err != nil {
		return nil, fmt.Errorf("create workspace: %w", err)
	}

	return &Workspace{
		ID:          id,
		UserID:      w.UserID,
		Platform:    w.Platform,
		PlatformID:  w.PlatformID,
		BotToken:    w.BotToken,
		BotUserID:   w.BotUserID,
		TeamName:    w.TeamName,
		ConnectedAt: now,
	}, nil
}

func (d *DB) UpdateWorkspace(ctx context.Context, id, userID string, botToken, botUserID, teamName *string) error {
	var setBotToken sql.NullString
	if botToken != nil {
		setBotToken = sql.NullString{String: *botToken, Valid: true}
	}

	var setBotUserID sql.NullString
	if botUserID != nil {
		setBotUserID = sql.NullString{String: *botUserID, Valid: true}
	}

	var setTeamName sql.NullString
	if teamName != nil {
		setTeamName = sql.NullString{String: *teamName, Valid: true}
	}

	_, err := d.db.ExecContext(ctx, `
		UPDATE workspaces
		SET bot_token = COALESCE($1, bot_token),
			bot_user_id = COALESCE($2, bot_user_id),
			team_name = COALESCE($3, team_name)
		WHERE id = $4 AND user_id = $5
	`, setBotToken, setBotUserID, setTeamName, id, userID)
	if err != nil {
		return fmt.Errorf("update workspace: %w", err)
	}
	return nil
}

func (d *DB) DeleteWorkspace(ctx context.Context, id, userID string) error {
	_, err := d.db.ExecContext(ctx, `
		DELETE FROM workspaces
		WHERE id = $1 AND user_id = $2
	`, id, userID)
	if err != nil {
		return fmt.Errorf("delete workspace: %w", err)
	}
	return nil
}
