package goshared

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/lib/pq"
)

func (d *DB) CreateIssuePipeline(ctx context.Context, p *IssuePipeline) (*IssuePipeline, error) {
	id := GenerateID()
	now := time.Now()

	var prNumber sql.NullInt64
	if p.PrNumber != nil {
		prNumber = sql.NullInt64{Int64: int64(*p.PrNumber), Valid: true}
	}

	var prURL sql.NullString
	if p.PrURL != nil {
		prURL = sql.NullString{String: *p.PrURL, Valid: true}
	}

	var branchName sql.NullString
	if p.BranchName != nil {
		branchName = sql.NullString{String: *p.BranchName, Valid: true}
	}

	var errorMessage sql.NullString
	if p.ErrorMessage != nil {
		errorMessage = sql.NullString{String: *p.ErrorMessage, Valid: true}
	}

	var sessionID sql.NullString
	if p.SessionID != nil {
		sessionID = sql.NullString{String: *p.SessionID, Valid: true}
	}

	var startedAt sql.NullTime
	if p.StartedAt != nil {
		startedAt = sql.NullTime{Time: *p.StartedAt, Valid: true}
	}

	var finishedAt sql.NullTime
	if p.FinishedAt != nil {
		finishedAt = sql.NullTime{Time: *p.FinishedAt, Valid: true}
	}

	_, err := d.db.ExecContext(ctx, `
		INSERT INTO issue_pipelines (
			id, user_id, agent_id, repo, issue_number, issue_title, issue_body,
			issue_labels, status, pr_number, pr_url, branch_name, error_message,
			session_id, started_at, finished_at, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $17)
	`,
		id,
		p.UserID,
		p.AgentID,
		p.Repo,
		p.IssueNumber,
		p.IssueTitle,
		p.IssueBody,
		pq.Array(p.IssueLabels),
		p.Status,
		prNumber,
		prURL,
		branchName,
		errorMessage,
		sessionID,
		startedAt,
		finishedAt,
		now,
	)
	if err != nil {
		return nil, fmt.Errorf("create issue pipeline: %w", err)
	}

	created := *p
	created.ID = id
	created.CreatedAt = now
	created.UpdatedAt = now
	return &created, nil
}

func (d *DB) GetIssuePipeline(ctx context.Context, id, userID string) (*IssuePipeline, error) {
	var pipeline IssuePipeline
	var prNumber sql.NullInt64
	var prURL sql.NullString
	var branchName sql.NullString
	var errorMessage sql.NullString
	var sessionID sql.NullString
	var startedAt sql.NullTime
	var finishedAt sql.NullTime

	err := d.db.QueryRowContext(ctx, `
		SELECT id, user_id, agent_id, repo, issue_number, issue_title, issue_body,
			issue_labels, status, pr_number, pr_url, branch_name, error_message,
			session_id, started_at, finished_at, created_at, updated_at
		FROM issue_pipelines
		WHERE id = $1 AND user_id = $2
	`, id, userID).Scan(
		&pipeline.ID,
		&pipeline.UserID,
		&pipeline.AgentID,
		&pipeline.Repo,
		&pipeline.IssueNumber,
		&pipeline.IssueTitle,
		&pipeline.IssueBody,
		pq.Array(&pipeline.IssueLabels),
		&pipeline.Status,
		&prNumber,
		&prURL,
		&branchName,
		&errorMessage,
		&sessionID,
		&startedAt,
		&finishedAt,
		&pipeline.CreatedAt,
		&pipeline.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get issue pipeline: %w", err)
	}

	if prNumber.Valid {
		num := int(prNumber.Int64)
		pipeline.PrNumber = &num
	}
	if prURL.Valid {
		pipeline.PrURL = &prURL.String
	}
	if branchName.Valid {
		pipeline.BranchName = &branchName.String
	}
	if errorMessage.Valid {
		pipeline.ErrorMessage = &errorMessage.String
	}
	if sessionID.Valid {
		pipeline.SessionID = &sessionID.String
	}
	if startedAt.Valid {
		pipeline.StartedAt = &startedAt.Time
	}
	if finishedAt.Valid {
		pipeline.FinishedAt = &finishedAt.Time
	}

	return &pipeline, nil
}

