package goshared

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

func (d *DB) CreateDelegationEvent(ctx context.Context, e *DelegationEvent) error {
	id := GenerateID()
	now := time.Now()

	var toSessionID sql.NullString
	if e.ToSessionID != nil {
		toSessionID = sql.NullString{String: *e.ToSessionID, Valid: true}
	}

	var reason sql.NullString
	if e.Reason != nil {
		reason = sql.NullString{String: *e.Reason, Valid: true}
	}

	var costUSD sql.NullFloat64
	if e.CostUSD != nil {
		costUSD = sql.NullFloat64{Float64: *e.CostUSD, Valid: true}
	}

	_, err := d.db.ExecContext(ctx, `
		INSERT INTO delegation_events (
			id, user_id, from_session_id, to_session_id, from_agent_id, to_agent_id,
			action, reason, cost_usd, timestamp
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`,
		id,
		e.UserID,
		e.FromSessionID,
		toSessionID,
		e.FromAgentID,
		e.ToAgentID,
		e.Action,
		reason,
		costUSD,
		now,
	)
	if err != nil {
		return fmt.Errorf("create delegation event: %w", err)
	}
	return nil
}

func (d *DB) GetDelegationEvents(ctx context.Context, userID string, limit int) ([]DelegationEvent, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT id, user_id, from_session_id, to_session_id, from_agent_id, to_agent_id,
			action, reason, cost_usd, timestamp
		FROM delegation_events
		WHERE user_id = $1
		ORDER BY timestamp DESC
		LIMIT $2
	`, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("get delegation events: %w", err)
	}
	defer rows.Close()

	var events []DelegationEvent
	for rows.Next() {
		var event DelegationEvent
		var toSessionID sql.NullString
		var reason sql.NullString
		var costUSD sql.NullFloat64

		err := rows.Scan(
			&event.ID,
			&event.UserID,
			&event.FromSessionID,
			&toSessionID,
			&event.FromAgentID,
			&event.ToAgentID,
			&event.Action,
			&reason,
			&costUSD,
			&event.Timestamp,
		)
		if err != nil {
			return nil, fmt.Errorf("scan delegation event: %w", err)
		}

		if toSessionID.Valid {
			event.ToSessionID = &toSessionID.String
		}
		if reason.Valid {
			event.Reason = &reason.String
		}
		if costUSD.Valid {
			event.CostUSD = &costUSD.Float64
		}

		events = append(events, event)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate delegation events: %w", err)
	}

	return events, nil
}
