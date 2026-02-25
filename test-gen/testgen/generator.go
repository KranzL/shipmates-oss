package testgen

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	gh "github.com/KranzL/shipmates-oss/go-github"
	llm "github.com/KranzL/shipmates-oss/go-llm"
)

var (
	cloneMutexes             sync.Map
	fencePattern             = regexp.MustCompile("(?s)```(?:json)?\\s*(.+?)\\s*```")
	commaBeforeCloseBracket  = regexp.MustCompile(`,(\s*[\]})`)
	errFileTreeLimitReached  = errors.New("file tree limit reached")
	maxSourceFileSize        int64 = 1024 * 1024
	tokenSanitizePattern     = regexp.MustCompile(`https://[^@]+@`)
)

type Generator struct {
	store        *Store
	memoryClient *MemoryClient
	relayURL     string
	relaySecret  string
	db           *sql.DB
}

func NewGenerator(db *sql.DB, memoryClient *MemoryClient, relayURL string, relaySecret string) *Generator {
	return &Generator{
		store:        NewStore(db),
		memoryClient: memoryClient,
		relayURL:     relayURL,
		relaySecret:  relaySecret,
		db:           db,
	}
}

func (g *Generator) ProcessTestGen(ctx context.Context, req TestGenRequest) (*TestGenResult, error) {
	jobCtx, cancel := context.WithTimeout(ctx, MaxJobWallTime)
	defer cancel()

	jobID, err := g.store.CreateTestGen(req)
	if err != nil {
		return nil, fmt.Errorf("create test gen: %w", err)
	}

	result := &TestGenResult{
		ID:           jobID,
		TestsCreated: []string{},
	}

	var relay *RelayClient
	if req.SessionID != "" {
		relay, err = NewRelayClient(g.relayURL, g.relaySecret, req.SessionID)
		if err != nil {
			relay = nil
		}
		if relay != nil {
			defer relay.Close()
		}
	}

	if relay != nil {
		relay.EmitEvent(RelayEvent{Type: "phase", Phase: "started", Message: "Test generation started"})
	}

	provider := llm.Provider(req.Provider)
	llmClient, err := llm.NewClient(provider, req.APIKey, &llm.ClientOptions{
		BaseURL: req.BaseURL,
		ModelID: req.ModelID,
	})
	if err != nil {
		g.emitError(relay, "LLM API key is invalid or provider is unsupported. Check your API key configuration.", false)
		g.updateFailedResult(jobID, result, fmt.Sprintf("create LLM client: %v", err))
		return result, fmt.Errorf("create LLM client: %w", err)
	}

	repoDir, err := g.cloneRepo(jobCtx, req.Repo, req.GithubToken, jobID, relay)
	if err != nil {
		g.emitError(relay, fmt.Sprintf("Failed to clone %s. Check that the GitHub token has read access.", req.Repo), false)
		g.updateFailedResult(jobID, result, sanitizeTokenFromError(err))
		return result, fmt.Errorf("clone repo: %w", err)
	}
	defer os.RemoveAll(repoDir)

	if relay != nil {
		relay.EmitEvent(RelayEvent{Type: "phase", Phase: "framework_detection", Message: "Detecting test framework"})
	}

	framework, err := DetectTestFramework(repoDir)
	if err != nil {
		g.emitError(relay, "No supported test framework found. Install vitest, jest, or configure Go tests.", false)
		g.updateFailedResult(jobID, result, fmt.Sprintf("detect framework: %v", err))
		return result, fmt.Errorf("detect framework: %w", err)
	}

	if relay != nil {
		relay.EmitEvent(RelayEvent{Type: "framework_detected", Framework: string(framework.Framework), Command: framework.CoverageCommand})
	}

	if relay != nil {
		relay.EmitEvent(RelayEvent{Type: "phase", Phase: "baseline_coverage", Message: "Running baseline coverage"})
	}

	baselineCoverage, baselineTestResult, err := RunWithCoverage(jobCtx, repoDir, framework, jobID, 0, relay)
	if err != nil {
		g.emitError(relay, fmt.Sprintf("No coverage data produced. Ensure coverage is configured for %s.", framework.Framework), false)
		g.updateFailedResult(jobID, result, fmt.Sprintf("baseline coverage: %v", err))
		return result, fmt.Errorf("baseline coverage: %w", err)
	}

	result.BaselineCoverage = baselineCoverage.TotalCoverage
	maxCoverageAchieved := baselineCoverage.TotalCoverage

	if relay != nil {
		relay.EmitEvent(RelayEvent{
			Type:  "coverage_baseline",
			Total: baselineCoverage.TotalCoverage,
			ByFile: buildCoverageByFileRecord(baselineCoverage),
		})
	}

	config := req.Config
	if config == nil {
		config = &TestGenConfig{
			TargetCoverage: DefaultTargetCoverage,
			MaxIterations:  DefaultMaxIterations,
		}
	}
	if config.TargetCoverage == 0 {
		config.TargetCoverage = DefaultTargetCoverage
	}
	if config.MaxIterations == 0 {
		config.MaxIterations = DefaultMaxIterations
	}

	iterations := []TestGenIteration{}
	totalInputTokens := 0
	totalOutputTokens := 0

	systemPrompt := BuildTestGenSystemPrompt(framework.Framework)

	for i := 1; i <= config.MaxIterations; i++ {
		if relay != nil {
			relay.EmitEvent(RelayEvent{Type: "phase", Phase: "iteration_start", Iteration: i, Message: fmt.Sprintf("Starting iteration %d", i)})
		}

		currentCoverage := baselineCoverage
		if len(iterations) > 0 {
			currentCoverage = iterations[len(iterations)-1].CoverageReport
		}

		topGaps := ExtractTopGapFiles(currentCoverage, MaxCoverageFiles)
		gapDetails := make([]*FileCoverageDetail, 0, len(topGaps))
		for _, gap := range topGaps {
			if detail, exists := currentCoverage.FileCoverage[gap.Path]; exists {
				gapDetails = append(gapDetails, detail)
			}
		}

		uncoveredFuncs := []UncoveredFunction{}
		for _, gap := range topGaps {
			filePath := filepath.Join(repoDir, gap.Path)
			stat, err := os.Stat(filePath)
			if err != nil || stat.Size() > maxSourceFileSize {
				continue
			}
			content, err := os.ReadFile(filePath)
			if err != nil {
				continue
			}
			funcs := ExtractFunctionBodies(gap.Path, string(content), gap.UncoveredRanges)
			uncoveredFuncs = append(uncoveredFuncs, funcs...)
			if len(uncoveredFuncs) >= MaxUncoveredFunctions {
				break
			}
		}
		if len(uncoveredFuncs) > MaxUncoveredFunctions {
			uncoveredFuncs = uncoveredFuncs[:MaxUncoveredFunctions]
		}

		if relay != nil {
			for _, gap := range topGaps {
				funcNames := []string{}
				for _, f := range uncoveredFuncs {
					if f.FilePath == gap.Path {
						funcNames = append(funcNames, f.FunctionName)
					}
				}
				if len(funcNames) > 0 {
					relay.EmitEvent(RelayEvent{Type: "gap_identified", File: gap.Path, UncoveredFunctions: funcNames})
				}
			}
		}

		var existingTests []TestExample
		if i == 1 {
			existingTests, _ = collectExistingTests(repoDir, framework.Framework, MaxTestExamples)
		} else {
			existingTests = []TestExample{}
			pattern := GetTestFilePattern(framework.Framework)
			filepath.Walk(repoDir, func(path string, info os.FileInfo, err error) error {
				if err != nil || info.IsDir() {
					return nil
				}
				if pattern.MatchString(filepath.Base(path)) {
					relPath, _ := filepath.Rel(repoDir, path)
					existingTests = append(existingTests, TestExample{FilePath: relPath})
					if len(existingTests) >= MaxTestExamples {
						return filepath.SkipDir
					}
				}
				return nil
			})
		}

		fileTree, _ := collectFileTree(repoDir, MaxFileTreeLines)

		failedTestContext := ""
		if i > 1 && !baselineTestResult.Passed {
			failedTestContext = baselineTestResult.Output
		}

		promptCtx := PromptContext{
			Task:                    req.Task,
			Repo:                    req.Repo,
			FileTree:                fileTree,
			Framework:               framework.Framework,
			CoverageGaps:            gapDetails,
			UncoveredFunctionBodies: uncoveredFuncs,
			ExistingTestExamples:    existingTests,
			FailedTestContext:       failedTestContext,
			Iteration:               i,
		}

		userPrompt := BuildTestGenUserPrompt(promptCtx)

		if relay != nil {
			relay.EmitEvent(RelayEvent{Type: "phase", Phase: "llm_call", Iteration: i, Message: "Calling LLM to generate tests"})
		}

		responseText, inputTokens, outputTokens, err := callLLMWithRetry(jobCtx, llmClient, userPrompt, systemPrompt, MaxOutputTokens)
		if err != nil {
			g.emitError(relay, fmt.Sprintf("LLM call failed after retries (iteration %d). Check API key and provider status.", i), false)
			g.updateFailedResult(jobID, result, fmt.Sprintf("LLM call failed (iteration %d): %v", i, err))
			return result, fmt.Errorf("LLM call failed: %w", err)
		}

		totalInputTokens += inputTokens
		totalOutputTokens += outputTokens

		generatedTests, err := parseTestGenResponse(responseText, framework.Framework)
		if err != nil {
			g.emitError(relay, fmt.Sprintf("LLM response could not be parsed (iteration %d). Retrying with additional context.", i), true)
			g.updateFailedResult(jobID, result, fmt.Sprintf("parse LLM response (iteration %d): %v", i, err))
			return result, fmt.Errorf("parse LLM response: %w", err)
		}

		if relay != nil {
			relay.EmitEvent(RelayEvent{Type: "phase", Phase: "writing_tests", Iteration: i, Message: fmt.Sprintf("Writing %d test files", len(generatedTests))})
		}

		writtenFiles := []string{}
		for _, test := range generatedTests {
			if !isPathSafe(repoDir, test.FilePath) {
				continue
			}
			targetPath := filepath.Join(repoDir, test.FilePath)
			if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
				continue
			}
			if err := os.WriteFile(targetPath, []byte(test.Content), 0644); err != nil {
				continue
			}
			writtenFiles = append(writtenFiles, test.FilePath)
			result.TestsCreated = append(result.TestsCreated, test.FilePath)
			if relay != nil {
				relay.EmitEvent(RelayEvent{Type: "test_generated", Path: test.FilePath, TargetFile: test.FilePath})
			}
		}

		if relay != nil {
			relay.EmitEvent(RelayEvent{Type: "phase", Phase: "running_tests", Iteration: i, Message: "Running tests with coverage"})
		}

		newCoverage, newTestResult, err := RunWithCoverage(jobCtx, repoDir, framework, jobID, i, relay)
		if err != nil {
			for _, f := range writtenFiles {
				os.Remove(filepath.Join(repoDir, f))
			}
			exec.CommandContext(jobCtx, "git", "-C", repoDir, "checkout", "--", ".").Run()
			result.TestsCreated = result.TestsCreated[:len(result.TestsCreated)-len(writtenFiles)]
			if relay != nil {
				relay.EmitEvent(RelayEvent{Type: "rollback", Iteration: i, Message: "Test run failed, rolling back iteration"})
			}
			continue
		}

		delta := newCoverage.TotalCoverage - currentCoverage.TotalCoverage

		if newCoverage.TotalCoverage < maxCoverageAchieved-0.1 {
			for _, f := range writtenFiles {
				os.Remove(filepath.Join(repoDir, f))
			}
			exec.CommandContext(jobCtx, "git", "-C", repoDir, "checkout", "--", ".").Run()
			result.TestsCreated = result.TestsCreated[:len(result.TestsCreated)-len(writtenFiles)]
			if relay != nil {
				relay.EmitEvent(RelayEvent{Type: "rollback", Iteration: i, Message: "Coverage decreased, rolling back iteration", Delta: delta})
			}
			break
		}

		iteration := TestGenIteration{
			Iteration:      i,
			TestsGenerated: writtenFiles,
			TestsPassed:    newTestResult.Passed,
			CoverageReport: newCoverage,
			CoverageDelta:  delta,
		}
		iterations = append(iterations, iteration)

		if err := g.store.CreateIteration(jobID, i, writtenFiles, newCoverage.TotalCoverage, delta, inputTokens, outputTokens); err != nil {
			fmt.Fprintf(os.Stderr, "create iteration record: %v\n", err)
		}

		if relay != nil {
			relay.EmitEvent(RelayEvent{
				Type:      "coverage_measured",
				Iteration: i,
				Coverage:  newCoverage.TotalCoverage,
				Delta:     delta,
				ByFile:    buildCoverageByFileRecord(newCoverage),
			})
			relay.EmitEvent(RelayEvent{
				Type:       "iteration_complete",
				Iteration:  i,
				Coverage:   newCoverage.TotalCoverage,
				Delta:      delta,
				Target:     config.TargetCoverage,
				Continuing: newCoverage.TotalCoverage < config.TargetCoverage,
			})
		}

		if newCoverage.TotalCoverage > maxCoverageAchieved {
			maxCoverageAchieved = newCoverage.TotalCoverage
		}

		baselineCoverage = newCoverage
		baselineTestResult = newTestResult

		shouldStop, reason := checkConvergence(iterations, config.TargetCoverage, maxCoverageAchieved)
		if shouldStop {
			if relay != nil {
				relay.EmitEvent(RelayEvent{Type: "converged", Message: fmt.Sprintf("Converged: %s", reason)})
			}
			break
		}

		if i == config.MaxIterations {
			if relay != nil {
				relay.EmitEvent(RelayEvent{Type: "converged", Message: "Maximum iterations reached"})
			}
		}
	}

	result.Iterations = len(iterations)
	result.FinalCoverage = maxCoverageAchieved
	result.InputTokens = totalInputTokens
	result.OutputTokens = totalOutputTokens

	branchName := fmt.Sprintf("shipmates/test-gen/%s", jobID)
	result.BranchName = branchName

	if len(result.TestsCreated) > 0 {
		if relay != nil {
			relay.EmitEvent(RelayEvent{Type: "phase", Phase: "git_operations", Message: "Creating branch and PR"})
		}

		if err := g.createBranchAndPR(jobCtx, repoDir, req.GithubToken, req.Repo, branchName, jobID, result.BaselineCoverage, result.FinalCoverage); err == nil {
			ghClient := gh.NewClient(req.GithubToken)
			prReq := gh.CreatePRRequest{
				Title: fmt.Sprintf("Generated tests: %.1f%% -> %.1f%% coverage", result.BaselineCoverage, result.FinalCoverage),
				Body:  fmt.Sprintf("Automated test generation\n\nCoverage: %.1f%% -> %.1f%%\nIterations: %d\nFiles created: %d", result.BaselineCoverage, result.FinalCoverage, result.Iterations, len(result.TestsCreated)),
				Head:  branchName,
				Base:  "main",
			}
			prResp, prErr := ghClient.CreatePullRequest(jobCtx, req.Repo, prReq)
			if prErr == nil {
				result.PRURL = prResp.HTMLURL
				if relay != nil {
					relay.EmitEvent(RelayEvent{Type: "pr_created", Message: result.PRURL, Path: result.BranchName, Coverage: result.FinalCoverage, Target: config.TargetCoverage})
				}
			}
		}
	}

	if err := g.store.UpdateTestGenResult(jobID, result); err != nil {
		fmt.Fprintf(os.Stderr, "update test gen result: %v\n", err)
	}

	return result, nil
}

