package engine

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/KranzL/shipmates-oss/go-llm"
)

const (
	llmTimeoutMs      = 120000
	jitterFactor      = 0.2
	maxIterationsConst = 3
)

var apiRetryDelays = []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second}

type IterationOptions struct {
	Client           llm.Client
	RepoDir          string
	Task             string
	Repo             string
	FileTree         string
	FullFiles        []FileWithContent
	AgentScope       *AgentScope
	AgentPersonality string
	MaxIterations    int
	MaxSketchTokens  int
	OnEvent          func(event map[string]interface{})
	Ctx              context.Context
}

type IterationResult struct {
	TotalApplied    int
	TotalFailed     int
	Iterations      int
	AppliedFiles    []string
	CreatedFiles    []string
	LastApplyResult *ApplyResult
	TestsPassed     *bool
	TestOutput      string
}

func summarizeError(errorMsg string) string {
	if len(errorMsg) > 120 {
		return errorMsg[:120]
	}
	return errorMsg
}

func applyJitter(delayMs time.Duration) time.Duration {
	jitter := float64(delayMs) * jitterFactor * (rand.Float64()*2 - 1)
	return time.Duration(int64(float64(delayMs) + jitter))
}

func withTimeout(ctx context.Context, fn func() (*llm.ChatResult, error), timeoutMs int) (*llm.ChatResult, error) {
	resultChan := make(chan *llm.ChatResult, 1)
	errChan := make(chan error, 1)

	go func() {
		result, err := fn()
		if err != nil {
			errChan <- err
		} else {
			resultChan <- result
		}
	}()

	timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()

	select {
	case result := <-resultChan:
		return result, nil
	case err := <-errChan:
		return nil, err
	case <-timeoutCtx.Done():
		return nil, fmt.Errorf("LLM API call timed out after %dms", timeoutMs)
	}
}

func CallWithRetry(ctx context.Context, fn func() (*llm.ChatResult, error)) (*llm.ChatResult, error) {
	var lastErr error

	for attempt := 0; attempt <= len(apiRetryDelays); attempt++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		result, err := fn()
		if err == nil {
			return result, nil
		}

		lastErr = err

		if attempt < len(apiRetryDelays) {
			delay := applyJitter(apiRetryDelays[attempt])
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}

	return nil, lastErr
}

func BuildFailedEditContext(repoDir string, failedEdits []ApplyResultEntry) ([]FailedEditContext, error) {
	var contexts []FailedEditContext

	for _, failed := range failedEdits {
		if failed.Success {
			continue
		}
		absolutePath := filepath.Join(repoDir, failed.FilePath)
		actualContent := ""
		data, err := os.ReadFile(absolutePath)
		if err == nil {
			actualContent = string(data)
		}

		contexts = append(contexts, FailedEditContext{
			FilePath:      failed.FilePath,
			SearchText:    failed.OriginalSearchText,
			ActualContent: actualContent,
		})
	}

	return contexts, nil
}

