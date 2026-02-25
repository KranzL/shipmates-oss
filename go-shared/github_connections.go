package goshared

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/lib/pq"
)

func (d *DB) GetGithubConnection(ctx context.Context, userID string) (*GithubConnection, error) {
	var gc GithubConnection
	var webhookSecret sql.NullString
	var issueLabel sql.NullString

	err := d.db.QueryRowContext(ctx, `
		SELECT id, user_id, github_username, github_token, repos, webhook_secret, issue_label, created_at
		FROM github_connections
		WHERE user_id = $1
	`, userID).Scan(&gc.ID, &gc.UserID, &gc.GithubUsername, &gc.GithubToken, pq.Array(&gc.Repos), &webhookSecret, &issueLabel, &gc.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get github connection: %w", err)
	}

	if webhookSecret.Valid {
		gc.WebhookSecret = &webhookSecret.String
	}
	if issueLabel.Valid {
		gc.IssueLabel = &issueLabel.String
	}

	return &gc, nil
}

func (d *DB) CreateGithubConnection(ctx context.Context, userID, githubUsername, githubToken string, repos []string) (*GithubConnection, error) {
	id := GenerateID()
	now := time.Now()
	defaultIssueLabel := "shipmates"

	_, err := d.db.ExecContext(ctx, `
		INSERT INTO github_connections (id, user_id, github_username, github_token, repos, issue_label, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, id, userID, githubUsername, githubToken, pq.Array(repos), defaultIssueLabel, now)
	if err != nil {
		return nil, fmt.Errorf("create github connection: %w", err)
	}

	return &GithubConnection{
		ID:             id,
		UserID:         userID,
		GithubUsername: githubUsername,
		GithubToken:    githubToken,
		Repos:          repos,
		IssueLabel:     &defaultIssueLabel,
		CreatedAt:      now,
	}, nil
}

func (d *DB) UpdateGithubConnection(ctx context.Context, userID string, githubToken string, repos []string) error {
	_, err := d.db.ExecContext(ctx, `
		UPDATE github_connections
		SET github_token = $1, repos = $2
		WHERE user_id = $3
	`, githubToken, pq.Array(repos), userID)
	if err != nil {
		return fmt.Errorf("update github connection: %w", err)
	}
	return nil
}

func (d *DB) UpdateGithubConnectionRepos(ctx context.Context, userID string, repos []string) error {
	_, err := d.db.ExecContext(ctx, `
		UPDATE github_connections
		SET repos = $1
		WHERE user_id = $2
	`, pq.Array(repos), userID)
	if err != nil {
		return fmt.Errorf("update github connection repos: %w", err)
	}
	return nil
}

func (d *DB) UpdateGithubWebhookSecret(ctx context.Context, userID string, webhookSecret string) error {
	_, err := d.db.ExecContext(ctx, `
		UPDATE github_connections
		SET webhook_secret = $1
		WHERE user_id = $2
	`, webhookSecret, userID)
	if err != nil {
		return fmt.Errorf("update github webhook secret: %w", err)
	}
	return nil
}

func (d *DB) DeleteGithubConnection(ctx context.Context, userID string) error {
	_, err := d.db.ExecContext(ctx, `
		DELETE FROM github_connections
		WHERE user_id = $1
	`, userID)
	if err != nil {
		return fmt.Errorf("delete github connection: %w", err)
	}
	return nil
}

func (d *DB) GetGithubConnectionByRepo(ctx context.Context, repo string) (*GithubConnection, error) {
	var gc GithubConnection
	var webhookSecret sql.NullString
	var issueLabel sql.NullString

	err := d.db.QueryRowContext(ctx, `
		SELECT id, user_id, github_username, github_token, repos, webhook_secret, issue_label, created_at
		FROM github_connections
		WHERE $1 = ANY(repos)
	`, repo).Scan(&gc.ID, &gc.UserID, &gc.GithubUsername, &gc.GithubToken, pq.Array(&gc.Repos), &webhookSecret, &issueLabel, &gc.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get github connection by repo: %w", err)
	}

	if webhookSecret.Valid {
		gc.WebhookSecret = &webhookSecret.String
	}
	if issueLabel.Valid {
		gc.IssueLabel = &issueLabel.String
	}

	return &gc, nil
}