func (g *Generator) emitError(relay *RelayClient, message string, recoverable bool) {
	if relay != nil {
		relay.EmitEvent(RelayEvent{Type: "error", Message: message, Recoverable: recoverable})
	}
}

func (g *Generator) cloneRepo(ctx context.Context, repo string, token string, jobID string, relay *RelayClient) (string, error) {
	parts := strings.Split(repo, "/")
	if len(parts) < 2 {
		return "", fmt.Errorf("invalid repo format: %s", repo)
	}
	repoName := parts[len(parts)-1]

	mutexKey := repo
	mutexVal, _ := cloneMutexes.LoadOrStore(mutexKey, &sync.Mutex{})
	mutex := mutexVal.(*sync.Mutex)

	cacheDir := filepath.Join(os.TempDir(), "test-gen-cache", strings.ReplaceAll(repo, "/", "-"))
	workDir := filepath.Join(os.TempDir(), fmt.Sprintf("test-gen-%s-%s", jobID, repoName))

	cloneCtx, cancel := context.WithTimeout(ctx, CloneTimeoutSeconds*time.Second)
	defer cancel()

	repoURL := fmt.Sprintf("https://x-access-token:%s@github.com/%s.git", token, repo)

	mutex.Lock()

	if _, err := os.Stat(cacheDir); err == nil {
		if relay != nil {
			relay.EmitEvent(RelayEvent{Type: "phase", Phase: "clone", Message: "Updating cached repository"})
		}
		fetchCmd := exec.CommandContext(cloneCtx, "git", "-C", cacheDir, "fetch", "origin")
		fetchCmd.Run()
		resetCmd := exec.CommandContext(cloneCtx, "git", "-C", cacheDir, "reset", "--hard", "origin/HEAD")
		resetCmd.Run()
	} else {
		if relay != nil {
			relay.EmitEvent(RelayEvent{Type: "phase", Phase: "clone", Message: "Cloning repository"})
		}
		os.MkdirAll(filepath.Dir(cacheDir), 0755)
		cloneCmd := exec.CommandContext(cloneCtx, "git", "clone", "--depth", "1", repoURL, cacheDir)
		if err := cloneCmd.Run(); err != nil {
			mutex.Unlock()
			return "", fmt.Errorf("git clone failed")
		}
	}

	cpCmd := exec.CommandContext(cloneCtx, "cp", "-r", cacheDir, workDir)
	cpErr := cpCmd.Run()
	mutex.Unlock()

	if cpErr != nil {
		return "", fmt.Errorf("copy repo: %w", cpErr)
	}

	return workDir, nil
}

