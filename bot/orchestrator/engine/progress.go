package engine

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/KranzL/shipmates-oss/bot/orchestrator"
)

type ProgressEmitter struct {
	relay *orchestrator.RelayConnection
	logFn func(ctx context.Context, msg string) (string, error)
}

func NewProgressEmitter(relay *orchestrator.RelayConnection, logFn func(ctx context.Context, msg string) (string, error)) *ProgressEmitter {
	return &ProgressEmitter{
		relay: relay,
		logFn: logFn,
	}
}

func (pe *ProgressEmitter) EmitEvent(event map[string]interface{}) {
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	pe.relay.Push(string(data) + "\n")
}

func (pe *ProgressEmitter) EmitPhase(phase, message string) {
	pe.EmitEvent(map[string]interface{}{
		"type":    "phase",
		"phase":   phase,
		"message": message,
	})
}

func (pe *ProgressEmitter) EmitFileRead(path string) {
	pe.EmitEvent(map[string]interface{}{
		"type": "file_read",
		"path": path,
	})
}

func (pe *ProgressEmitter) EmitFileEdit(path string, additions, deletions int) {
	pe.EmitEvent(map[string]interface{}{
		"type":      "file_edit",
		"path":      path,
		"additions": additions,
		"deletions": deletions,
	})
}

func (pe *ProgressEmitter) EmitFileCreate(path string) {
	pe.EmitEvent(map[string]interface{}{
		"type": "file_create",
		"path": path,
	})
}

func (pe *ProgressEmitter) EmitPlan(steps []string) {
	pe.EmitEvent(map[string]interface{}{
		"type":  "plan",
		"steps": steps,
	})
}

func (pe *ProgressEmitter) EmitTestRun(command string) {
	pe.EmitEvent(map[string]interface{}{
		"type":    "test_run",
		"command": command,
	})
}

func (pe *ProgressEmitter) EmitTestResult(passed bool, summary string) {
	pe.EmitEvent(map[string]interface{}{
		"type":    "test_result",
		"passed":  passed,
		"summary": summary,
	})
}

func (pe *ProgressEmitter) EmitCommit(message string, files []string) {
	pe.EmitEvent(map[string]interface{}{
		"type":    "commit",
		"message": message,
		"files":   files,
	})
}

func (pe *ProgressEmitter) EmitThought(content string) {
	pe.EmitEvent(map[string]interface{}{
		"type":    "thought",
		"content": content,
	})
}

func (pe *ProgressEmitter) EmitError(message string, recoverable bool) {
	pe.EmitEvent(map[string]interface{}{
		"type":        "error",
		"message":     message,
		"recoverable": recoverable,
	})
}

func (pe *ProgressEmitter) EmitRetry(attempt int, reason string) {
	pe.EmitEvent(map[string]interface{}{
		"type":    "retry",
		"attempt": attempt,
		"reason":  reason,
	})
}

func (pe *ProgressEmitter) LogPhase(ctx context.Context, phase, message string) (string, error) {
	return pe.logFn(ctx, fmt.Sprintf("[%s] %s", phase, message))
}

func (pe *ProgressEmitter) LogCompletion(ctx context.Context, branchName string, editedFiles, createdFiles []string, prUrl string) (string, error) {
	parts := []string{fmt.Sprintf("Done. Branch: `%s`", branchName)}

	var fileParts []string
	if len(editedFiles) > 0 {
		fileParts = append(fileParts, fmt.Sprintf("Edited: %v", editedFiles))
	}
	if len(createdFiles) > 0 {
		fileParts = append(fileParts, fmt.Sprintf("Created: %v", createdFiles))
	}
	if len(fileParts) > 0 {
		parts = append(parts, fmt.Sprintf("%v", fileParts))
	}
	if prUrl != "" {
		parts = append(parts, fmt.Sprintf("PR: %s", prUrl))
	}

	message := ""
	for i, part := range parts {
		if i > 0 {
			message += "\n"
		}
		message += part
	}

	return pe.logFn(ctx, message)
}

func (pe *ProgressEmitter) LogError(ctx context.Context, message string) (string, error) {
	return pe.logFn(ctx, message)
}

func (pe *ProgressEmitter) LogPlan(ctx context.Context, explanation string) (string, error) {
	return pe.logFn(ctx, fmt.Sprintf("Plan: %s", explanation))
}

func (pe *ProgressEmitter) PushStatus(status string) {
	pe.relay.PushStatus(status)
}

func (pe *ProgressEmitter) Close() {
	pe.relay.Close()
}
