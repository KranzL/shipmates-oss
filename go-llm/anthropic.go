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
	anthropicEndpoint     = "https://api.anthropic.com/v1/messages"
	anthropicDefaultModel = "claude-sonnet-4-5-20250929"
	anthropicAPIVersion   = "2023-06-01"
)

var anthropicHTTPClient = &http.Client{
	Timeout: 120 * time.Second,
}

type anthropicClient struct {
	apiKey  string
	modelID string
}

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

func newAnthropicClient(apiKey string, modelID string) *anthropicClient {
	if modelID == "" {
		modelID = anthropicDefaultModel
	}
	return &anthropicClient{apiKey: apiKey, modelID: modelID}
}

func (c *anthropicClient) Chat(ctx context.Context, messages []ChatMessage, systemPrompt string, maxTokens int) (*ChatResult, error) {
	apiMessages := make([]anthropicMessage, len(messages))
	for i, m := range messages {
		apiMessages[i] = anthropicMessage{Role: m.Role, Content: m.Content}
	}

	reqBody := anthropicRequest{
		Model:     c.modelID,
		MaxTokens: maxTokens,
		System:    systemPrompt,
		Messages:  apiMessages,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal anthropic request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", anthropicEndpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create anthropic request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", anthropicAPIVersion)

	resp, err := anthropicHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("anthropic API call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		limitedReader := io.LimitReader(resp.Body, 4096)
		respBody, _ := io.ReadAll(limitedReader)
		return nil, &ProviderError{
			Provider:   ProviderAnthropic,
			StatusCode: resp.StatusCode,
			Body:       string(respBody),
		}
	}

	var apiResp anthropicResponse
	limitedBody := io.LimitReader(resp.Body, 10*1024*1024)
	if err := json.NewDecoder(limitedBody).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("decode anthropic response: %w", err)
	}

	if len(apiResp.Content) == 0 {
		return nil, fmt.Errorf("empty anthropic response")
	}

	text := ""
	for _, block := range apiResp.Content {
		if block.Type == "text" {
			text = block.Text
			break
		}
	}

	return &ChatResult{
		Text:         text,
		InputTokens:  apiResp.Usage.InputTokens,
		OutputTokens: apiResp.Usage.OutputTokens,
	}, nil
}