func (g *Generator) createBranchAndPR(ctx context.Context, repoDir string, token string, repo string, branchName string, jobID string, baselineCov float64, finalCov float64) error {
	checkoutCmd := exec.CommandContext(ctx, "git", "-C", repoDir, "checkout", "-b", branchName)
	if err := checkoutCmd.Run(); err != nil {
		return fmt.Errorf("git checkout: %w", err)
	}

	addCmd := exec.CommandContext(ctx, "git", "-C", repoDir, "add", ".")
	if err := addCmd.Run(); err != nil {
		return fmt.Errorf("git add: %w", err)
	}

	commitMsg := fmt.Sprintf("Add generated tests (coverage: %.1f%% -> %.1f%%)", baselineCov, finalCov)
	commitCmd := exec.CommandContext(ctx, "git", "-C", repoDir, "commit", "-m", commitMsg)
	if err := commitCmd.Run(); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}

	repoURL := fmt.Sprintf("https://x-access-token:%s@github.com/%s.git", token, repo)
	pushCmd := exec.CommandContext(ctx, "git", "-C", repoDir, "push", repoURL, branchName)
	if err := pushCmd.Run(); err != nil {
		return fmt.Errorf("git push failed")
	}

	return nil
}

func (g *Generator) updateFailedResult(jobID string, result *TestGenResult, errMsg string) {
	g.store.UpdateTestGenStatus(jobID, "failed")
}

