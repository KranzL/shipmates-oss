package voice

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"time"
)

const (
	ElevenLabsAPIURL        = "https://api.elevenlabs.io/v1/text-to-speech"
	ElevenLabsModel         = "eleven_turbo_v2_5"
	ElevenLabsOutputFormat  = "pcm_24000"
	MaxTTSTextLength        = 5000
	MaxTTSResponseBytes     = 10 * 1024 * 1024
	TTSTimeout              = 30 * time.Second
	MaxTTSRetries           = 3
	TTSRetryBaseDelay       = 500 * time.Millisecond
)

var validVoiceIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

type TTSClient struct {
	apiKey     string
	httpClient *http.Client
}

type ttsRequest struct {
	Text    string `json:"text"`
	ModelID string `json:"model_id"`
}

func NewTTSClient(apiKey string) (*TTSClient, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("elevenlabs API key is required")
	}

	httpClient := &http.Client{
		Timeout: TTSTimeout,
		Transport: &http.Transport{
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
			TLSHandshakeTimeout: 10 * time.Second,
		},
	}

	return &TTSClient{
		apiKey:     apiKey,
		httpClient: httpClient,
	}, nil
}

func (c *TTSClient) Synthesize(ctx context.Context, text, voiceID string) ([]byte, error) {
	if text == "" {
		return nil, fmt.Errorf("text is required")
	}
	if voiceID == "" {
		return nil, fmt.Errorf("voice ID is required")
	}
	if len(voiceID) < 1 || len(voiceID) > 64 {
		return nil, fmt.Errorf("voice ID length must be between 1 and 64 characters")
	}
	if !validVoiceIDPattern.MatchString(voiceID) {
		return nil, fmt.Errorf("invalid voice ID format")
	}

	if len(text) > MaxTTSTextLength {
		text = text[:MaxTTSTextLength]
	}

	requestURL := fmt.Sprintf("%s/%s?output_format=%s", ElevenLabsAPIURL, voiceID, ElevenLabsOutputFormat)

	var lastErr error
	for attempt := 0; attempt < MaxTTSRetries; attempt++ {
		if attempt > 0 {
			delay := TTSRetryBaseDelay * time.Duration(1<<uint(attempt-1))
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}

		audio, err := c.doSynthesizeRequest(ctx, requestURL, text)
		if err == nil {
			return audio, nil
		}

		lastErr = err

		if isClientError(err) {
			return nil, err
		}
	}

	return nil, fmt.Errorf("synthesis failed after %d attempts: %w", MaxTTSRetries, lastErr)
}

func (c *TTSClient) doSynthesizeRequest(ctx context.Context, requestURL, text string) ([]byte, error) {
	reqBody := ttsRequest{
		Text:    text,
		ModelID: ElevenLabsModel,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("xi-api-key", c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/octet-stream")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		limitedReader := io.LimitReader(resp.Body, 500)
		errBody, _ := io.ReadAll(limitedReader)
		return nil, &apiError{
			statusCode: resp.StatusCode,
			message:    string(errBody),
		}
	}

	limitedReader := io.LimitReader(resp.Body, int64(MaxTTSResponseBytes))
	audio, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	return audio, nil
}

func (c *TTSClient) Close() {
	c.httpClient.CloseIdleConnections()
}
