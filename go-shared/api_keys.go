package goshared

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

func (d *DB) ListAPIKeys(ctx context.Context, userID string) ([]ApiKey, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT id, user_id, provider, api_key, label, base_url, model_id, created_at
		FROM api_keys
		WHERE user_id = $1
		ORDER BY created_at
		LIMIT 50
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("list api keys: %w", err)
	}
	defer rows.Close()

	var keys []ApiKey
	for rows.Next() {
		var key ApiKey
		var baseURL sql.NullString
		var modelID sql.NullString

		err := rows.Scan(
			&key.ID,
			&key.UserID,
			&key.Provider,
			&key.APIKey,
			&key.Label,
			&baseURL,
			&modelID,
			&key.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan api key: %w", err)
		}

		if baseURL.Valid {
			key.BaseURL = &baseURL.String
		}
		if modelID.Valid {
			key.ModelID = &modelID.String
		}

		keys = append(keys, key)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate api keys: %w", err)
	}

	return keys, nil
}

func (d *DB) GetAPIKey(ctx context.Context, id, userID string) (*ApiKey, error) {
	var key ApiKey
	var baseURL sql.NullString
	var modelID sql.NullString

	err := d.db.QueryRowContext(ctx, `
		SELECT id, user_id, provider, api_key, label, base_url, model_id, created_at
		FROM api_keys
		WHERE id = $1 AND user_id = $2
	`, id, userID).Scan(
		&key.ID,
		&key.UserID,
		&key.Provider,
		&key.APIKey,
		&key.Label,
		&baseURL,
		&modelID,
		&key.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get api key: %w", err)
	}

	if baseURL.Valid {
		key.BaseURL = &baseURL.String
	}
	if modelID.Valid {
		key.ModelID = &modelID.String
	}

	return &key, nil
}

func (d *DB) GetAPIKeyByProvider(ctx context.Context, userID string, provider LLMProvider) (*ApiKey, error) {
	var key ApiKey
	var baseURL sql.NullString
	var modelID sql.NullString

	err := d.db.QueryRowContext(ctx, `
		SELECT id, user_id, provider, api_key, label, base_url, model_id, created_at
		FROM api_keys
		WHERE user_id = $1 AND provider = $2
	`, userID, provider).Scan(
		&key.ID,
		&key.UserID,
		&key.Provider,
		&key.APIKey,
		&key.Label,
		&baseURL,
		&modelID,
		&key.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get api key by provider: %w", err)
	}

	if baseURL.Valid {
		key.BaseURL = &baseURL.String
	}
	if modelID.Valid {
		key.ModelID = &modelID.String
	}

	return &key, nil
}

func (d *DB) GetPrimaryAPIKey(ctx context.Context, userID string) (*ApiKey, error) {
	var key ApiKey
	var baseURL sql.NullString
	var modelID sql.NullString

	err := d.db.QueryRowContext(ctx, `
		SELECT id, user_id, provider, api_key, label, base_url, model_id, created_at
		FROM api_keys
		WHERE user_id = $1
		ORDER BY created_at
		LIMIT 1
	`, userID).Scan(
		&key.ID,
		&key.UserID,
		&key.Provider,
		&key.APIKey,
		&key.Label,
		&baseURL,
		&modelID,
		&key.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get primary api key: %w", err)
	}

	if baseURL.Valid {
		key.BaseURL = &baseURL.String
	}
	if modelID.Valid {
		key.ModelID = &modelID.String
	}

	return &key, nil
}

func (d *DB) CreateAPIKey(ctx context.Context, userID string, key *ApiKey) (*ApiKey, error) {
	id := GenerateID()
	now := time.Now()

	var baseURL sql.NullString
	if key.BaseURL != nil {
		baseURL = sql.NullString{String: *key.BaseURL, Valid: true}
	}

	var modelID sql.NullString
	if key.ModelID != nil {
		modelID = sql.NullString{String: *key.ModelID, Valid: true}
	}

	var returnedID string
	var returnedCreatedAt time.Time

	err := d.db.QueryRowContext(ctx, `
		INSERT INTO api_keys (id, user_id, provider, api_key, label, base_url, model_id, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (user_id, provider) DO UPDATE SET
			api_key = EXCLUDED.api_key,
			label = EXCLUDED.label,
			base_url = EXCLUDED.base_url,
			model_id = EXCLUDED.model_id
		RETURNING id, created_at
	`, id, userID, key.Provider, key.APIKey, key.Label, baseURL, modelID, now).Scan(&returnedID, &returnedCreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create api key: %w", err)
	}

	return &ApiKey{
		ID:        returnedID,
		UserID:    userID,
		Provider:  key.Provider,
		APIKey:    key.APIKey,
		Label:     key.Label,
		BaseURL:   key.BaseURL,
		ModelID:   key.ModelID,
		CreatedAt: returnedCreatedAt,
	}, nil
}

func (d *DB) DeleteAPIKey(ctx context.Context, id, userID string) error {
	_, err := d.db.ExecContext(ctx, `
		DELETE FROM api_keys
		WHERE id = $1 AND user_id = $2
	`, id, userID)
	if err != nil {
		return fmt.Errorf("delete api key: %w", err)
	}
	return nil
}