func isPathSafe(repoDir string, filePath string) bool {
	if strings.Contains(filePath, "..") || strings.ContainsRune(filePath, 0) {
		return false
	}
	absPath, err := filepath.Abs(filepath.Join(repoDir, filePath))
	if err != nil {
		return false
	}
	absRepo, err := filepath.Abs(repoDir)
	if err != nil {
		return false
	}
	if !strings.HasPrefix(absPath, absRepo+string(filepath.Separator)) {
		return false
	}
	resolved, err := filepath.EvalSymlinks(filepath.Dir(absPath))
	if err != nil {
		return true
	}
	resolvedRepo, err := filepath.EvalSymlinks(absRepo)
	if err != nil {
		return true
	}
	return strings.HasPrefix(resolved, resolvedRepo+string(filepath.Separator)) || resolved == resolvedRepo
}

func sanitizeTokenFromError(err error) string {
	return tokenSanitizePattern.ReplaceAllString(err.Error(), "https://***@")
}

func checkConvergence(iterations []TestGenIteration, targetCoverage float64, maxCoverageAchieved float64) (bool, string) {
	if len(iterations) == 0 {
		return false, ""
	}

	if maxCoverageAchieved >= targetCoverage {
		return true, "target_met"
	}

	if len(iterations) >= ConvergenceWindow {
		recentIterations := iterations[len(iterations)-ConvergenceWindow:]
		allBelowThreshold := true
		for _, iter := range recentIterations {
			if iter.CoverageDelta >= ConvergenceThreshold {
				allBelowThreshold = false
				break
			}
		}
		if allBelowThreshold {
			return true, "stalled"
		}
	}

	return false, ""
}

