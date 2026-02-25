package issue_pipeline

import (
	"context"
	"fmt"

	goshared "github.com/KranzL/shipmates-oss/go-shared"
)

func validateAndFetchPipelineData(ctx context.Context, db *goshared.DB, pipeline goshared.IssuePipeline) (*goshared.Agent, *goshared.ApiKey, *goshared.GithubConnection, interface{}, error) {
	agent, err := db.GetAgent(ctx, pipeline.AgentID, pipeline.UserID)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("get agent: %w", err)
	}
	if agent == nil {
		return nil, nil, nil, nil, fmt.Errorf("agent not found")
	}

	if agent.APIKeyID == nil {
		return agent, nil, nil, nil, fmt.Errorf("agent has no API key configured")
	}

	apiKey, err := db.GetAPIKey(ctx, *agent.APIKeyID, pipeline.UserID)
	if err != nil {
		return agent, nil, nil, nil, fmt.Errorf("get API key: %w", err)
	}
	if apiKey == nil {
		return agent, nil, nil, nil, fmt.Errorf("API key not found")
	}

	githubConn, err := db.GetGithubConnection(ctx, pipeline.UserID)
	if err != nil {
		return agent, apiKey, nil, nil, fmt.Errorf("get GitHub connection: %w", err)
	}
	if githubConn == nil {
		return agent, apiKey, nil, nil, fmt.Errorf("GitHub connection not found")
	}

	return agent, apiKey, githubConn, agent.ScopeConfig, nil
}
