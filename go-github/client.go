package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"
)

const (
	baseURL    = "https://api.github.com"
	maxBodyLen = 10 * 1024 * 1024
)

type Client struct {
	token      string
	httpClient *http.Client

	mu             sync.Mutex
	rateLimitLeft  int
	rateLimitReset time.Time
}

func NewClient(token string) *Client {
	return &Client{
		token: token,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		rateLimitLeft: -1,
	}
}

func (c *Client) RateLimitRemaining() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.rateLimitLeft
}

func (c *Client) RateLimitResetsAt() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.rateLimitReset
}

func (c *Client) doRequest(ctx context.Context, method string, path string, body io.Reader, accept string) (*http.Response, error) {
	url := baseURL + path

	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("User-Agent", "shipmates-go-github/1.0")
	if accept != "" {
		req.Header.Set("Accept", accept)
	} else {
		req.Header.Set("Accept", "application/vnd.github+json")
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}

	c.updateRateLimits(resp)

	return resp, nil
}

func (c *Client) updateRateLimits(resp *http.Response) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if remaining := resp.Header.Get("X-RateLimit-Remaining"); remaining != "" {
		if n, err := strconv.Atoi(remaining); err == nil {
			c.rateLimitLeft = n
		}
	}

	if reset := resp.Header.Get("X-RateLimit-Reset"); reset != "" {
		if ts, err := strconv.ParseInt(reset, 10, 64); err == nil {
			c.rateLimitReset = time.Unix(ts, 0)
		}
	}
}

func (c *Client) doJSON(ctx context.Context, method string, path string, body io.Reader, target any) error {
	resp, err := c.doRequest(ctx, method, path, body, "")
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		limitedReader := io.LimitReader(resp.Body, 4096)
		respBody, _ := io.ReadAll(limitedReader)
		return fmt.Errorf("github API %s %s returned %d: %s", method, path, resp.StatusCode, string(respBody))
	}

	if target != nil {
		limitedBody := io.LimitReader(resp.Body, int64(maxBodyLen))
		if err := json.NewDecoder(limitedBody).Decode(target); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}

	return nil
}
