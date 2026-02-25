package goshared

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/lib/pq"
)

func (d *DB) ListAgents(ctx context.Context, userID string) ([]Agent, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT id, user_id, name, role, personality, voice_id, scope_config,
			api_key_id, can_delegate_to, max_cost_per_task,
			max_delegation_depth, autonomy_level, created_at, updated_at
		FROM agents
		WHERE user_id = $1
		ORDER BY created_at
		LIMIT 100
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}
	defer rows.Close()

	var agents []Agent
	for rows.Next() {
		var agent Agent
		err := rows.Scan(
			&agent.ID,
			&agent.UserID,
			&agent.Name,
			&agent.Role,
			&agent.Personality,
			&agent.VoiceID,
			&agent.ScopeConfig,
			&agent.APIKeyID,
			pq.Array(&agent.CanDelegateTo),
			&agent.MaxCostPerTask,
			&agent.MaxDelegationDepth,
			&agent.AutonomyLevel,
			&agent.CreatedAt,
			&agent.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan agent: %w", err)
		}
		agents = append(agents, agent)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate agents: %w", err)
	}

	return agents, nil
}

func (d *DB) GetAgent(ctx context.Context, id, userID string) (*Agent, error) {
	var agent Agent
	err := d.db.QueryRowContext(ctx, `
		SELECT id, user_id, name, role, personality, voice_id, scope_config,
			api_key_id, can_delegate_to, max_cost_per_task,
			max_delegation_depth, autonomy_level, created_at, updated_at
		FROM agents
		WHERE id = $1 AND user_id = $2
	`, id, userID).Scan(
		&agent.ID,
		&agent.UserID,
		&agent.Name,
		&agent.Role,
		&agent.Personality,
		&agent.VoiceID,
		&agent.ScopeConfig,
		&agent.APIKeyID,
		pq.Array(&agent.CanDelegateTo),
		&agent.MaxCostPerTask,
		&agent.MaxDelegationDepth,
		&agent.AutonomyLevel,
		&agent.CreatedAt,
		&agent.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get agent: %w", err)
	}
	return &agent, nil
}

func (d *DB) GetAgentByID(ctx context.Context, id string) (*Agent, error) {
	var agent Agent
	err := d.db.QueryRowContext(ctx, `
		SELECT id, user_id, name, role, personality, voice_id, scope_config,
			api_key_id, can_delegate_to, max_cost_per_task,
			max_delegation_depth, autonomy_level, created_at, updated_at
		FROM agents
		WHERE id = $1
	`, id).Scan(
		&agent.ID,
		&agent.UserID,
		&agent.Name,
		&agent.Role,
		&agent.Personality,
		&agent.VoiceID,
		&agent.ScopeConfig,
		&agent.APIKeyID,
		pq.Array(&agent.CanDelegateTo),
		&agent.MaxCostPerTask,
		&agent.MaxDelegationDepth,
		&agent.AutonomyLevel,
		&agent.CreatedAt,
		&agent.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get agent by id: %w", err)
	}
	return &agent, nil
}

func (d *DB) GetAgentByName(ctx context.Context, userID, name string) (*Agent, error) {
	var agent Agent
	err := d.db.QueryRowContext(ctx, `
		SELECT id, user_id, name, role, personality, voice_id, scope_config,
			api_key_id, can_delegate_to, max_cost_per_task,
			max_delegation_depth, autonomy_level, created_at, updated_at
		FROM agents
		WHERE user_id = $1 AND LOWER(name) = LOWER($2)
	`, userID, name).Scan(
		&agent.ID,
		&agent.UserID,
		&agent.Name,
		&agent.Role,
		&agent.Personality,
		&agent.VoiceID,
		&agent.ScopeConfig,
		&agent.APIKeyID,
		pq.Array(&agent.CanDelegateTo),
		&agent.MaxCostPerTask,
		&agent.MaxDelegationDepth,
		&agent.AutonomyLevel,
		&agent.CreatedAt,
		&agent.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get agent by name: %w", err)
	}
	return &agent, nil
}

func (d *DB) CreateAgent(ctx context.Context, userID string, agent *Agent) (*Agent, error) {
	id := GenerateID()
	now := time.Now()

	var apiKeyID sql.NullString
	if agent.APIKeyID != nil {
		apiKeyID = sql.NullString{String: *agent.APIKeyID, Valid: true}
	}

	_, err := d.db.ExecContext(ctx, `
		INSERT INTO agents (
			id, user_id, name, role, personality, voice_id, scope_config,
			api_key_id, can_delegate_to, max_cost_per_task,
			max_delegation_depth, autonomy_level, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $13)
	`,
		id,
		userID,
		agent.Name,
		agent.Role,
		agent.Personality,
		agent.VoiceID,
		agent.ScopeConfig,
		apiKeyID,
		pq.Array(agent.CanDelegateTo),
		agent.MaxCostPerTask,
		agent.MaxDelegationDepth,
		agent.AutonomyLevel,
		now,
	)
	if err != nil {
		return nil, fmt.Errorf("create agent: %w", err)
	}

	return &Agent{
		ID:                 id,
		UserID:             userID,
		Name:               agent.Name,
		Role:               agent.Role,
		Personality:        agent.Personality,
		VoiceID:            agent.VoiceID,
		ScopeConfig:        agent.ScopeConfig,
		APIKeyID:           agent.APIKeyID,
		CanDelegateTo:      agent.CanDelegateTo,
		MaxCostPerTask:     agent.MaxCostPerTask,
		MaxDelegationDepth: agent.MaxDelegationDepth,
		AutonomyLevel:      agent.AutonomyLevel,
		CreatedAt:          now,
		UpdatedAt:          now,
	}, nil
}