func collectExistingTests(repoDir string, framework TestFramework, maxExamples int) ([]TestExample, error) {
	pattern := GetTestFilePattern(framework)
	examples := []TestExample{}

	err := filepath.Walk(repoDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if info.Name() == ".git" || info.Name() == "node_modules" || info.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}

		if pattern.MatchString(filepath.Base(path)) {
			content, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			relPath, _ := filepath.Rel(repoDir, path)
			examples = append(examples, TestExample{
				FilePath: relPath,
				Content:  string(content),
			})
			if len(examples) >= maxExamples {
				return filepath.SkipDir
			}
		}
		return nil
	})

	return examples, err
}

func collectFileTree(repoDir string, maxLines int) (string, error) {
	files := []string{}

	err := filepath.Walk(repoDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if info.Name() == ".git" || info.Name() == "node_modules" || info.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}

		relPath, _ := filepath.Rel(repoDir, path)
		files = append(files, relPath)
		if len(files) >= maxLines {
			return errFileTreeLimitReached
		}
		return nil
	})

	if err != nil && err != errFileTreeLimitReached {
		return "", err
	}

	return strings.Join(files, "\n"), nil
}

func callLLMWithRetry(ctx context.Context, client llm.Client, userPrompt string, systemPrompt string, maxTokens int) (string, int, int, error) {
	messages := []llm.ChatMessage{
		{Role: "user", Content: userPrompt},
	}

	var lastErr error
	for _, delay := range LLMRetryDelays {
		timeoutCtx, timeoutCancel := context.WithTimeout(ctx, LLMTimeoutSeconds*time.Second)

		result, err := client.Chat(timeoutCtx, messages, systemPrompt, maxTokens)
		timeoutCancel()
		if err == nil {
			return result.Text, result.InputTokens, result.OutputTokens, nil
		}

		lastErr = err

		if !isRetryableError(err) {
			return "", 0, 0, lastErr
		}

		jitter := time.Duration(float64(delay) * LLMJitterFactor * rand.Float64())
		time.Sleep(time.Duration(delay)*time.Millisecond + jitter)
	}

	return "", 0, 0, fmt.Errorf("LLM call failed after retries: %w", lastErr)
}

