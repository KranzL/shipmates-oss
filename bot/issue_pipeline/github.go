package issue_pipeline

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

var (
	repoFormatPattern   = regexp.MustCompile(`^[a-zA-Z0-9_-]+/[a-zA-Z0-9_-]+$`)
	branchFormatPattern = regexp.MustCompile(`^(?!refs/)[a-zA-Z0-9_.\-]+(/[a-zA-Z0-9_.\-]+)*$`)

	githubHTTPClient = &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 15 * time.Second,
			MaxIdleConns:          100,
			MaxIdleConnsPerHost:   10,
			IdleConnTimeout:       90 * time.Second,
			DialContext: (&net.Dialer{
				Timeout:   10 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
		},
	}

	githubLimiter = rate.NewLimiter(rate.Every(time.Second), 3)
)

type PullRequest struct {
	Number int
	URL    string
}

func escapeMarkdown(s string) string {
	replacer := strings.NewReplacer(
		"[", "\\[",
		"]", "\\]",
		"(", "\\(",
		")", "\\)",
		"*", "\\*",
		"_", "\\_",
		"`", "\\`",
		"<", "\\<",
		">", "\\>",
	)
	return replacer.Replace(s)
}

func createPullRequest(ctx context.Context, token, repo, head, base, title, body string) (*PullRequest, error) {
	if !repoFormatPattern.MatchString(repo) {
		return nil, fmt.Errorf("invalid repository format: %s", repo)
	}
	if !branchFormatPattern.MatchString(head) {
		return nil, fmt.Errorf("invalid head branch name: %s", head)
	}
	if !branchFormatPattern.MatchString(base) {
		return nil, fmt.Errorf("invalid base branch name: %s", base)
	}

	if err := githubLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limit wait: %w", err)
	}

	url := fmt.Sprintf("https://api.github.com/repos/%s/pulls", repo)

	payload := map[string]string{
		"title": title,
		"body":  body,
		"head":  head,
		"base":  base,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, "POST", url, bytes.NewReader(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := githubHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 10*1024))
		return nil, fmt.Errorf("failed to create pull request: %d %s", resp.StatusCode, string(bodyBytes))
	}

	var result struct {
		Number  int    `json:"number"`
		HTMLURL string `json:"html_url"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &PullRequest{
		Number: result.Number,
		URL:    result.HTMLURL,
	}, nil
}

func commentOnIssue(ctx context.Context, token, repo string, issueNumber int, body string) error {
	if !repoFormatPattern.MatchString(repo) {
		return fmt.Errorf("invalid repository format: %s", repo)
	}

	if err := githubLimiter.Wait(ctx); err != nil {
		return fmt.Errorf("rate limit wait: %w", err)
	}

	url := fmt.Sprintf("https://api.github.com/repos/%s/issues/%d/comments", repo, issueNumber)

	payload := map[string]string{
		"body": body,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, "POST", url, bytes.NewReader(payloadBytes))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := githubHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 10*1024))
		return fmt.Errorf("failed to comment on issue: %d %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}

func getDefaultBranch(ctx context.Context, token, repo string) (string, error) {
	if !repoFormatPattern.MatchString(repo) {
		return "", fmt.Errorf("invalid repository format: %s", repo)
	}

	if err := githubLimiter.Wait(ctx); err != nil {
		return "", fmt.Errorf("rate limit wait: %w", err)
	}

	url := fmt.Sprintf("https://api.github.com/repos/%s", repo)

	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := githubHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 10*1024))
		return "", fmt.Errorf("failed to get repository info: %d %s", resp.StatusCode, string(bodyBytes))
	}

	var result struct {
		DefaultBranch string `json:"default_branch"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}

	return result.DefaultBranch, nil
}
