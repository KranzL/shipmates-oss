package diagnosis

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	gh "github.com/KranzL/shipmates-oss/go-github"
	llm "github.com/KranzL/shipmates-oss/go-llm"
)

type Diagnoser struct {
	store        *Store
	memoryClient *MemoryClient
	db           *sql.DB
}

func NewDiagnoser(db *sql.DB, memoryClient *MemoryClient) *Diagnoser {
	return &Diagnoser{
		store:        NewStore(db),
		memoryClient: memoryClient,
		db:           db,
	}
}

func (d *Diagnoser) ProcessDiagnosis(req DiagnosisRequest) (*DiagnosisResult, error) {
	diagID, err := d.store.CreateDiagnosis(req)
	if err != nil {
		return nil, fmt.Errorf("create diagnosis record: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	ghClient := gh.NewClient(req.GithubToken)

	var errorSection string
	if req.RunID > 0 {
		logs, err := ghClient.FetchWorkflowLogs(ctx, req.Repo, req.RunID)
		if err != nil {
			log.Printf("fetch workflow logs: %v", err)
		} else {
			errorSection = ExtractErrorSections(logs, 15000)
		}
	}

	if errorSection == "" {
		errorSection = "(No logs available for this failure)"
	}

	category := ClassifyError(errorSection)

	commits, err := ghClient.ListCommits(ctx, req.Repo, req.CommitSHA, 5)
	if err != nil {
		log.Printf("list commits: %v", err)
	}

	var commitSummary strings.Builder
	for _, c := range commits {
		sha := c.SHA
		if len(sha) > 7 {
			sha = sha[:7]
		}
		msg := c.Commit.Message
		if idx := strings.Index(msg, "\n"); idx >= 0 {
			msg = msg[:idx]
		}
		fmt.Fprintf(&commitSummary, "- %s: %s\n", sha, msg)
	}

	pastFailures, err := d.memoryClient.FetchPastFailures(req.UserID, errorSection[:min(500, len(errorSection))])
	if err != nil {
		log.Printf("fetch past failures: %v", err)
	}

	systemPrompt := BuildSystemPrompt(req.AgentName, req.AgentRole, req.Personality)
	userPrompt := BuildUserPrompt(req.WorkflowName, errorSection, commitSummary.String(), pastFailures)

	llmClient, err := llm.NewClient(
		llm.Provider(req.Provider),
		req.APIKey,
		&llm.ClientOptions{
			BaseURL: req.BaseURL,
			ModelID: req.ModelID,
		},
	)
	if err != nil {
		if updateErr := d.store.UpdateDiagnosisResult(diagID, category, "", "", 0, 0, 0, "error"); updateErr != nil {
			log.Printf("update diagnosis result: %v", updateErr)
		}
		return nil, fmt.Errorf("create LLM client: %w", err)
	}

	chatResult, err := llmClient.Chat(
		ctx,
		[]llm.ChatMessage{{Role: "user", Content: userPrompt}},
		systemPrompt,
		4096,
	)
	if err != nil {
		if updateErr := d.store.UpdateDiagnosisResult(diagID, category, "", "", 0, 0, 0, "error"); updateErr != nil {
			log.Printf("update diagnosis result: %v", updateErr)
		}
		return nil, fmt.Errorf("LLM diagnosis: %w", err)
	}

	result, err := parseDiagnosisResponse(chatResult.Text, category)
	if err != nil {
		if updateErr := d.store.UpdateDiagnosisResult(diagID, category, chatResult.Text, "", 0, chatResult.InputTokens, chatResult.OutputTokens, "error"); updateErr != nil {
			log.Printf("update diagnosis result: %v", updateErr)
		}
		return nil, fmt.Errorf("parse diagnosis response: %w", err)
	}

	result.ID = diagID
	result.InputTokens = chatResult.InputTokens
	result.OutputTokens = chatResult.OutputTokens

	status := "completed"
	if result.AutoFixable {
		status = "awaiting_approval"
	}

	if err := d.store.UpdateDiagnosisResult(diagID, result.ErrorCategory, result.Diagnosis, result.RootCause, result.Confidence, chatResult.InputTokens, chatResult.OutputTokens, status); err != nil {
		log.Printf("update diagnosis result: %v", err)
	}

	if err := PostDiagnosisComment(ctx, ghClient, req.Repo, req.PRNumber, req.AgentName, req.AgentRole, result); err != nil {
		log.Printf("post diagnosis comment: %v", err)
	}

	return result, nil
}

func parseDiagnosisResponse(text string, fallbackCategory ErrorCategory) (*DiagnosisResult, error) {
	jsonStart := strings.Index(text, "{")
	jsonEnd := strings.LastIndex(text, "}")
	if jsonStart < 0 || jsonEnd <= jsonStart {
		return &DiagnosisResult{
			ErrorCategory: fallbackCategory,
			Diagnosis:     text,
		}, nil
	}

	jsonStr := text[jsonStart : jsonEnd+1]

	var raw struct {
		ErrorCategory string    `json:"errorCategory"`
		Diagnosis     string    `json:"diagnosis"`
		RootCause     string    `json:"rootCause"`
		SuggestedFix  string    `json:"suggestedFix"`
		Confidence    float64   `json:"confidence"`
		AutoFixable   bool      `json:"autoFixable"`
		FixFiles      []FixFile `json:"fixFiles"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &raw); err != nil {
		return &DiagnosisResult{
			ErrorCategory: fallbackCategory,
			Diagnosis:     text,
		}, nil
	}

	cat := ErrorCategory(raw.ErrorCategory)
	if cat == "" {
		cat = fallbackCategory
	}

	autoFixable := raw.AutoFixable && IsAutoFixSafe(cat, raw.FixFiles)

	return &DiagnosisResult{
		ErrorCategory: cat,
		Diagnosis:     raw.Diagnosis,
		RootCause:     raw.RootCause,
		SuggestedFix:  raw.SuggestedFix,
		Confidence:    raw.Confidence,
		AutoFixable:   autoFixable,
	}, nil
}
