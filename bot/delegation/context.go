package delegation

import (
	"container/list"
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	goshared "github.com/KranzL/shipmates-oss/go-shared"
	"github.com/KranzL/shipmates-oss/go-llm"
	"github.com/KranzL/shipmates-oss/bot/orchestrator"
	"golang.org/x/time/rate"
)

const (
	maxSessionOutputChunks = 50
	maxSummaryTokens       = 500
	maxFullOutputLength    = 100000
	maxFilesToExtract      = 50
	summaryCacheTTL        = 5 * time.Minute
	maxSummaryCacheSize    = 500
)

var (
	filePatternRegex = regexp.MustCompile(`(?:^|\s)([\w./-]+\.\w{1,10})(?:\s|$|:|,)`)
	knownExtensions  = map[string]bool{
		"ts": true, "tsx": true, "js": true, "jsx": true, "py": true, "go": true,
		"rs": true, "java": true, "kt": true, "rb": true, "php": true, "cs": true,
		"cpp": true, "c": true, "h": true, "hpp": true, "css": true, "scss": true,
		"html": true, "json": true, "yaml": true, "yml": true, "toml": true,
		"md": true, "sql": true, "prisma": true,
	}
)

var (
	summaryCacheMu  sync.RWMutex
	summaryCache    = make(map[string]*summaryCacheEntry)
	summaryCacheLRU = list.New()
	summaryRateLimiter = rate.NewLimiter(rate.Every(200*time.Millisecond), 5)
)

type summaryCacheEntry struct {
	summary   string
	timestamp time.Time
	element   *list.Element
}

type ContextBuildOptions struct {
	FromSessionID string
	ToAgentID     string
	Description   string
	Mode          goshared.DelegationContextMode
}

func BuildDelegationContext(ctx context.Context, db *goshared.DB, llmClient llm.Client, opts ContextBuildOptions) (*goshared.DelegationContext, error) {
	if opts.FromSessionID == "" {
		return &goshared.DelegationContext{
			Summary:       opts.Description,
			RelevantFiles: []string{},
			Repo:          nil,
		}, nil
	}

	fromSession, err := db.GetSessionByID(ctx, opts.FromSessionID)
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}
	if fromSession == nil {
		return &goshared.DelegationContext{
			Summary:       opts.Description,
			RelevantFiles: []string{},
			Repo:          nil,
		}, nil
	}

	toAgent, err := db.GetAgent(ctx, opts.ToAgentID, fromSession.UserID)
	if err != nil {
		return nil, fmt.Errorf("get agent: %w", err)
	}
	if toAgent == nil {
		return &goshared.DelegationContext{
			Summary:       opts.Description,
			RelevantFiles: []string{},
			Repo:          nil,
		}, nil
	}

	sessionOutputs, err := db.GetSessionOutputs(ctx, opts.FromSessionID, 0, maxSessionOutputChunks)
	if err != nil {
		return nil, fmt.Errorf("get session outputs: %w", err)
	}

	totalSize := 0
	for _, output := range sessionOutputs {
		totalSize += len(output.Content)
	}
	var fullOutput strings.Builder
	fullOutput.Grow(totalSize)
	for _, output := range sessionOutputs {
		fullOutput.WriteString(output.Content)
	}

	outputStr := fullOutput.String()
	repo := fromSession.Repo

	switch opts.Mode {
	case goshared.DelegationContextModeSummary:
		return buildSummaryContext(ctx, db, llmClient, opts, outputStr, repo, fromSession.UserID)
	case goshared.DelegationContextModeDiff:
		return buildDiffContext(fromSession, repo)
	default:
		return buildFullContext(outputStr, repo)
	}
}