func isRetryableError(err error) bool {
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "429") ||
		strings.Contains(errStr, "500") ||
		strings.Contains(errStr, "503") ||
		strings.Contains(errStr, "rate limit")
}

func parseTestGenResponse(raw string, framework TestFramework) ([]GeneratedTest, error) {
	cleaned := stripMarkdownFences(raw)
	cleaned = stripTrailingCommas(cleaned)

	jsonStr, err := extractJSONSubstring(cleaned)
	if err != nil {
		jsonStr = cleaned
	}

	var parsedTests []struct {
		FilePath string `json:"filePath"`
		Content  string `json:"content"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &parsedTests); err != nil {
		return nil, fmt.Errorf("parse JSON: %w", err)
	}

	if len(parsedTests) > MaxGeneratedFilesPerIter {
		return nil, fmt.Errorf("too many files generated: %d > %d", len(parsedTests), MaxGeneratedFilesPerIter)
	}

	tests := make([]GeneratedTest, 0, len(parsedTests))
	for _, pt := range parsedTests {
		if pt.FilePath == "" || pt.Content == "" {
			continue
		}
		tests = append(tests, GeneratedTest{
			FilePath:  pt.FilePath,
			Content:   pt.Content,
			Framework: framework,
		})
	}

	return tests, nil
}

func stripMarkdownFences(text string) string {
	matches := fencePattern.FindStringSubmatch(text)
	if len(matches) > 1 {
		return matches[1]
	}
	return text
}

func extractJSONSubstring(text string) (string, error) {
	start := strings.Index(text, "[")
	if start == -1 {
		return "", fmt.Errorf("no JSON array found")
	}

	depth := 0
	for i := start; i < len(text); i++ {
		if text[i] == '[' {
			depth++
		} else if text[i] == ']' {
			depth--
			if depth == 0 {
				return text[start : i+1], nil
			}
		}
	}

	return "", fmt.Errorf("unclosed JSON array")
}

func stripTrailingCommas(text string) string {
	return commaBeforeCloseBracket.ReplaceAllString(text, "$1")
}

func buildCoverageByFileRecord(cov *CoverageReport) map[string]float64 {
	result := make(map[string]float64)
	for path, detail := range cov.FileCoverage {
		result[path] = detail.CoveragePercent
	}
	return result
}
