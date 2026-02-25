package goshared

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

const MaxDueSchedulesPerBatch = 100

type AgentScheduleWithAgent struct {
	AgentSchedule
	AgentName string `json:"agentName"`
}

func (d *DB) ListSchedules(ctx context.Context, userID string) ([]AgentScheduleWithAgent, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT s.id, s.user_id, s.agent_id, s.name, s.task, s.cron_expression, s.timezone,
			s.is_active, s.next_run_at, s.last_run_at, s.last_status, s.run_count, s.failure_count,
			s.created_at, s.updated_at, a.name as agent_name
		FROM agent_schedules s
		JOIN agents a ON s.agent_id = a.id
		WHERE s.user_id = $1
		ORDER BY s.created_at DESC
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("list schedules: %w", err)
	}
	defer rows.Close()

	schedules := make([]AgentScheduleWithAgent, 0, 20)
	for rows.Next() {
		var schedule AgentScheduleWithAgent
		err := rows.Scan(
			&schedule.ID,
			&schedule.UserID,
			&schedule.AgentID,
			&schedule.Name,
			&schedule.Task,
			&schedule.CronExpression,
			&schedule.Timezone,
			&schedule.IsActive,
			&schedule.NextRunAt,
			&schedule.LastRunAt,
			&schedule.LastStatus,
			&schedule.RunCount,
			&schedule.FailureCount,
			&schedule.CreatedAt,
			&schedule.UpdatedAt,
			&schedule.AgentName,
		)
		if err != nil {
			return nil, fmt.Errorf("scan schedule: %w", err)
		}
		schedules = append(schedules, schedule)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate schedules: %w", err)
	}

	return schedules, nil
}

func (d *DB) GetSchedule(ctx context.Context, id, userID string) (*AgentSchedule, error) {
	var schedule AgentSchedule
	err := d.db.QueryRowContext(ctx, `
		SELECT id, user_id, agent_id, name, task, cron_expression, timezone,
			is_active, next_run_at, last_run_at, last_status, run_count, failure_count,
			created_at, updated_at
		FROM agent_schedules
		WHERE id = $1 AND user_id = $2
	`, id, userID).Scan(
		&schedule.ID,
		&schedule.UserID,
		&schedule.AgentID,
		&schedule.Name,
		&schedule.Task,
		&schedule.CronExpression,
		&schedule.Timezone,
		&schedule.IsActive,
		&schedule.NextRunAt,
		&schedule.LastRunAt,
		&schedule.LastStatus,
		&schedule.RunCount,
		&schedule.FailureCount,
		&schedule.CreatedAt,
		&schedule.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get schedule: %w", err)
	}
	return &schedule, nil
}