func buildSummaryContext(ctx context.Context, db *goshared.DB, llmClient llm.Client, opts ContextBuildOptions, fullOutput string, repo *string, userID string) (*goshared.DelegationContext, error) {
	toAgent, err := db.GetAgent(ctx, opts.ToAgentID, userID)
	if err != nil || toAgent == nil || toAgent.APIKeyID == nil {
		boundedOutput := fullOutput
		if len(boundedOutput) > 5000 {
			boundedOutput = boundedOutput[:5000]
		}
		return &goshared.DelegationContext{
			Summary:       boundedOutput,
			RelevantFiles: parseFilesFromOutput(boundedOutput),
			Repo:          repo,
		}, nil
	}

	apiKey, err := db.GetAPIKey(ctx, *toAgent.APIKeyID, userID)
	if err != nil || apiKey == nil {
		boundedOutput := fullOutput
		if len(boundedOutput) > 5000 {
			boundedOutput = boundedOutput[:5000]
		}
		return &goshared.DelegationContext{
			Summary:       boundedOutput,
			RelevantFiles: parseFilesFromOutput(boundedOutput),
			Repo:          repo,
		}, nil
	}

	cacheKey := fmt.Sprintf("%s-%s-%s", userID, opts.FromSessionID, opts.Mode)
	summaryCacheMu.Lock()
	cached, found := summaryCache[cacheKey]
	if found && time.Since(cached.timestamp) < summaryCacheTTL {
		summaryCacheLRU.MoveToFront(cached.element)
		summary := cached.summary
		summaryCacheMu.Unlock()
		return &goshared.DelegationContext{
			Summary:       summary,
			RelevantFiles: parseFilesFromOutput(fullOutput),
			Repo:          repo,
		}, nil
	}
	summaryCacheMu.Unlock()

	if llmClient == nil {
		boundedOutput := fullOutput
		if len(boundedOutput) > 5000 {
			boundedOutput = boundedOutput[:5000]
		}
		return &goshared.DelegationContext{
			Summary:       boundedOutput,
			RelevantFiles: parseFilesFromOutput(boundedOutput),
			Repo:          repo,
		}, nil
	}

	sanitizedOutput := orchestrator.SanitizeOutput(fullOutput)
	truncatedOutput := sanitizedOutput
	if len(truncatedOutput) > 20000 {
		truncatedOutput = truncatedOutput[:20000]
	}

	if err := summaryRateLimiter.Wait(ctx); err != nil {
		boundedOutput := fullOutput
		if len(boundedOutput) > 5000 {
			boundedOutput = boundedOutput[:5000]
		}
		return &goshared.DelegationContext{
			Summary:       boundedOutput,
			RelevantFiles: parseFilesFromOutput(boundedOutput),
			Repo:          repo,
		}, nil
	}

	summaryResult, err := llmClient.Chat(ctx, []llm.ChatMessage{
		{
			Role:    "user",
			Content: fmt.Sprintf("Summarize this completed work for the next agent. Focus on what was done, what files were changed, and the current state:\n\n%s", truncatedOutput),
		},
	}, "You are summarizing completed work for another coding agent. Be concise and focus on actionable information.", maxSummaryTokens)

	if err != nil {
		boundedOutput := fullOutput
		if len(boundedOutput) > 5000 {
			boundedOutput = boundedOutput[:5000]
		}
		return &goshared.DelegationContext{
			Summary:       boundedOutput,
			RelevantFiles: parseFilesFromOutput(boundedOutput),
			Repo:          repo,
		}, nil
	}

	summaryCacheMu.Lock()
	entry := &summaryCacheEntry{
		summary:   summaryResult.Text,
		timestamp: time.Now(),
	}
	entry.element = summaryCacheLRU.PushFront(cacheKey)
	summaryCache[cacheKey] = entry

	if len(summaryCache) > maxSummaryCacheSize {
		oldest := summaryCacheLRU.Back()
		if oldest != nil {
			oldestKey := oldest.Value.(string)
			summaryCacheLRU.Remove(oldest)
			delete(summaryCache, oldestKey)
		}
	}
	summaryCacheMu.Unlock()

	return &goshared.DelegationContext{
		Summary:       summaryResult.Text,
		RelevantFiles: parseFilesFromOutput(fullOutput),
		Repo:          repo,
	}, nil
}

func buildDiffContext(fromSession *goshared.Session, repo *string) (*goshared.DelegationContext, error) {
	branchName := "main"
	if fromSession.BranchName != nil {
		branchName = *fromSession.BranchName
	}

	summary := fmt.Sprintf("Continuing work on branch %s", branchName)
	return &goshared.DelegationContext{
		Summary:       summary,
		RelevantFiles: []string{},
		GitDiff:       nil,
		Repo:          repo,
	}, nil
}

func buildFullContext(fullOutput string, repo *string) (*goshared.DelegationContext, error) {
	sanitizedOutput := orchestrator.SanitizeOutput(fullOutput)
	boundedOutput := sanitizedOutput
	if len(boundedOutput) > maxFullOutputLength {
		boundedOutput = boundedOutput[:maxFullOutputLength]
	}

	summary := boundedOutput
	if len(summary) > 5000 {
		summary = summary[:5000]
	}

	parentOutput := boundedOutput
	return &goshared.DelegationContext{
		Summary:             summary,
		RelevantFiles:       parseFilesFromOutput(boundedOutput),
		ParentSessionOutput: &parentOutput,
		Repo:                repo,
	}, nil
}

func parseFilesFromOutput(output string) []string {
	filesMap := make(map[string]bool, maxFilesToExtract)
	matches := filePatternRegex.FindAllStringSubmatch(output, -1)

	for _, match := range matches {
		if len(filesMap) >= maxFilesToExtract {
			break
		}
		if len(match) > 1 {
			candidate := match[1]
			parts := strings.Split(candidate, ".")
			if len(parts) < 2 {
				continue
			}
			extension := strings.ToLower(parts[len(parts)-1])
			if knownExtensions[extension] && strings.Contains(candidate, "/") {
				filesMap[candidate] = true
			}
		}
	}

	files := make([]string, 0, len(filesMap))
	for file := range filesMap {
		files = append(files, file)
	}

	return files
}
