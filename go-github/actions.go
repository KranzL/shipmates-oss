package github

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
)

func (c *Client) FetchWorkflowRun(ctx context.Context, repo string, runID int64) (*WorkflowRun, error) {
	path := fmt.Sprintf("/repos/%s/actions/runs/%d", repo, runID)

	var run WorkflowRun
	if err := c.doJSON(ctx, "GET", path, nil, &run); err != nil {
		return nil, fmt.Errorf("fetch workflow run: %w", err)
	}

	return &run, nil
}

func (c *Client) FetchWorkflowLogs(ctx context.Context, repo string, runID int64) (map[string]string, error) {
	path := fmt.Sprintf("/repos/%s/actions/runs/%d/logs", repo, runID)

	resp, err := c.doRequest(ctx, "GET", path, nil, "")
	if err != nil {
		return nil, fmt.Errorf("fetch workflow logs: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		limitedReader := io.LimitReader(resp.Body, 4096)
		respBody, _ := io.ReadAll(limitedReader)
		return nil, fmt.Errorf("fetch workflow logs returned %d: %s", resp.StatusCode, string(respBody))
	}

	zipData, err := io.ReadAll(io.LimitReader(resp.Body, 50*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read workflow logs: %w", err)
	}

	reader, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return nil, fmt.Errorf("open zip: %w", err)
	}

	logs := make(map[string]string, len(reader.File))
	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}

		rc, err := file.Open()
		if err != nil {
			continue
		}

		content, err := io.ReadAll(io.LimitReader(rc, 1*1024*1024))
		rc.Close()
		if err != nil {
			continue
		}

		name := file.Name
		if idx := strings.LastIndex(name, "/"); idx >= 0 {
			name = name[idx+1:]
		}
		logs[name] = string(content)
	}

	return logs, nil
}
