package llm

import (
	"context"
	"fmt"
)

type Provider string

const (
	ProviderAnthropic Provider = "anthropic"
	ProviderOpenAI    Provider = "openai"
	ProviderGoogle    Provider = "google"
	ProviderVenice    Provider = "venice"
	ProviderGroq      Provider = "groq"
	ProviderTogether  Provider = "together"
	ProviderFireworks Provider = "fireworks"
	ProviderCustom    Provider = "custom"
)

func ValidProvider(p Provider) bool {
	switch p {
	case ProviderAnthropic, ProviderOpenAI, ProviderGoogle,
		ProviderVenice, ProviderGroq, ProviderTogether,
		ProviderFireworks, ProviderCustom:
		return true
	}
	return false
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatResult struct {
	Text         string `json:"text"`
	InputTokens  int    `json:"inputTokens"`
	OutputTokens int    `json:"outputTokens"`
}

type Client interface {
	Chat(ctx context.Context, messages []ChatMessage, systemPrompt string, maxTokens int) (*ChatResult, error)
}

type ClientOptions struct {
	BaseURL string
	ModelID string
}

type ProviderError struct {
	Provider   Provider
	StatusCode int
	Body       string
}

func (e *ProviderError) Error() string {
	return fmt.Sprintf("%s API returned %d: %s", e.Provider, e.StatusCode, e.Body)
}