func (d *DB) ListIssuePipelines(ctx context.Context, userID string, limit, offset int) ([]IssuePipeline, int, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT id, user_id, agent_id, repo, issue_number, issue_title, issue_body,
			issue_labels, status, pr_number, pr_url, branch_name, error_message,
			session_id, started_at, finished_at, created_at, updated_at,
			COUNT(*) OVER() AS total_count
		FROM issue_pipelines
		WHERE user_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`, userID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list issue pipelines: %w", err)
	}
	defer rows.Close()

	var pipelines []IssuePipeline
	var totalCount int
	for rows.Next() {
		var pipeline IssuePipeline
		var prNumber sql.NullInt64
		var prURL sql.NullString
		var branchName sql.NullString
		var errorMessage sql.NullString
		var sessionID sql.NullString
		var startedAt sql.NullTime
		var finishedAt sql.NullTime

		err := rows.Scan(
			&pipeline.ID,
			&pipeline.UserID,
			&pipeline.AgentID,
			&pipeline.Repo,
			&pipeline.IssueNumber,
			&pipeline.IssueTitle,
			&pipeline.IssueBody,
			pq.Array(&pipeline.IssueLabels),
			&pipeline.Status,
			&prNumber,
			&prURL,
			&branchName,
			&errorMessage,
			&sessionID,
			&startedAt,
			&finishedAt,
			&pipeline.CreatedAt,
			&pipeline.UpdatedAt,
			&totalCount,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("scan issue pipeline: %w", err)
		}

		if prNumber.Valid {
			num := int(prNumber.Int64)
			pipeline.PrNumber = &num
		}
		if prURL.Valid {
			pipeline.PrURL = &prURL.String
		}
		if branchName.Valid {
			pipeline.BranchName = &branchName.String
		}
		if errorMessage.Valid {
			pipeline.ErrorMessage = &errorMessage.String
		}
		if sessionID.Valid {
			pipeline.SessionID = &sessionID.String
		}
		if startedAt.Valid {
			pipeline.StartedAt = &startedAt.Time
		}
		if finishedAt.Valid {
			pipeline.FinishedAt = &finishedAt.Time
		}

		pipelines = append(pipelines, pipeline)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate issue pipelines: %w", err)
	}

	if len(pipelines) == 0 {
		totalCount = 0
	}

	return pipelines, totalCount, nil
}

func (d *DB) ListIssuePipelinesWithAgent(ctx context.Context, userID string, limit, offset int) ([]IssuePipelineWithAgent, int, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT ip.id, ip.user_id, ip.agent_id, ip.repo, ip.issue_number, ip.issue_title, ip.issue_body,
			ip.issue_labels, ip.status, ip.pr_number, ip.pr_url, ip.branch_name, ip.error_message,
			ip.session_id, ip.started_at, ip.finished_at, ip.created_at, ip.updated_at,
			agents.name AS agent_name,
			COUNT(*) OVER() AS total_count
		FROM issue_pipelines ip
		JOIN agents ON agents.id = ip.agent_id
		WHERE ip.user_id = $1
		ORDER BY ip.created_at DESC
		LIMIT $2 OFFSET $3
	`, userID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list issue pipelines with agent: %w", err)
	}
	defer rows.Close()

	var pipelines []IssuePipelineWithAgent
	var totalCount int
	for rows.Next() {
		var p IssuePipelineWithAgent
		var prNumber sql.NullInt64
		var prURL sql.NullString
		var branchName sql.NullString
		var errorMessage sql.NullString
		var sessionID sql.NullString
		var startedAt sql.NullTime
		var finishedAt sql.NullTime

		err := rows.Scan(
			&p.ID,
			&p.UserID,
			&p.AgentID,
			&p.Repo,
			&p.IssueNumber,
			&p.IssueTitle,
			&p.IssueBody,
			pq.Array(&p.IssueLabels),
			&p.Status,
			&prNumber,
			&prURL,
			&branchName,
			&errorMessage,
			&sessionID,
			&startedAt,
			&finishedAt,
			&p.CreatedAt,
			&p.UpdatedAt,
			&p.AgentName,
			&totalCount,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("scan issue pipeline with agent: %w", err)
		}

		if prNumber.Valid {
			num := int(prNumber.Int64)
			p.PrNumber = &num
		}
		if prURL.Valid {
			p.PrURL = &prURL.String
		}
		if branchName.Valid {
			p.BranchName = &branchName.String
		}
		if errorMessage.Valid {
			p.ErrorMessage = &errorMessage.String
		}
		if sessionID.Valid {
			p.SessionID = &sessionID.String
		}
		if startedAt.Valid {
			p.StartedAt = &startedAt.Time
		}
		if finishedAt.Valid {
			p.FinishedAt = &finishedAt.Time
		}

		pipelines = append(pipelines, p)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate issue pipelines with agent: %w", err)
	}

	if len(pipelines) == 0 {
		totalCount = 0
	}

	return pipelines, totalCount, nil
}