func IterateSketchApply(opts IterationOptions) (*IterationResult, error) {
	systemPrompt := BuildSketchSystemPrompt()
	var previousErrors string
	failedEditContexts := make([]FailedEditContext, 0, 10)
	var lastApplyResult *ApplyResult
	accumulatedApplied := make([]string, 0, 50)
	accumulatedCreated := make([]string, 0, 20)
	actualIterations := 0

	for iteration := 1; iteration <= opts.MaxIterations; iteration++ {
		if opts.Ctx.Err() != nil {
			break
		}

		actualIterations = iteration

		userPromptOpts := SketchPromptOptions{
			Task:             opts.Task,
			Repo:             opts.Repo,
			FileTree:         opts.FileTree,
			FullFiles:        opts.FullFiles,
			AgentPersonality: opts.AgentPersonality,
			AgentScope:       opts.AgentScope,
		}

		if iteration > 1 {
			userPromptOpts.PreviousErrors = previousErrors
			userPromptOpts.FailedEdits = failedEditContexts
			userPromptOpts.Iteration = iteration
		}

		userPrompt := BuildSketchUserPrompt(userPromptOpts)

		var rawSketchText string
		result, err := CallWithRetry(opts.Ctx, func() (*llm.ChatResult, error) {
			messages := []llm.ChatMessage{
				{Role: "user", Content: userPrompt},
			}
			return withTimeout(opts.Ctx, func() (*llm.ChatResult, error) {
				return opts.Client.Chat(opts.Ctx, messages, systemPrompt, opts.MaxSketchTokens)
			}, llmTimeoutMs)
		})

		if err != nil {
			if opts.Ctx.Err() != nil {
				break
			}
			previousErrors = err.Error()
			continue
		}
		rawSketchText = result.Text

		delegations := ExtractDelegationBlocks(rawSketchText)
		for _, d := range delegations {
			if opts.OnEvent != nil {
				opts.OnEvent(map[string]interface{}{
					"type":        "delegation_requested",
					"toAgentId":   d.ToAgentID,
					"description": d.Description,
				})
			}
		}

		sketch, parseErr := ParseSketchResponse(rawSketchText)
		if parseErr != nil {
			correctionPrompt := "Your previous response was not valid JSON. Please respond with ONLY a valid JSON object."
			retryResult, retryErr := CallWithRetry(opts.Ctx, func() (*llm.ChatResult, error) {
				messages := []llm.ChatMessage{
					{Role: "user", Content: userPrompt},
					{Role: "assistant", Content: rawSketchText},
					{Role: "user", Content: correctionPrompt},
				}
				return withTimeout(opts.Ctx, func() (*llm.ChatResult, error) {
					return opts.Client.Chat(opts.Ctx, messages, systemPrompt, opts.MaxSketchTokens)
				}, llmTimeoutMs)
			})

			if retryErr != nil {
				previousErrors = retryErr.Error()
				continue
			}

			sketch, parseErr = ParseSketchResponse(retryResult.Text)
			if parseErr != nil {
				previousErrors = parseErr.Error()
				continue
			}
		}

		sketch = ValidateAgentScope(sketch, opts.AgentScope)

		if iteration > 1 && opts.OnEvent != nil {
			opts.OnEvent(map[string]interface{}{
				"type":    "retry",
				"attempt": iteration,
				"reason":  summarizeError(previousErrors),
			})
		}

		if opts.OnEvent != nil {
			steps := []string{}
			if sketch.Explanation != "" {
				steps = append(steps, sketch.Explanation)
			}
			opts.OnEvent(map[string]interface{}{
				"type":  "plan",
				"steps": steps,
			})
		}

		applyResult, err := ApplyEdits(opts.RepoDir, sketch.Edits, sketch.NewFiles)
		if err != nil {
			previousErrors = err.Error()
			continue
		}

		lastApplyResult = applyResult

		for _, applied := range applyResult.Applied {
			if applied.Success {
				accumulatedApplied = append(accumulatedApplied, applied.FilePath)
				if opts.OnEvent != nil {
					opts.OnEvent(map[string]interface{}{
						"type":      "file_edit",
						"path":      applied.FilePath,
						"additions": 0,
						"deletions": 0,
					})
				}
			}
		}

		for _, created := range applyResult.Created {
			if created.Success {
				accumulatedCreated = append(accumulatedCreated, created.FilePath)
				if opts.OnEvent != nil {
					opts.OnEvent(map[string]interface{}{
						"type": "file_create",
						"path": created.FilePath,
					})
				}
			}
		}

		if applyResult.TotalFailed == 0 {
			return &IterationResult{
				TotalApplied:    applyResult.TotalApplied,
				TotalFailed:     0,
				Iterations:      actualIterations,
				AppliedFiles:    accumulatedApplied,
				CreatedFiles:    accumulatedCreated,
				LastApplyResult: applyResult,
			}, nil
		}

		if applyResult.TotalApplied == 0 && iteration > 1 {
			break
		}

		failedEdits := make([]ApplyResultEntry, 0, len(applyResult.Applied))
		for _, r := range applyResult.Applied {
			if !r.Success {
				failedEdits = append(failedEdits, r)
			}
		}

		failedEditContexts, _ = BuildFailedEditContext(opts.RepoDir, failedEdits)

		errorLines := make([]string, 0, opts.MaxIterations)
		for _, r := range applyResult.Applied {
			if !r.Success {
				errorLines = append(errorLines, fmt.Sprintf("%s: %s", r.FilePath, r.Error))
			}
		}
		previousErrors = strings.Join(errorLines, "\n")
	}

	totalApplied := 0
	totalFailed := 0
	if lastApplyResult != nil {
		totalApplied = lastApplyResult.TotalApplied
		totalFailed = lastApplyResult.TotalFailed
	}

	return &IterationResult{
		TotalApplied:    totalApplied,
		TotalFailed:     totalFailed,
		Iterations:      actualIterations,
		AppliedFiles:    accumulatedApplied,
		CreatedFiles:    accumulatedCreated,
		LastApplyResult: lastApplyResult,
	}, nil
}
