package llm

import "fmt"

func NewClient(provider Provider, apiKey string, options *ClientOptions) (Client, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("API key is required for provider %s", provider)
	}

	opts := ClientOptions{}
	if options != nil {
		opts = *options
	}

	switch provider {
	case ProviderAnthropic:
		return newAnthropicClient(apiKey, opts.ModelID), nil
	case ProviderOpenAI:
		modelID := opts.ModelID
		if modelID == "" {
			modelID = openaiDefaultModel
		}
		return newOpenAICompatClient(ProviderOpenAI, apiKey, "https://api.openai.com/v1", modelID), nil
	case ProviderGoogle:
		return newGoogleClient(apiKey, opts.ModelID), nil
	case ProviderVenice:
		modelID := opts.ModelID
		if modelID == "" {
			modelID = veniceDefaultModel
		}
		return newOpenAICompatClient(ProviderVenice, apiKey, "https://api.venice.ai/api/v1", modelID), nil
	case ProviderGroq:
		modelID := opts.ModelID
		if modelID == "" {
			modelID = groqDefaultModel
		}
		return newOpenAICompatClient(ProviderGroq, apiKey, "https://api.groq.com/openai/v1", modelID), nil
	case ProviderTogether:
		modelID := opts.ModelID
		if modelID == "" {
			modelID = togetherDefaultModel
		}
		return newOpenAICompatClient(ProviderTogether, apiKey, "https://api.together.xyz/v1", modelID), nil
	case ProviderFireworks:
		modelID := opts.ModelID
		if modelID == "" {
			modelID = fireworksDefaultModel
		}
		return newOpenAICompatClient(ProviderFireworks, apiKey, "https://api.fireworks.ai/inference/v1", modelID), nil
	case ProviderCustom:
		if opts.BaseURL == "" {
			return nil, fmt.Errorf("base URL is required for custom provider")
		}
		modelID := opts.ModelID
		if modelID == "" {
			modelID = "default"
		}
		return newOpenAICompatClient(ProviderCustom, apiKey, opts.BaseURL, modelID), nil
	default:
		return nil, fmt.Errorf("unsupported provider: %s", provider)
	}
}
