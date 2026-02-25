package diagnosis

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

type MemoryClient struct {
	baseURL    string
	secret     string
	httpClient *http.Client
}

func NewMemoryClient(baseURL string, secret string) *MemoryClient {
	return &MemoryClient{
		baseURL: baseURL,
		secret:  secret,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

type memorySearchResult struct {
	Results []struct {
		Content string `json:"content"`
	} `json:"results"`
}

func (mc *MemoryClient) FetchPastFailures(userID string, errorText string) ([]string, error) {
	if mc.baseURL == "" {
		return nil, nil
	}

	query := errorText
	if len(query) > 200 {
		query = query[:200]
	}

	params := url.Values{}
	params.Set("q", "CI failure "+query)
	params.Set("userId", userID)
	params.Set("limit", "5")

	reqURL := fmt.Sprintf("%s/memories/search?%s", mc.baseURL, params.Encode())

	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create memory request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+mc.secret)
	req.Header.Set("X-User-ID", userID)

	resp, err := mc.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("query memory service: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil
	}

	var result memorySearchResult
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1*1024*1024)).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode memory response: %w", err)
	}

	var failures []string
	for _, r := range result.Results {
		failures = append(failures, r.Content)
	}

	return failures, nil
}
