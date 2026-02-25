package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	googleDefaultModel = "gemini-2.5-pro"
	googleEndpoint     = "https://generativelanguage.googleapis.com/v1beta/models"
)

var googleHTTPClient = &http.Client{
	Timeout: 120 * time.Second,
}

type googleClient struct {
	apiKey  string
	modelID string
}

type googleRequest struct {
	Contents         []googleContent       `json:"contents"`
	SystemInstruction *googleContent        `json:"systemInstruction,omitempty"`
	GenerationConfig *googleGenerationConfig `json:"generationConfig,omitempty"`
}

type googleContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []googlePart `json:"parts"`
}

type googlePart struct {
	Text string `json:"text"`
}

type googleGenerationConfig struct {
	MaxOutputTokens int `json:"maxOutputTokens,omitempty"`
}

type googleResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
	UsageMetadata struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
	} `json:"usageMetadata"`
}

func newGoogleClient(apiKey string, modelID string) *googleClient {
	if modelID == "" {
		modelID = googleDefaultModel
	}
	return &googleClient{apiKey: apiKey, modelID: modelID}
}

func (c *googleClient) Chat(ctx context.Context, messages []ChatMessage, systemPrompt string, maxTokens int) (*ChatResult, error) {
	var contents []googleContent
	for _, m := range messages {
		role := m.Role
		if role == "assistant" {
			role = "model"
		}
		contents = append(contents, googleContent{
			Role:  role,
			Parts: []googlePart{{Text: m.Content}},
		})
	}

	reqBody := googleRequest{
		Contents: contents,
		GenerationConfig: &googleGenerationConfig{
			MaxOutputTokens: maxTokens,
		},
	}

	if systemPrompt != "" {
		reqBody.SystemInstruction = &googleContent{
			Parts: []googlePart{{Text: systemPrompt}},
		}
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal google request: %w", err)
	}

	endpoint := fmt.Sprintf("%s/%s:generateContent", googleEndpoint, c.modelID)
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create google request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", c.apiKey)

	resp, err := googleHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("google API call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		limitedReader := io.LimitReader(resp.Body, 4096)
		respBody, _ := io.ReadAll(limitedReader)
		return nil, &ProviderError{
			Provider:   ProviderGoogle,
			StatusCode: resp.StatusCode,
			Body:       string(respBody),
		}
	}

	var apiResp googleResponse
	limitedBody := io.LimitReader(resp.Body, 10*1024*1024)
	if err := json.NewDecoder(limitedBody).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("decode google response: %w", err)
	}

	text := ""
	if len(apiResp.Candidates) > 0 && len(apiResp.Candidates[0].Content.Parts) > 0 {
		text = apiResp.Candidates[0].Content.Parts[0].Text
	}

	return &ChatResult{
		Text:         text,
		InputTokens:  apiResp.UsageMetadata.PromptTokenCount,
		OutputTokens: apiResp.UsageMetadata.CandidatesTokenCount,
	}, nil
}
