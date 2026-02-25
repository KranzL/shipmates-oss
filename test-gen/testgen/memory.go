package testgen

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

func (mc *MemoryClient) FetchTestConventions(userID string, repo string) ([]string, error) {
	if mc.baseURL == "" {
		return nil, nil
	}

	params := url.Values{}
	params.Set("q", "test convention")
	params.Set("scope", "repo")
	params.Set("limit", "10")

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

	var conventions []string
	for _, r := range result.Results {
		conventions = append(conventions, r.Content)
	}

	return conventions, nil
}