func (d *DB) CreateSchedule(ctx context.Context, s *AgentSchedule) (*AgentSchedule, error) {
	id := GenerateID()
	now := time.Now()

	_, err := d.db.ExecContext(ctx, `
		INSERT INTO agent_schedules (
			id, user_id, agent_id, name, task, cron_expression, timezone,
			is_active, next_run_at, run_count, failure_count, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $12)
	`,
		id,
		s.UserID,
		s.AgentID,
		s.Name,
		s.Task,
		s.CronExpression,
		s.Timezone,
		s.IsActive,
		s.NextRunAt,
		0,
		0,
		now,
	)
	if err != nil {
		return nil, fmt.Errorf("create schedule: %w", err)
	}

	return &AgentSchedule{
		ID:             id,
		UserID:         s.UserID,
		AgentID:        s.AgentID,
		Name:           s.Name,
		Task:           s.Task,
		CronExpression: s.CronExpression,
		Timezone:       s.Timezone,
		IsActive:       s.IsActive,
		NextRunAt:      s.NextRunAt,
		LastRunAt:      nil,
		LastStatus:     nil,
		RunCount:       0,
		FailureCount:   0,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, nil
}

func (d *DB) UpdateSchedule(ctx context.Context, id, userID string, s *AgentSchedule) error {
	_, err := d.db.ExecContext(ctx, `
		UPDATE agent_schedules
		SET name = $1, task = $2, cron_expression = $3, timezone = $4, next_run_at = $5, updated_at = $6
		WHERE id = $7 AND user_id = $8
	`,
		s.Name,
		s.Task,
		s.CronExpression,
		s.Timezone,
		s.NextRunAt,
		time.Now(),
		id,
		userID,
	)
	if err != nil {
		return fmt.Errorf("update schedule: %w", err)
	}
	return nil
}

func (d *DB) ToggleSchedule(ctx context.Context, id, userID string, isActive bool, nextRunAt *time.Time) error {
	if nextRunAt != nil {
		_, err := d.db.ExecContext(ctx, `
			UPDATE agent_schedules
			SET is_active = $1, next_run_at = $2, updated_at = $3
			WHERE id = $4 AND user_id = $5
		`, isActive, *nextRunAt, time.Now(), id, userID)
		if err != nil {
			return fmt.Errorf("toggle schedule: %w", err)
		}
	} else {
		_, err := d.db.ExecContext(ctx, `
			UPDATE agent_schedules
			SET is_active = $1, updated_at = $2
			WHERE id = $3 AND user_id = $4
		`, isActive, time.Now(), id, userID)
		if err != nil {
			return fmt.Errorf("toggle schedule: %w", err)
		}
	}
	return nil
}

func (d *DB) DeleteSchedule(ctx context.Context, id, userID string) error {
	_, err := d.db.ExecContext(ctx, `
		DELETE FROM agent_schedules
		WHERE id = $1 AND user_id = $2
	`, id, userID)
	if err != nil {
		return fmt.Errorf("delete schedule: %w", err)
	}
	return nil
}

func (d *DB) GetDueSchedules(ctx context.Context, now time.Time) ([]AgentSchedule, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT id, user_id, agent_id, name, task, cron_expression, timezone,
			is_active, next_run_at, last_run_at, last_status, run_count, failure_count,
			created_at, updated_at
		FROM agent_schedules
		WHERE is_active = true AND next_run_at <= $1
		ORDER BY next_run_at
		LIMIT $2
	`, now, MaxDueSchedulesPerBatch)
	if err != nil {
		return nil, fmt.Errorf("get due schedules: %w", err)
	}
	defer rows.Close()

	schedules := make([]AgentSchedule, 0, 100)
	for rows.Next() {
		var schedule AgentSchedule
		err := rows.Scan(
			&schedule.ID,
			&schedule.UserID,
			&schedule.AgentID,
			&schedule.Name,
			&schedule.Task,
			&schedule.CronExpression,
			&schedule.Timezone,
			&schedule.IsActive,
			&schedule.NextRunAt,
			&schedule.LastRunAt,
			&schedule.LastStatus,
			&schedule.RunCount,
			&schedule.FailureCount,
			&schedule.CreatedAt,
			&schedule.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan schedule: %w", err)
		}
		schedules = append(schedules, schedule)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate schedules: %w", err)
	}

	return schedules, nil
}

func (d *DB) UpdateScheduleAfterRun(ctx context.Context, id string, lastStatus string, nextRunAt time.Time, failed bool) error {
	now := time.Now()

	if failed {
		_, err := d.db.ExecContext(ctx, `
			UPDATE agent_schedules
			SET last_run_at = $1, last_status = $2, next_run_at = $3, run_count = run_count + 1,
				failure_count = failure_count + 1, updated_at = $4
			WHERE id = $5
		`, now, lastStatus, nextRunAt, now, id)
		if err != nil {
			return fmt.Errorf("update schedule after run: %w", err)
		}
	} else {
		_, err := d.db.ExecContext(ctx, `
			UPDATE agent_schedules
			SET last_run_at = $1, last_status = $2, next_run_at = $3, run_count = run_count + 1,
				failure_count = 0, updated_at = $4
			WHERE id = $5
		`, now, lastStatus, nextRunAt, now, id)
		if err != nil {
			return fmt.Errorf("update schedule after run: %w", err)
		}
	}

	return nil
}
