package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	openaiDefaultModel    = "gpt-4o"
	veniceDefaultModel    = "qwen3-coder-480b-a35b-instruct"
	groqDefaultModel      = "llama-3.3-70b-versatile"
	togetherDefaultModel  = "meta-llama/Llama-3.3-70B-Instruct-Turbo"
	fireworksDefaultModel = "accounts/fireworks/models/llama-v3p3-70b-instruct"
)

var openaiHTTPClient = &http.Client{
	Timeout: 120 * time.Second,
}

type openaiCompatClient struct {
	provider Provider
	apiKey   string
	baseURL  string
	modelID  string
}

type openaiRequest struct {
	Model     string          `json:"model"`
	MaxTokens int             `json:"max_tokens"`
	Messages  []openaiMessage `json:"messages"`
}

type openaiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openaiResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

func newOpenAICompatClient(provider Provider, apiKey string, baseURL string, modelID string) *openaiCompatClient {
	return &openaiCompatClient{provider: provider, apiKey: apiKey, baseURL: strings.TrimRight(baseURL, "/"), modelID: modelID}
}

func (c *openaiCompatClient) Chat(ctx context.Context, messages []ChatMessage, systemPrompt string, maxTokens int) (*ChatResult, error) {
	apiMessages := make([]openaiMessage, 0, len(messages)+1)
	if systemPrompt != "" {
		apiMessages = append(apiMessages, openaiMessage{Role: "system", Content: systemPrompt})
	}
	for _, m := range messages {
		apiMessages = append(apiMessages, openaiMessage{Role: m.Role, Content: m.Content})
	}

	reqBody := openaiRequest{
		Model:     c.modelID,
		MaxTokens: maxTokens,
		Messages:  apiMessages,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal openai request: %w", err)
	}

	endpoint := c.baseURL + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create openai request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := openaiHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai API call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		limitedReader := io.LimitReader(resp.Body, 4096)
		respBody, _ := io.ReadAll(limitedReader)
		return nil, &ProviderError{
			Provider:   c.provider,
			StatusCode: resp.StatusCode,
			Body:       string(respBody),
		}
	}

	var apiResp openaiResponse
	limitedBody := io.LimitReader(resp.Body, 10*1024*1024)
	if err := json.NewDecoder(limitedBody).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("decode openai response: %w", err)
	}

	text := ""
	if len(apiResp.Choices) > 0 {
		text = apiResp.Choices[0].Message.Content
	}

	return &ChatResult{
		Text:         text,
		InputTokens:  apiResp.Usage.PromptTokens,
		OutputTokens: apiResp.Usage.CompletionTokens,
	}, nil
}
