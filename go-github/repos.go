package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
)

func (c *Client) ListCommits(ctx context.Context, repo string, sha string, limit int) ([]CommitResponse, error) {
	path := fmt.Sprintf("/repos/%s/commits?per_page=%d", repo, limit)
	if sha != "" {
		path += "&sha=" + sha
	}

	var commits []CommitResponse
	if err := c.doJSON(ctx, "GET", path, nil, &commits); err != nil {
		return nil, fmt.Errorf("list commits: %w", err)
	}

	return commits, nil
}

func (c *Client) GetDefaultBranch(ctx context.Context, repo string) (string, error) {
	path := fmt.Sprintf("/repos/%s", repo)

	var repoData Repository
	if err := c.doJSON(ctx, "GET", path, nil, &repoData); err != nil {
		return "", fmt.Errorf("get default branch: %w", err)
	}

	return repoData.DefaultBranch, nil
}

func (c *Client) CreatePullRequest(ctx context.Context, repo string, request CreatePRRequest) (*CreatePRResponse, error) {
	path := fmt.Sprintf("/repos/%s/pulls", repo)

	body, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("marshal create PR: %w", err)
	}

	var response CreatePRResponse
	if err := c.doJSON(ctx, "POST", path, bytes.NewReader(body), &response); err != nil {
		return nil, fmt.Errorf("create pull request: %w", err)
	}

	return &response, nil
}
