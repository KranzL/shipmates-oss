package goshared

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

type UsageSummaryRecord struct {
	Type          UsageType `json:"type"`
	TotalQuantity float64   `json:"totalQuantity"`
}

func (d *DB) RecordUsage(ctx context.Context, userID string, usageType UsageType, quantity float64, metadata map[string]interface{}, sessionID *string) error {
	id := GenerateID()
	now := time.Now()

	var metadataJSON []byte
	var err error
	if metadata != nil {
		metadataJSON, err = json.Marshal(metadata)
		if err != nil {
			return fmt.Errorf("marshal metadata: %w", err)
		}
	}

	var sessionIDVal sql.NullString
	if sessionID != nil {
		sessionIDVal = sql.NullString{String: *sessionID, Valid: true}
	}

	_, err = d.db.ExecContext(ctx, `
		INSERT INTO usage_records (id, user_id, type, quantity, metadata, session_id, recorded_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, id, userID, usageType, quantity, metadataJSON, sessionIDVal, now)
	if err != nil {
		return fmt.Errorf("record usage: %w", err)
	}

	return nil
}

func (d *DB) GetUserUsageSummary(ctx context.Context, userID string, startDate, endDate time.Time) ([]UsageSummaryRecord, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT type, SUM(quantity) as total_quantity
		FROM usage_records
		WHERE user_id = $1 AND recorded_at BETWEEN $2 AND $3
		GROUP BY type
	`, userID, startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("get user usage summary: %w", err)
	}
	defer rows.Close()

	var records []UsageSummaryRecord
	for rows.Next() {
		var record UsageSummaryRecord
		err := rows.Scan(&record.Type, &record.TotalQuantity)
		if err != nil {
			return nil, fmt.Errorf("scan usage summary: %w", err)
		}
		records = append(records, record)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate usage summary: %w", err)
	}

	return records, nil
}

func (d *DB) GetSessionUsage(ctx context.Context, sessionID string) ([]UsageRecord, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT id, user_id, type, quantity, metadata, session_id, recorded_at
		FROM usage_records
		WHERE session_id = $1
		ORDER BY recorded_at
	`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("get session usage: %w", err)
	}
	defer rows.Close()

	var records []UsageRecord
	for rows.Next() {
		var record UsageRecord
		var sessionIDSQL sql.NullString

		err := rows.Scan(&record.ID, &record.UserID, &record.Type, &record.Quantity, &record.Metadata, &sessionIDSQL, &record.RecordedAt)
		if err != nil {
			return nil, fmt.Errorf("scan usage record: %w", err)
		}

		if sessionIDSQL.Valid {
			record.SessionID = &sessionIDSQL.String
		}

		records = append(records, record)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate usage records: %w", err)
	}

	return records, nil
}
