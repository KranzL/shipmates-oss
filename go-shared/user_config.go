package goshared

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

func (d *DB) GetUserConfig(ctx context.Context, userID string) (*UserConfig, error) {
	user, err := d.GetUserByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	if user == nil {
		return nil, fmt.Errorf("user not found")
	}

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	var (
		agents     []Agent
		apiKeys    []ApiKey
		ghConn     *GithubConnection
		agentsErr  error
		keysErr    error
		ghErr      error
		wg         sync.WaitGroup
	)

	wg.Add(3)
	go func() { defer wg.Done(); agents, agentsErr = d.ListAgents(ctx, userID) }()
	go func() { defer wg.Done(); apiKeys, keysErr = d.ListAPIKeys(ctx, userID) }()
	go func() { defer wg.Done(); ghConn, ghErr = d.GetGithubConnection(ctx, userID) }()
	wg.Wait()

	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context cancelled: %w", err)
	}

	if agentsErr != nil {
		return nil, fmt.Errorf("list agents: %w", agentsErr)
	}
	if keysErr != nil {
		return nil, fmt.Errorf("list api keys: %w", keysErr)
	}
	if ghErr != nil {
		return nil, fmt.Errorf("get github connection: %w", ghErr)
	}

	agentConfigs := make([]AgentConfig, len(agents))
	for i, agent := range agents {
		var scope AgentScope
		if err := json.Unmarshal(agent.ScopeConfig, &scope); err != nil {
			return nil, fmt.Errorf("unmarshal scope config for agent %s: %w", agent.ID, err)
		}

		agentConfigs[i] = AgentConfig{
			ID:          agent.ID,
			Name:        agent.Name,
			Role:        agent.Role,
			Personality: agent.Personality,
			VoiceID:     agent.VoiceID,
			Scope:       scope,
			APIKeyID:    agent.APIKeyID,
		}
	}

	userAPIKeys := make([]UserApiKey, 0, len(apiKeys))
	for _, key := range apiKeys {
		decrypted, err := DecryptWithKey(key.APIKey)
		if err != nil {
			return nil, fmt.Errorf("decrypt api key %s (provider=%s): %w", key.ID, key.Provider, err)
		}

		userAPIKeys = append(userAPIKeys, UserApiKey{
			Provider: key.Provider,
			APIKey:   decrypted,
			Label:    &key.Label,
			BaseURL:  key.BaseURL,
			ModelID:  key.ModelID,
		})
	}

	var githubToken *string
	var repos []string
	if ghConn != nil {
		decryptedToken, err := DecryptWithKey(ghConn.GithubToken)
		if err != nil {
			return nil, fmt.Errorf("decrypt github token: %w", err)
		}
		githubToken = &decryptedToken
		repos = ghConn.Repos
	}

	return &UserConfig{
		ID:          user.ID,
		APIKeys:     userAPIKeys,
		Agents:      agentConfigs,
		GithubToken: githubToken,
		Repos:       repos,
	}, nil
}
