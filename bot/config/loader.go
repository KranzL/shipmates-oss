package config

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"

	goshared "github.com/KranzL/shipmates-oss/go-shared"
	"gopkg.in/yaml.v3"
)

const selfhostedEmail = "selfhosted@shipmates.local"

type AgentYAML struct {
	Name        string `yaml:"name"`
	Role        string `yaml:"role"`
	Personality string `yaml:"personality"`
	Voice       string `yaml:"voice"`
	Scope       struct {
		Directories []string `yaml:"directories"`
		Languages   []string `yaml:"languages"`
	} `yaml:"scope"`
}

type ShipmatesConfig struct {
	DiscordGuildID   string `yaml:"discord_guild_id"`
	SlackWorkspaceID string `yaml:"slack_workspace_id"`
	LLM              struct {
		Provider string `yaml:"provider"`
		BaseURL  string `yaml:"base_url"`
		ModelID  string `yaml:"model_id"`
	} `yaml:"llm"`
	GitHub struct {
		Repos []string `yaml:"repos"`
	} `yaml:"github"`
	Agents []AgentYAML `yaml:"agents"`
}

func findConfigFile() string {
	candidates := []string{
		filepath.Join(".", "shipmates.config.yml"),
		filepath.Join(".", "shipmates.config.yaml"),
		filepath.Join("..", "shipmates.config.yml"),
		filepath.Join("..", "shipmates.config.yaml"),
	}
	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

func Load(ctx context.Context, db *goshared.DB) error {
	configPath := findConfigFile()
	if configPath == "" {
		log.Println("no shipmates.config.yml found, skipping config seeding")
		return nil
	}

	log.Printf("loading config from %s", configPath)

	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("read config file: %w", err)
	}

	var cfg ShipmatesConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parse config file: %w", err)
	}

	if cfg.DiscordGuildID == "" && cfg.SlackWorkspaceID == "" {
		return fmt.Errorf("discord_guild_id or slack_workspace_id is required in shipmates.config.yml")
	}
	if cfg.LLM.Provider == "" {
		return fmt.Errorf("llm.provider is required in shipmates.config.yml")
	}
	if len(cfg.Agents) == 0 {
		return fmt.Errorf("at least one agent is required in shipmates.config.yml")
	}

	hash, err := goshared.HashPassword("selfhosted-placeholder")
	if err != nil {
		return fmt.Errorf("hash placeholder password: %w", err)
	}

	user, err := db.CreateUser(ctx, selfhostedEmail, hash)
	if err != nil {
		return fmt.Errorf("create selfhosted user: %w", err)
	}

	if cfg.DiscordGuildID != "" {
		if err := upsertWorkspace(ctx, db, user.ID, goshared.PlatformDiscord, cfg.DiscordGuildID); err != nil {
			return err
		}
	}

	if cfg.SlackWorkspaceID != "" {
		if err := upsertWorkspace(ctx, db, user.ID, goshared.PlatformSlack, cfg.SlackWorkspaceID); err != nil {
			return err
		}
	}

	if llmKey := os.Getenv("LLM_API_KEY"); llmKey != "" {
		encrypted, err := goshared.EncryptWithKey(llmKey)
		if err != nil {
			return fmt.Errorf("encrypt llm api key: %w", err)
		}

		apiKey := &goshared.ApiKey{
			Provider: goshared.LLMProvider(cfg.LLM.Provider),
			APIKey:   encrypted,
			Label:    "default",
		}
		if cfg.LLM.BaseURL != "" {
			apiKey.BaseURL = &cfg.LLM.BaseURL
		}
		if cfg.LLM.ModelID != "" {
			apiKey.ModelID = &cfg.LLM.ModelID
		}

		if _, err := db.CreateAPIKey(ctx, user.ID, apiKey); err != nil {
			return fmt.Errorf("upsert api key: %w", err)
		}
	}

	if ghToken := os.Getenv("GITHUB_TOKEN"); ghToken != "" && len(cfg.GitHub.Repos) > 0 {
		encrypted, err := goshared.EncryptWithKey(ghToken)
		if err != nil {
			return fmt.Errorf("encrypt github token: %w", err)
		}

		existing, err := db.GetGithubConnection(ctx, user.ID)
		if err != nil {
			return fmt.Errorf("check github connection: %w", err)
		}

		if existing != nil {
			if err := db.UpdateGithubConnection(ctx, user.ID, encrypted, cfg.GitHub.Repos); err != nil {
				return fmt.Errorf("update github connection: %w", err)
			}
		} else {
			if _, err := db.CreateGithubConnection(ctx, user.ID, "selfhosted", encrypted, cfg.GitHub.Repos); err != nil {
				return fmt.Errorf("create github connection: %w", err)
			}
		}
	}

	if err := syncAgents(ctx, db, user.ID, cfg); err != nil {
		return err
	}

	platforms := ""
	if cfg.DiscordGuildID != "" {
		platforms += "discord:" + cfg.DiscordGuildID
	}
	if cfg.SlackWorkspaceID != "" {
		if platforms != "" {
			platforms += ", "
		}
		platforms += "slack:" + cfg.SlackWorkspaceID
	}

	log.Printf("config loaded: %d agents, %s", len(cfg.Agents), platforms)
	return nil
}

