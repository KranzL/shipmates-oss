package goshared

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

const MaxClarificationsPerSession = 10

func (d *DB) CreateClarification(ctx context.Context, sessionID, question string) (*Clarification, error) {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	var exists string
	err = tx.QueryRowContext(ctx, `
		SELECT id FROM sessions WHERE id = $1 FOR UPDATE
	`, sessionID).Scan(&exists)
	if err != nil {
		return nil, fmt.Errorf("lock session: %w", err)
	}

	var count int
	err = tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM clarifications WHERE session_id = $1
	`, sessionID).Scan(&count)
	if err != nil {
		return nil, fmt.Errorf("count clarifications: %w", err)
	}

	if count >= MaxClarificationsPerSession {
		return nil, fmt.Errorf("max clarifications (%d) reached for session", MaxClarificationsPerSession)
	}

	id := GenerateID()
	now := time.Now()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO clarifications (id, session_id, question, status, asked_at)
		VALUES ($1, $2, $3, $4, $5)
	`, id, sessionID, question, ClarificationStatusPending, now)
	if err != nil {
		return nil, fmt.Errorf("create clarification: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return &Clarification{
		ID:        id,
		SessionID: sessionID,
		Question:  question,
		Status:    ClarificationStatusPending,
		AskedAt:   now,
	}, nil
}

func (d *DB) AnswerClarification(ctx context.Context, id, userID, answer string) error {
	now := time.Now()

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	var sessionID string
	err = tx.QueryRowContext(ctx, `
		UPDATE clarifications AS c
		SET answer = $1, status = $2, answered_at = $3
		FROM sessions AS s
		WHERE c.id = $4 AND c.session_id = s.id AND s.user_id = $5
		RETURNING c.session_id
	`, answer, ClarificationStatusAnswered, now, id, userID).Scan(&sessionID)
	if err != nil {
		return fmt.Errorf("answer clarification: %w", err)
	}

	var pendingCount int
	err = tx.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM clarifications
		WHERE session_id = $1 AND status = $2
	`, sessionID, ClarificationStatusPending).Scan(&pendingCount)
	if err != nil {
		return fmt.Errorf("count pending clarifications: %w", err)
	}

	if pendingCount == 0 {
		_, err = tx.ExecContext(ctx, `
			UPDATE sessions
			SET status = $1, updated_at = $2
			WHERE id = $3 AND status = $4
		`, SessionStatusRunning, now, sessionID, SessionStatusWaitingForClarification)
		if err != nil {
			return fmt.Errorf("update session status: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

func (d *DB) GetPendingClarifications(ctx context.Context, sessionID string) ([]Clarification, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT id, session_id, question, answer, status, asked_at, answered_at
		FROM clarifications
		WHERE session_id = $1 AND status = $2
		ORDER BY asked_at ASC
	`, sessionID, ClarificationStatusPending)
	if err != nil {
		return nil, fmt.Errorf("get pending clarifications: %w", err)
	}
	defer rows.Close()

	clarifications := []Clarification{}
	for rows.Next() {
		var c Clarification
		var answerSQL sql.NullString
		var answeredAtSQL sql.NullTime

		err := rows.Scan(&c.ID, &c.SessionID, &c.Question, &answerSQL, &c.Status, &c.AskedAt, &answeredAtSQL)
		if err != nil {
			return nil, fmt.Errorf("scan pending clarification: %w", err)
		}

		if answerSQL.Valid {
			c.Answer = &answerSQL.String
		}
		if answeredAtSQL.Valid {
			c.AnsweredAt = &answeredAtSQL.Time
		}

		clarifications = append(clarifications, c)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("get pending clarifications rows: %w", err)
	}

	return clarifications, nil
}

func (d *DB) ListClarifications(ctx context.Context, sessionID string) ([]Clarification, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT id, session_id, question, answer, status, asked_at, answered_at
		FROM clarifications
		WHERE session_id = $1
		ORDER BY asked_at ASC
	`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list clarifications: %w", err)
	}
	defer rows.Close()

	clarifications := []Clarification{}
	for rows.Next() {
		var c Clarification
		var answerSQL sql.NullString
		var answeredAtSQL sql.NullTime

		err := rows.Scan(&c.ID, &c.SessionID, &c.Question, &answerSQL, &c.Status, &c.AskedAt, &answeredAtSQL)
		if err != nil {
			return nil, fmt.Errorf("scan clarification: %w", err)
		}

		if answerSQL.Valid {
			c.Answer = &answerSQL.String
		}
		if answeredAtSQL.Valid {
			c.AnsweredAt = &answeredAtSQL.Time
		}

		clarifications = append(clarifications, c)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list clarifications rows: %w", err)
	}

	return clarifications, nil
}

func (d *DB) ExpireStaleClarifications(ctx context.Context, threshold time.Time) ([]string, error) {
	rows, err := d.db.QueryContext(ctx, `
		UPDATE clarifications
		SET status = $1
		WHERE status = $2 AND asked_at < $3
		RETURNING id
	`, ClarificationStatusExpired, ClarificationStatusPending, threshold)
	if err != nil {
		return nil, fmt.Errorf("expire stale clarifications: %w", err)
	}
	defer rows.Close()

	expired := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan expired id: %w", err)
		}
		expired = append(expired, id)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("expire stale clarifications rows: %w", err)
	}

	return expired, nil
}

func (d *DB) GetClarification(ctx context.Context, id string) (*Clarification, error) {
	var c Clarification
	var answerSQL sql.NullString
	var answeredAtSQL sql.NullTime

	err := d.db.QueryRowContext(ctx, `
		SELECT id, session_id, question, answer, status, asked_at, answered_at
		FROM clarifications
		WHERE id = $1
	`, id).Scan(&c.ID, &c.SessionID, &c.Question, &answerSQL, &c.Status, &c.AskedAt, &answeredAtSQL)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get clarification: %w", err)
	}

	if answerSQL.Valid {
		c.Answer = &answerSQL.String
	}
	if answeredAtSQL.Valid {
		c.AnsweredAt = &answeredAtSQL.Time
	}

	return &c, nil
}

func (d *DB) UpdateClarificationStatus(ctx context.Context, id string, status ClarificationStatus) error {
	_, err := d.db.ExecContext(ctx, `
		UPDATE clarifications
		SET status = $1
		WHERE id = $2
	`, status, id)
	if err != nil {
		return fmt.Errorf("update clarification status: %w", err)
	}
	return nil
}

func (d *DB) CountPendingClarifications(ctx context.Context, sessionID string) (int, error) {
	var count int
	err := d.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM clarifications
		WHERE session_id = $1 AND status = $2
	`, sessionID, ClarificationStatusPending).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count pending clarifications: %w", err)
	}

	return count, nil
}