func (d *DB) UpdateIssuePipelineStatus(ctx context.Context, id, userID, status string, errorMessage *string) error {
	now := time.Now()

	var errorMsg sql.NullString
	if errorMessage != nil {
		errorMsg = sql.NullString{String: *errorMessage, Valid: true}
	}

	var startedAt, finishedAt sql.NullTime
	if status == "running" {
		startedAt = sql.NullTime{Time: now, Valid: true}
	}
	if status == "done" || status == "error" {
		finishedAt = sql.NullTime{Time: now, Valid: true}
	}

	_, err := d.db.ExecContext(ctx, `
		UPDATE issue_pipelines
		SET status = $1, error_message = $2, updated_at = $3,
			started_at = COALESCE($4, started_at),
			finished_at = COALESCE($5, finished_at)
		WHERE id = $6 AND user_id = $7
	`, status, errorMsg, now, startedAt, finishedAt, id, userID)
	if err != nil {
		return fmt.Errorf("update issue pipeline status: %w", err)
	}
	return nil
}

func (d *DB) UpdateIssuePipelineResult(ctx context.Context, id, userID string, prNumber int, prURL, branchName, sessionID string) error {
	_, err := d.db.ExecContext(ctx, `
		UPDATE issue_pipelines
		SET pr_number = $1, pr_url = $2, branch_name = $3, session_id = $4, updated_at = $5
		WHERE id = $6 AND user_id = $7
	`, prNumber, prURL, branchName, sessionID, time.Now(), id, userID)
	if err != nil {
		return fmt.Errorf("update issue pipeline result: %w", err)
	}
	return nil
}

func (d *DB) GetPendingIssuePipelines(ctx context.Context, limit int) ([]IssuePipeline, error) {
	if limit <= 0 {
		limit = 100
	}

	rows, err := d.db.QueryContext(ctx, `
		SELECT id, user_id, agent_id, repo, issue_number, issue_title, issue_body,
			issue_labels, status, pr_number, pr_url, branch_name, error_message,
			session_id, started_at, finished_at, created_at, updated_at
		FROM issue_pipelines
		WHERE status = 'pending'
		ORDER BY created_at ASC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("get pending issue pipelines: %w", err)
	}
	defer rows.Close()

	pipelines := make([]IssuePipeline, 0, limit)
	for rows.Next() {
		var pipeline IssuePipeline
		var prNumber sql.NullInt64
		var prURL sql.NullString
		var branchName sql.NullString
		var errorMessage sql.NullString
		var sessionID sql.NullString
		var startedAt sql.NullTime
		var finishedAt sql.NullTime

		err := rows.Scan(
			&pipeline.ID,
			&pipeline.UserID,
			&pipeline.AgentID,
			&pipeline.Repo,
			&pipeline.IssueNumber,
			&pipeline.IssueTitle,
			&pipeline.IssueBody,
			pq.Array(&pipeline.IssueLabels),
			&pipeline.Status,
			&prNumber,
			&prURL,
			&branchName,
			&errorMessage,
			&sessionID,
			&startedAt,
			&finishedAt,
			&pipeline.CreatedAt,
			&pipeline.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan pending issue pipeline: %w", err)
		}

		if prNumber.Valid {
			num := int(prNumber.Int64)
			pipeline.PrNumber = &num
		}
		if prURL.Valid {
			pipeline.PrURL = &prURL.String
		}
		if branchName.Valid {
			pipeline.BranchName = &branchName.String
		}
		if errorMessage.Valid {
			pipeline.ErrorMessage = &errorMessage.String
		}
		if sessionID.Valid {
			pipeline.SessionID = &sessionID.String
		}
		if startedAt.Valid {
			pipeline.StartedAt = &startedAt.Time
		}
		if finishedAt.Valid {
			pipeline.FinishedAt = &finishedAt.Time
		}

		pipelines = append(pipelines, pipeline)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending issue pipelines: %w", err)
	}

	return pipelines, nil
}
