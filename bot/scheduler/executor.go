package scheduler

import (
	"context"
	"encoding/json"
	"fmt"

	goshared "github.com/KranzL/shipmates-oss/go-shared"
)

func validateAndFetchScheduleData(ctx context.Context, db *goshared.DB, schedule goshared.AgentSchedule) (*goshared.Agent, *goshared.ApiKey, *goshared.GithubConnection, string, interface{}, error) {
	agent, err := db.GetAgent(ctx, schedule.AgentID, schedule.UserID)
	if err != nil {
		return nil, nil, nil, "", nil, fmt.Errorf("get agent: %w", err)
	}
	if agent == nil {
		return nil, nil, nil, "", nil, fmt.Errorf("agent not found")
	}

	if agent.APIKeyID == nil {
		return agent, nil, nil, "", nil, fmt.Errorf("agent has no API key configured")
	}

	apiKey, err := db.GetAPIKey(ctx, *agent.APIKeyID, schedule.UserID)
	if err != nil {
		return agent, nil, nil, "", nil, fmt.Errorf("get API key: %w", err)
	}
	if apiKey == nil {
		return agent, nil, nil, "", nil, fmt.Errorf("API key not found")
	}

	githubConn, err := db.GetGithubConnection(ctx, schedule.UserID)
	if err != nil {
		return agent, apiKey, nil, "", nil, fmt.Errorf("get GitHub connection: %w", err)
	}
	if githubConn == nil {
		return agent, apiKey, nil, "", nil, fmt.Errorf("GitHub connection not found")
	}

	if len(githubConn.Repos) == 0 {
		return agent, apiKey, githubConn, "", nil, fmt.Errorf("no repositories configured")
	}

	repo := githubConn.Repos[0]

	var scopeConfig interface{}
	if len(agent.ScopeConfig) > 0 {
		var scope map[string]interface{}
		if err := json.Unmarshal(agent.ScopeConfig, &scope); err == nil {
			scopeConfig = scope
		}
	}

	return agent, apiKey, githubConn, repo, scopeConfig, nil
}
