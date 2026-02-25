package review

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

type Reviewer struct {
	store        *Store
	memoryClient *MemoryClient
	db           *sql.DB
}

func NewReviewer(db *sql.DB, memoryClient *MemoryClient) *Reviewer {
	return &Reviewer{
		store:        NewStore(db),
		memoryClient: memoryClient,
		db:           db,
	}
}

func (rv *Reviewer) ProcessReview(req ReviewRequest) (*ReviewResult, error) {
	reviewID, err := rv.store.CreateReview(req.UserID, req.AgentID, req.Repo, req.PRNumber)
	if err != nil {
		return nil, fmt.Errorf("create review record: %w", err)
	}

	ghClient := gh.NewClient(req.GithubToken)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	diff, err := ghClient.FetchDiff(ctx, req.Repo, req.PRNumber)
	if err != nil {
		if updateErr := rv.store.UpdateReviewResult(reviewID, nil, 0, 0, "error"); updateErr != nil {
			log.Printf("update review result after fetch diff error: %v", updateErr)
		}
		return nil, fmt.Errorf("fetch diff: %w", err)
	}

	files := ParseDiff(diff)
	diffContext := BuildDiffContext(files, 8000)

	conventions, err := rv.memoryClient.FetchConventions(req.UserID, req.Repo)
	if err != nil {
		log.Printf("fetch conventions: %v", err)
	}

	calibration, err := FetchCalibrationData(rv.db, req.UserID, req.Repo)
	if err != nil {
		log.Printf("fetch calibration: %v", err)
	}

	systemPrompt := BuildSystemPrompt(req.AgentName, req.AgentRole, req.Personality, conventions, calibration)
	userPrompt := BuildUserPrompt(req.PRTitle, req.PRBody, diffContext)

	llmClient, err := llm.NewClient(
		llm.Provider(req.Provider),
		req.APIKey,
		&llm.ClientOptions{
			BaseURL: req.BaseURL,
			ModelID: req.ModelID,
		},
	)
	if err != nil {
		if updateErr := rv.store.UpdateReviewResult(reviewID, nil, 0, 0, "error"); updateErr != nil {
			log.Printf("update review result after LLM client error: %v", updateErr)
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
		if updateErr := rv.store.UpdateReviewResult(reviewID, nil, 0, 0, "error"); updateErr != nil {
			log.Printf("update review result after LLM chat error: %v", updateErr)
		}
		return nil, fmt.Errorf("LLM review: %w", err)
	}

	result, err := parseReviewResponse(chatResult.Text)
	if err != nil {
		if updateErr := rv.store.UpdateReviewResult(reviewID, nil, chatResult.InputTokens, chatResult.OutputTokens, "error"); updateErr != nil {
			log.Printf("update review result after parse error: %v", updateErr)
		}
		return nil, fmt.Errorf("parse review response: %w", err)
	}

	result.ID = reviewID
	result.InputTokens = chatResult.InputTokens
	result.OutputTokens = chatResult.OutputTokens

	severityCounts := make(map[string]int)
	for _, comment := range result.Comments {
		severityCounts[string(comment.Severity)]++
	}
	result.SeverityCounts = severityCounts

	for _, comment := range result.Comments {
		if _, err := rv.store.CreateComment(reviewID, comment); err != nil {
			log.Printf("create comment for review %s: %v", reviewID, err)
		}
	}

	if err := PostReview(ctx, ghClient, req.Repo, req.PRNumber, req.AgentName, req.AgentRole, result, files); err != nil {
		log.Printf("post github review: %v", err)
		if updateErr := rv.store.UpdateReviewResult(reviewID, severityCounts, chatResult.InputTokens, chatResult.OutputTokens, "posted_partial"); updateErr != nil {
			log.Printf("update review result after partial post: %v", updateErr)
		}
	} else {
		if updateErr := rv.store.UpdateReviewResult(reviewID, severityCounts, chatResult.InputTokens, chatResult.OutputTokens, "completed"); updateErr != nil {
			log.Printf("update review result after completion: %v", updateErr)
		}
	}

	return result, nil
}

func parseReviewResponse(text string) (*ReviewResult, error) {
	jsonStart := strings.Index(text, "{")
	jsonEnd := strings.LastIndex(text, "}")
	if jsonStart < 0 || jsonEnd <= jsonStart {
		return &ReviewResult{Summary: text}, nil
	}

	jsonStr := text[jsonStart : jsonEnd+1]

	var raw struct {
		Summary  string          `json:"summary"`
		Comments json.RawMessage `json:"comments"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &raw); err != nil {
		return &ReviewResult{Summary: text}, nil
	}

	result := &ReviewResult{Summary: raw.Summary}

	if raw.Comments != nil {
		var comments []ReviewComment
		if err := json.Unmarshal(raw.Comments, &comments); err == nil {
			result.Comments = comments
		}
	}

	return result, nil
}
