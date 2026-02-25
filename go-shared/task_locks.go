package goshared

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

func (d *DB) AcquireTaskLock(ctx context.Context, userID, sessionID, agentID, filePath string, duration time.Duration) (bool, error) {
	id := GenerateID()
	now := time.Now()
	expiresAt := now.Add(duration)

	result, err := d.db.ExecContext(ctx, `
		WITH cleanup AS (
			DELETE FROM task_locks
			WHERE user_id = $1 AND file_path = $2 AND expires_at < $3
		)
		INSERT INTO task_locks (id, user_id, session_id, agent_id, file_path, locked_at, expires_at)
		VALUES ($4, $1, $5, $6, $2, $7, $8)
		ON CONFLICT (user_id, file_path) DO NOTHING
	`, userID, filePath, now, id, sessionID, agentID, now, expiresAt)
	if err != nil {
		return false, fmt.Errorf("acquire task lock: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("check rows affected: %w", err)
	}

	return rows == 1, nil
}

func (d *DB) ReleaseTaskLock(ctx context.Context, userID, filePath string) error {
	_, err := d.db.ExecContext(ctx, `
		DELETE FROM task_locks
		WHERE user_id = $1 AND file_path = $2
	`, userID, filePath)
	if err != nil {
		return fmt.Errorf("release task lock: %w", err)
	}
	return nil
}

func (d *DB) ReleaseSessionLocks(ctx context.Context, sessionID, userID string) error {
	_, err := d.db.ExecContext(ctx, `
		DELETE FROM task_locks
		WHERE session_id = $1
		AND user_id IN (SELECT user_id FROM sessions WHERE id = $2 AND user_id = $3)
	`, sessionID, sessionID, userID)
	if err != nil {
		return fmt.Errorf("release session locks: %w", err)
	}
	return nil
}

func (d *DB) GetActiveLocks(ctx context.Context, userID string) ([]TaskLock, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT id, user_id, session_id, agent_id, file_path, locked_at, expires_at
		FROM task_locks
		WHERE user_id = $1 AND expires_at > $2
		ORDER BY locked_at DESC
	`, userID, time.Now())
	if err != nil {
		return nil, fmt.Errorf("get active locks: %w", err)
	}
	defer rows.Close()

	var locks []TaskLock
	for rows.Next() {
		var lock TaskLock
		err := rows.Scan(
			&lock.ID,
			&lock.UserID,
			&lock.SessionID,
			&lock.AgentID,
			&lock.FilePath,
			&lock.LockedAt,
			&lock.ExpiresAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan task lock: %w", err)
		}
		locks = append(locks, lock)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active locks: %w", err)
	}

	return locks, nil
}

func (d *DB) GetLockHolder(ctx context.Context, userID, filePath string) (*TaskLock, error) {
	var lock TaskLock
	err := d.db.QueryRowContext(ctx, `
		SELECT id, user_id, session_id, agent_id, file_path, locked_at, expires_at
		FROM task_locks
		WHERE user_id = $1 AND file_path = $2 AND expires_at > $3
	`, userID, filePath, time.Now()).Scan(
		&lock.ID,
		&lock.UserID,
		&lock.SessionID,
		&lock.AgentID,
		&lock.FilePath,
		&lock.LockedAt,
		&lock.ExpiresAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get lock holder: %w", err)
	}

	return &lock, nil
}

func (d *DB) CleanExpiredLocks(ctx context.Context) (int, error) {
	result, err := d.db.ExecContext(ctx, `
		DELETE FROM task_locks
		WHERE expires_at < $1
	`, time.Now())
	if err != nil {
		return 0, fmt.Errorf("clean expired locks: %w", err)
	}

	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("get affected rows: %w", err)
	}

	return int(count), nil
}