func (d *DB) UpdateAgent(ctx context.Context, id, userID string, agent *Agent) (*Agent, error) {
	now := time.Now()

	var apiKeyID sql.NullString
	if agent.APIKeyID != nil {
		apiKeyID = sql.NullString{String: *agent.APIKeyID, Valid: true}
	}

	_, err := d.db.ExecContext(ctx, `
		UPDATE agents
		SET name = $1, role = $2, personality = $3, voice_id = $4, scope_config = $5,
			api_key_id = $6, can_delegate_to = $7, max_cost_per_task = $8,
			max_delegation_depth = $9, autonomy_level = $10, updated_at = $11
		WHERE id = $12 AND user_id = $13
	`,
		agent.Name,
		agent.Role,
		agent.Personality,
		agent.VoiceID,
		agent.ScopeConfig,
		apiKeyID,
		pq.Array(agent.CanDelegateTo),
		agent.MaxCostPerTask,
		agent.MaxDelegationDepth,
		agent.AutonomyLevel,
		now,
		id,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("update agent: %w", err)
	}

	updated := *agent
	updated.ID = id
	updated.UserID = userID
	updated.UpdatedAt = now

	return &updated, nil
}

func (d *DB) DeleteAgent(ctx context.Context, id, userID string) error {
	_, err := d.db.ExecContext(ctx, `
		DELETE FROM agents
		WHERE id = $1 AND user_id = $2
	`, id, userID)
	if err != nil {
		return fmt.Errorf("delete agent: %w", err)
	}
	return nil
}

func (d *DB) CountAgents(ctx context.Context, userID string) (int, error) {
	var count int
	err := d.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM agents
		WHERE user_id = $1
	`, userID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count agents: %w", err)
	}
	return count, nil
}

func (d *DB) GetAgentsByIDs(ctx context.Context, ids []string) ([]Agent, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	rows, err := d.db.QueryContext(ctx, `
		SELECT id, user_id, name, role, personality, voice_id, scope_config,
			api_key_id, can_delegate_to, max_cost_per_task,
			max_delegation_depth, autonomy_level, created_at, updated_at
		FROM agents
		WHERE id = ANY($1)
		ORDER BY created_at
	`, pq.Array(ids))
	if err != nil {
		return nil, fmt.Errorf("get agents by ids: %w", err)
	}
	defer rows.Close()

	var agents []Agent
	for rows.Next() {
		var agent Agent
		err := rows.Scan(
			&agent.ID,
			&agent.UserID,
			&agent.Name,
			&agent.Role,
			&agent.Personality,
			&agent.VoiceID,
			&agent.ScopeConfig,
			&agent.APIKeyID,
			pq.Array(&agent.CanDelegateTo),
			&agent.MaxCostPerTask,
			&agent.MaxDelegationDepth,
			&agent.AutonomyLevel,
			&agent.CreatedAt,
			&agent.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan agent: %w", err)
		}
		agents = append(agents, agent)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate agents by ids: %w", err)
	}

	return agents, nil
}

func (d *DB) GetAgentsByRole(ctx context.Context, userID string, role string) ([]Agent, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT id, user_id, name, role, personality, voice_id, scope_config,
			api_key_id, can_delegate_to, max_cost_per_task,
			max_delegation_depth, autonomy_level, created_at, updated_at
		FROM agents
		WHERE user_id = $1 AND role = $2
		ORDER BY created_at
		LIMIT 100
	`, userID, role)
	if err != nil {
		return nil, fmt.Errorf("get agents by role: %w", err)
	}
	defer rows.Close()

	var agents []Agent
	for rows.Next() {
		var agent Agent
		err := rows.Scan(
			&agent.ID,
			&agent.UserID,
			&agent.Name,
			&agent.Role,
			&agent.Personality,
			&agent.VoiceID,
			&agent.ScopeConfig,
			&agent.APIKeyID,
			pq.Array(&agent.CanDelegateTo),
			&agent.MaxCostPerTask,
			&agent.MaxDelegationDepth,
			&agent.AutonomyLevel,
			&agent.CreatedAt,
			&agent.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan agent: %w", err)
		}
		agents = append(agents, agent)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate agents by role: %w", err)
	}

	return agents, nil
}
