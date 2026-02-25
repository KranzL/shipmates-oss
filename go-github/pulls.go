package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
)

func (c *Client) FetchDiff(ctx context.Context, repo string, prNumber int) (string, error) {
	path := fmt.Sprintf("/repos/%s/pulls/%d", repo, prNumber)

	resp, err := c.doRequest(ctx, "GET", path, nil, "application/vnd.github.v3.diff")
	if err != nil {
		return "", fmt.Errorf("fetch diff: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		limitedReader := io.LimitReader(resp.Body, 4096)
		respBody, _ := io.ReadAll(limitedReader)
		return "", fmt.Errorf("fetch diff returned %d: %s", resp.StatusCode, string(respBody))
	}

	limitedBody := io.LimitReader(resp.Body, int64(maxBodyLen))
	diffBytes, err := io.ReadAll(limitedBody)
	if err != nil {
		return "", fmt.Errorf("read diff body: %w", err)
	}

	return string(diffBytes), nil
}

func (c *Client) CreateReview(ctx context.Context, repo string, prNumber int, review Review) error {
	path := fmt.Sprintf("/repos/%s/pulls/%d/reviews", repo, prNumber)

	body, err := json.Marshal(review)
	if err != nil {
		return fmt.Errorf("marshal review: %w", err)
	}

	return c.doJSON(ctx, "POST", path, bytes.NewReader(body), nil)
}

func (c *Client) PostComment(ctx context.Context, repo string, issueNumber int, body string) error {
	path := fmt.Sprintf("/repos/%s/issues/%d/comments", repo, issueNumber)

	payload, err := json.Marshal(map[string]string{"body": body})
	if err != nil {
		return fmt.Errorf("marshal comment: %w", err)
	}

	return c.doJSON(ctx, "POST", path, bytes.NewReader(payload), nil)
}

func (c *Client) FetchPullRequest(ctx context.Context, repo string, prNumber int) (*PullRequest, error) {
	path := fmt.Sprintf("/repos/%s/pulls/%d", repo, prNumber)

	var pr PullRequest
	if err := c.doJSON(ctx, "GET", path, nil, &pr); err != nil {
		return nil, fmt.Errorf("fetch pull request: %w", err)
	}

	return &pr, nil
}