func upsertWorkspace(ctx context.Context, db *goshared.DB, userID string, platform goshared.Platform, platformID string) error {
	existing, err := db.GetWorkspaceByPlatform(ctx, platform, platformID)
	if err != nil {
		return fmt.Errorf("check workspace %s:%s: %w", platform, platformID, err)
	}
	if existing != nil {
		return nil
	}

	_, err = db.CreateWorkspace(ctx, &goshared.Workspace{
		UserID:     userID,
		Platform:   platform,
		PlatformID: platformID,
	})
	if err != nil {
		return fmt.Errorf("create workspace %s:%s: %w", platform, platformID, err)
	}
	return nil
}

func syncAgents(ctx context.Context, db *goshared.DB, userID string, cfg ShipmatesConfig) error {
	existingAgents, err := db.ListAgents(ctx, userID)
	if err != nil {
		return fmt.Errorf("list existing agents: %w", err)
	}

	existingByName := make(map[string]goshared.Agent, len(existingAgents))
	for _, agent := range existingAgents {
		existingByName[agent.Name] = agent
	}

	for _, agentCfg := range cfg.Agents {
		scopeConfig := map[string]interface{}{
			"directories": agentCfg.Scope.Directories,
			"languages":   agentCfg.Scope.Languages,
			"repos":       cfg.GitHub.Repos,
		}
		scopeJSON, err := json.Marshal(scopeConfig)
		if err != nil {
			return fmt.Errorf("marshal scope for agent %s: %w", agentCfg.Name, err)
		}

		voiceID := agentCfg.Voice
		if voiceID == "" {
			voiceID = "default"
		}

		if existing, found := existingByName[agentCfg.Name]; found {
			_, err := db.UpdateAgent(ctx, existing.ID, userID, &goshared.Agent{
				Role:        agentCfg.Role,
				Personality: agentCfg.Personality,
				VoiceID:     voiceID,
				ScopeConfig: scopeJSON,
			})
			if err != nil {
				return fmt.Errorf("update agent %s: %w", agentCfg.Name, err)
			}
		} else {
			_, err := db.CreateAgent(ctx, userID, &goshared.Agent{
				Name:        agentCfg.Name,
				Role:        agentCfg.Role,
				Personality: agentCfg.Personality,
				VoiceID:     voiceID,
				ScopeConfig: scopeJSON,
			})
			if err != nil {
				return fmt.Errorf("create agent %s: %w", agentCfg.Name, err)
			}
		}
	}

	configAgentNames := make(map[string]bool, len(cfg.Agents))
	for _, a := range cfg.Agents {
		configAgentNames[a.Name] = true
	}

	for _, existing := range existingAgents {
		if !configAgentNames[existing.Name] {
			if err := db.DeleteAgent(ctx, existing.ID, userID); err != nil {
				return fmt.Errorf("delete agent %s: %w", existing.Name, err)
			}
		}
	}

	return nil
}
