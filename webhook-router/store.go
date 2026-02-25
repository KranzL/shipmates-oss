package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
)

type githubConnectionRow struct {
	ID            string
	UserID        string
	GithubToken   string
	WebhookSecret sql.NullString
	IssueLabel    sql.NullString
	Repos         []string
}

type agentRow struct {
	ID          string
	Name        string
	Role        string
	Personality string
	ScopeConfig json.RawMessage
}

type apiKeyRow struct {
	ID       string
	Provider string
	APIKey   string
	BaseURL  sql.NullString
	ModelID  sql.NullString
}

type scopeConfig struct {
	Repos []string `json:"repos"`
}

type webhookContext struct {
	UserID        string
	AgentID       string
	AgentName     string
	AgentRole     string
	Personality   string
	GithubToken   string
	APIKeyID      string
	Provider      string
	EncryptedKey  string
	BaseURL       string
	ModelID       string
	WebhookSecret string
	IssueLabel    string
}

func resolveWebhookContext(db *sql.DB, repo string, encryptionKey string) (*webhookContext, string, error) {
	rows, err := db.Query(`
		SELECT
			gc.id, gc.user_id, gc.github_token, gc.webhook_secret, gc.issue_label, gc.repos,
			a.id, a.name, a.role, a.personality, a.scope_config,
			k.id, k.provider, k.api_key, k.base_url, k.model_id
		FROM github_connections gc
		LEFT JOIN LATERAL (
			SELECT id, name, role, personality, scope_config
			FROM agents
			WHERE user_id = gc.user_id
			ORDER BY created_at ASC
		) a ON true
		LEFT JOIN LATERAL (
			SELECT id, provider, api_key, base_url, model_id
			FROM api_keys
			WHERE user_id = gc.user_id
			ORDER BY created_at ASC
			LIMIT 1
		) k ON true
		WHERE $1 = ANY(gc.repos) AND gc.webhook_secret IS NOT NULL
	`, repo)
	if err != nil {
		return nil, "", fmt.Errorf("query webhook context: %w", err)
	}
	defer rows.Close()

	var conn githubConnectionRow
	var agents []agentRow
	var apiKey *apiKeyRow

	for rows.Next() {
		var a agentRow
		var k apiKeyRow
		var agentID, agentName, agentRole, agentPersonality sql.NullString
		var agentScope []byte
		var keyID, keyProvider, keyAPIKey sql.NullString
		var keyBaseURL, keyModelID sql.NullString

		if err := rows.Scan(
			&conn.ID, &conn.UserID, &conn.GithubToken, &conn.WebhookSecret, &conn.IssueLabel, pq.Array(&conn.Repos),
			&agentID, &agentName, &agentRole, &agentPersonality, &agentScope,
			&keyID, &keyProvider, &keyAPIKey, &keyBaseURL, &keyModelID,
		); err != nil {
			return nil, "", fmt.Errorf("scan webhook context: %w", err)
		}

		if agentID.Valid {
			a.ID = agentID.String
			a.Name = agentName.String
			a.Role = agentRole.String
			a.Personality = agentPersonality.String
			a.ScopeConfig = agentScope
			agents = append(agents, a)
		}

		if keyID.Valid && apiKey == nil {
			k.ID = keyID.String
			k.Provider = keyProvider.String
			k.APIKey = keyAPIKey.String
			k.BaseURL = keyBaseURL
			k.ModelID = keyModelID
			apiKey = &k
		}
	}

	if conn.ID == "" {
		return nil, "no matching github connection", nil
	}

	if len(agents) == 0 {
		return nil, "no agent configured", nil
	}

	if apiKey == nil {
		return nil, "no API key configured", nil
	}

	var selectedAgent *agentRow
	for i := range agents {
		if selectedAgent == nil {
			selectedAgent = &agents[i]
		}

		var scope scopeConfig
		if err := json.Unmarshal(agents[i].ScopeConfig, &scope); err != nil {
			continue
		}

		for _, r := range scope.Repos {
			if strings.EqualFold(r, repo) {
				selectedAgent = &agents[i]
				break
			}
		}
	}

	webhookSecret := ""
	if conn.WebhookSecret.Valid {
		webhookSecret = conn.WebhookSecret.String
	}

	issueLabel := "shipmates"
	if conn.IssueLabel.Valid && conn.IssueLabel.String != "" {
		issueLabel = conn.IssueLabel.String
	}

	baseURL := ""
	if apiKey.BaseURL.Valid {
		baseURL = apiKey.BaseURL.String
	}
	modelID := ""
	if apiKey.ModelID.Valid {
		modelID = apiKey.ModelID.String
	}

	return &webhookContext{
		UserID:        conn.UserID,
		AgentID:       selectedAgent.ID,
		AgentName:     selectedAgent.Name,
		AgentRole:     selectedAgent.Role,
		Personality:   selectedAgent.Personality,
		GithubToken:   conn.GithubToken,
		APIKeyID:      apiKey.ID,
		Provider:      apiKey.Provider,
		EncryptedKey:  apiKey.APIKey,
		BaseURL:       baseURL,
		ModelID:       modelID,
		WebhookSecret: webhookSecret,
		IssueLabel:    issueLabel,
	}, "", nil
}

func createIssuePipeline(db *sql.DB, userID string, agentID string, repo string, issueNumber int, issueTitle string, issueBody string, labels []string) error {
	if len(issueBody) > 10000 {
		issueBody = issueBody[:10000]
	}

	labelsJSON, err := json.Marshal(labels)
	if err != nil {
		return fmt.Errorf("marshal labels: %w", err)
	}

	_, err = db.Exec(`
		INSERT INTO issue_pipelines (id, user_id, agent_id, repo, issue_number, issue_title, issue_body, issue_labels, status, created_at, updated_at)
		VALUES (gen_random_uuid()::text, $1, $2, $3, $4, $5, $6, $7, 'pending', $8, $8)
	`, userID, agentID, repo, issueNumber, issueTitle, issueBody, string(labelsJSON), time.Now())
	if err != nil {
		return fmt.Errorf("insert issue pipeline: %w", err)
	}

	return nil
}
