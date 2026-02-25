package voice

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

var ErrLowConfidence = errors.New("speech transcription confidence too low")

const (
	DeepgramAPIURL           = "https://api.deepgram.com/v1/listen"
	DeepgramModel            = "nova-2"
	DeepgramLanguage         = "en"
	DeepgramEncoding         = "linear16"
	DeepgramSampleRate       = "48000"
	DeepgramChannels         = "2"
	MaxSTTRequestBytes       = 10 * 1024 * 1024
	STTTimeout               = 30 * time.Second
	MaxSTTResponseBytes      = 1 * 1024 * 1024
	MaxSTTRetries            = 3
	STTRetryBaseDelay        = 500 * time.Millisecond
	minConfidenceThreshold   = 0.6
)

type STTClient struct {
	apiKey     string
	httpClient *http.Client
	requestURL string
}

type deepgramResponse struct {
	Results struct {
		Channels []struct {
			Alternatives []struct {
				Transcript string  `json:"transcript"`
				Confidence float64 `json:"confidence"`
			} `json:"alternatives"`
		} `json:"channels"`
	} `json:"results"`
}

func NewSTTClient(apiKey string) (*STTClient, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("deepgram API key is required")
	}

	httpClient := &http.Client{
		Timeout: STTTimeout,
		Transport: &http.Transport{
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
			TLSHandshakeTimeout: 10 * time.Second,
		},
	}

	params := url.Values{}
	params.Set("model", DeepgramModel)
	params.Set("language", DeepgramLanguage)
	params.Set("smart_format", "true")
	params.Set("encoding", DeepgramEncoding)
	params.Set("sample_rate", DeepgramSampleRate)
	params.Set("channels", DeepgramChannels)
	requestURL := DeepgramAPIURL + "?" + params.Encode()

	return &STTClient{
		apiKey:     apiKey,
		httpClient: httpClient,
		requestURL: requestURL,
	}, nil
}

func (c *STTClient) Transcribe(ctx context.Context, pcmData []byte) (string, error) {
	if len(pcmData) == 0 {
		return "", fmt.Errorf("empty audio data")
	}

	if len(pcmData) > MaxSTTRequestBytes {
		return "", fmt.Errorf("audio data exceeds maximum size of %d bytes", MaxSTTRequestBytes)
	}

	var lastErr error
	for attempt := 0; attempt < MaxSTTRetries; attempt++ {
		if attempt > 0 {
			delay := STTRetryBaseDelay * time.Duration(1<<uint(attempt-1))
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(delay):
			}
		}

		transcript, err := c.doTranscribeRequest(ctx, c.requestURL, pcmData)
		if err == nil {
			return transcript, nil
		}

		if errors.Is(err, ErrLowConfidence) {
			return "", err
		}

		lastErr = err

		if isClientError(err) {
			return "", err
		}
	}

	return "", fmt.Errorf("transcription failed after %d attempts: %w", MaxSTTRetries, lastErr)
}

func (c *STTClient) doTranscribeRequest(ctx context.Context, requestURL string, pcmData []byte) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, bytes.NewReader(pcmData))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Authorization", "Token "+c.apiKey)
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	limitedReader := io.LimitReader(resp.Body, int64(MaxSTTResponseBytes))
	body, err := io.ReadAll(limitedReader)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		sanitizedBody := string(body)
		if len(sanitizedBody) > 500 {
			sanitizedBody = sanitizedBody[:500]
		}
		return "", &apiError{
			statusCode: resp.StatusCode,
			message:    sanitizedBody,
		}
	}

	var result deepgramResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}

	if len(result.Results.Channels) > 0 && len(result.Results.Channels[0].Alternatives) > 0 {
		alt := result.Results.Channels[0].Alternatives[0]
		if alt.Confidence > 0 && alt.Confidence < minConfidenceThreshold {
			return "", ErrLowConfidence
		}
		return alt.Transcript, nil
	}

	return "", nil
}

func (c *STTClient) Close() {
	c.httpClient.CloseIdleConnections()
}

type apiError struct {
	statusCode int
	message    string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("API error %d: %s", e.statusCode, e.message)
}

func isClientError(err error) bool {
	if apiErr, ok := err.(*apiError); ok {
		return apiErr.statusCode >= 400 && apiErr.statusCode < 500
	}
	return false
}
