package engine

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	cloneTimeoutSec = 60
	gitTimeoutSec   = 30
	maxCachedRepos  = 5
	maxPushRetries  = 4
)

type CloneOptions struct {
	Repo        string
	GithubToken string
	TmpDir      string
	Sparse      bool
}

type CommitOptions struct {
	RepoDir    string
	BranchName string
	Message    string
	Files      []string
}

type CommitResult struct {
	Committed  bool
	Pushed     bool
	BranchName string
	Error      string
}

type FileDiffStat struct {
	Path      string
	Additions int
	Deletions int
}

type cloneCacheEntry struct {
	path     string
	lastUsed time.Time
}

var (
	cloneCacheMu   sync.Mutex
	cloneCache     = make(map[string]*cloneCacheEntry)
	inflightClones = make(map[string]chan struct{})

	validRepoPattern   = regexp.MustCompile(`^[a-zA-Z0-9_.-]+/[a-zA-Z0-9_.-]+$`)
	validTokenPattern  = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
	validBranchPattern = regexp.MustCompile(`^[a-zA-Z0-9._/-]+$`)
)

var cacheDirPath = filepath.Join(os.TempDir(), "shipmates-repo-cache")

func validateBranchName(branchName string) error {
	if len(branchName) > 200 {
		return fmt.Errorf("branch name too long: %d characters", len(branchName))
	}
	if strings.Contains(branchName, "..") {
		return fmt.Errorf("branch name contains ..: %s", branchName)
	}
	if !validBranchPattern.MatchString(branchName) {
		return fmt.Errorf("invalid branch name: %s", branchName)
	}
	return nil
}

func evictLRU() error {
	if len(cloneCache) < maxCachedRepos {
		return nil
	}

	var oldestKey string
	var oldestTime time.Time
	first := true

	for key, entry := range cloneCache {
		if first || entry.lastUsed.Before(oldestTime) {
			oldestTime = entry.lastUsed
			oldestKey = key
			first = false
		}
	}

	if oldestKey != "" {
		entry := cloneCache[oldestKey]
		if err := os.RemoveAll(entry.path); err != nil {
			fmt.Fprintf(os.Stderr, "[engine-git] failed to delete cached repo: %v\n", err)
		}
		delete(cloneCache, oldestKey)
	}

	return nil
}

func updateCachedRepo(ctx context.Context, repoPath, githubToken, tmpDir string) error {
	netrcPath := filepath.Join(tmpDir, ".netrc")
	netrcContent := fmt.Sprintf("machine github.com\nlogin x-access-token\npassword %s\n", githubToken)
	if err := os.WriteFile(netrcPath, []byte(netrcContent), 0600); err != nil {
		return err
	}
	defer os.Remove(netrcPath)

	gitConfig := filepath.Join(tmpDir, ".gitconfig")
	if err := os.WriteFile(gitConfig, []byte(""), 0600); err != nil {
		return err
	}

	fetchCtx, cancel := context.WithTimeout(ctx, gitTimeoutSec*time.Second)
	defer cancel()

	cmd := exec.CommandContext(fetchCtx, "git", "fetch", "origin")
	cmd.Dir = repoPath
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"HOME="+tmpDir,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL="+gitConfig,
	)
	if err := cmd.Run(); err != nil {
		return err
	}

	resetCtx, cancel2 := context.WithTimeout(ctx, gitTimeoutSec*time.Second)
	defer cancel2()

	resetCmd := exec.CommandContext(resetCtx, "git", "reset", "--hard", "origin/HEAD")
	resetCmd.Dir = repoPath
	resetCmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"HOME="+tmpDir,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL="+gitConfig,
	)
	if err := resetCmd.Run(); err != nil {
		return err
	}

	return nil
}

func performClone(ctx context.Context, repo, githubToken, tmpDir string, sparse bool) (string, error) {
	netrcPath := filepath.Join(tmpDir, ".netrc")
	netrcContent := fmt.Sprintf("machine github.com\nlogin x-access-token\npassword %s\n", githubToken)
	if err := os.WriteFile(netrcPath, []byte(netrcContent), 0600); err != nil {
		return "", err
	}
	defer os.Remove(netrcPath)

	gitConfig := filepath.Join(tmpDir, ".gitconfig")
	if err := os.WriteFile(gitConfig, []byte(""), 0600); err != nil {
		return "", err
	}

	if err := os.MkdirAll(cacheDirPath, 0755); err != nil {
		return "", err
	}

	repoName := strings.ReplaceAll(repo, "/", "-")
	cachedRepoPath := filepath.Join(cacheDirPath, repoName)

	cloneCtx, cancel := context.WithTimeout(ctx, cloneTimeoutSec*time.Second)
	defer cancel()

	var args []string
	if sparse {
		args = []string{"clone", "--filter=blob:none", "--sparse", "--depth", "1", fmt.Sprintf("https://github.com/%s.git", repo), cachedRepoPath}
	} else {
		args = []string{"clone", "--depth", "1", fmt.Sprintf("https://github.com/%s.git", repo), cachedRepoPath}
	}

	cmd := exec.CommandContext(cloneCtx, "git", args...)
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"HOME="+tmpDir,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL="+gitConfig,
	)
	if err := cmd.Run(); err != nil {
		return "", err
	}

	if err := evictLRU(); err != nil {
		return "", err
	}

	cloneCache[repo] = &cloneCacheEntry{
		path:     cachedRepoPath,
		lastUsed: time.Now(),
	}

	return cachedRepoPath, nil
}

func CloneRepo(ctx context.Context, opts CloneOptions) (string, error) {
	if !validRepoPattern.MatchString(opts.Repo) {
		return "", fmt.Errorf("invalid repository format: %s", opts.Repo)
	}
	if !validTokenPattern.MatchString(opts.GithubToken) {
		return "", fmt.Errorf("invalid GitHub token format")
	}

	cloneCacheMu.Lock()
	existing, inflightExists := inflightClones[opts.Repo]
	if inflightExists {
		cloneCacheMu.Unlock()
		<-existing
		cloneCacheMu.Lock()
		defer cloneCacheMu.Unlock()
		if entry, ok := cloneCache[opts.Repo]; ok {
			entry.lastUsed = time.Now()
			return entry.path, nil
		}
		return "", fmt.Errorf("clone failed")
	}

	cachedEntry, cacheExists := cloneCache[opts.Repo]
	if cacheExists {
		cloneCacheMu.Unlock()
		if err := updateCachedRepo(ctx, cachedEntry.path, opts.GithubToken, opts.TmpDir); err == nil {
			cloneCacheMu.Lock()
			cachedEntry.lastUsed = time.Now()
			cloneCacheMu.Unlock()
			return cachedEntry.path, nil
		}
		cloneCacheMu.Lock()
		delete(cloneCache, opts.Repo)
		os.RemoveAll(cachedEntry.path)
	}

	ch := make(chan struct{})
	inflightClones[opts.Repo] = ch
	cloneCacheMu.Unlock()

	path, err := performClone(ctx, opts.Repo, opts.GithubToken, opts.TmpDir, opts.Sparse)

	cloneCacheMu.Lock()
	delete(inflightClones, opts.Repo)
	close(ch)
	cloneCacheMu.Unlock()

	return path, err
}

func CreateBranch(ctx context.Context, repoDir, branchName string) error {
	if err := validateBranchName(branchName); err != nil {
		return err
	}

	createCtx, cancel := context.WithTimeout(ctx, gitTimeoutSec*time.Second)
	defer cancel()

	cmd := exec.CommandContext(createCtx, "git", "checkout", "-b", branchName)
	cmd.Dir = repoDir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	return cmd.Run()
}

func CommitAndPush(ctx context.Context, opts CommitOptions) (*CommitResult, error) {
	if err := validateBranchName(opts.BranchName); err != nil {
		return &CommitResult{Committed: false, Pushed: false, Error: err.Error()}, nil
	}

	addCtx, cancel := context.WithTimeout(ctx, gitTimeoutSec*time.Second)
	defer cancel()

	var addCmd *exec.Cmd
	if len(opts.Files) > 0 {
		args := append([]string{"add", "--"}, opts.Files...)
		addCmd = exec.CommandContext(addCtx, "git", args...)
	} else {
		addCmd = exec.CommandContext(addCtx, "git", "add", "-A")
	}
	addCmd.Dir = opts.RepoDir
	addCmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if err := addCmd.Run(); err != nil {
		return &CommitResult{Committed: false, Pushed: false, Error: err.Error()}, nil
	}

	commitCtx, cancel2 := context.WithTimeout(ctx, gitTimeoutSec*time.Second)
	defer cancel2()

	commitCmd := exec.CommandContext(commitCtx, "git", "commit", "-m", opts.Message)
	commitCmd.Dir = opts.RepoDir
	commitCmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if err := commitCmd.Run(); err != nil {
		return &CommitResult{Committed: false, Pushed: false, Error: err.Error()}, nil
	}

	for attempt := 0; attempt <= maxPushRetries-1; attempt++ {
		targetBranch := opts.BranchName
		if attempt > 0 {
			targetBranch = fmt.Sprintf("%s-%d", opts.BranchName, attempt)
			checkoutCtx, cancel3 := context.WithTimeout(ctx, gitTimeoutSec*time.Second)
			checkoutCmd := exec.CommandContext(checkoutCtx, "git", "checkout", "-b", targetBranch)
			checkoutCmd.Dir = opts.RepoDir
			checkoutCmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
			if err := checkoutCmd.Run(); err != nil {
				cancel3()
				if attempt == maxPushRetries-1 {
					return &CommitResult{Committed: true, Pushed: false, BranchName: targetBranch, Error: err.Error()}, nil
				}
				continue
			}
			cancel3()
		}

		pushCtx, cancel4 := context.WithTimeout(ctx, gitTimeoutSec*time.Second)
		pushCmd := exec.CommandContext(pushCtx, "git", "push", "origin", targetBranch)
		pushCmd.Dir = opts.RepoDir
		pushCmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
		err := pushCmd.Run()
		cancel4()

		if err == nil {
			return &CommitResult{Committed: true, Pushed: true, BranchName: targetBranch}, nil
		}

		if attempt == maxPushRetries-1 {
			return &CommitResult{Committed: true, Pushed: false, BranchName: targetBranch, Error: err.Error()}, nil
		}
	}

	return &CommitResult{Committed: true, Pushed: false, BranchName: opts.BranchName}, nil
}

func CleanupCredentials(tmpDir string) error {
	netrcPath := filepath.Join(tmpDir, ".netrc")
	if err := os.Remove(netrcPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func GetChangedFiles(ctx context.Context, repoDir string) ([]string, error) {
	diffCtx, cancel := context.WithTimeout(ctx, gitTimeoutSec*time.Second)
	defer cancel()

	cmd := exec.CommandContext(diffCtx, "git", "diff", "--name-only", "HEAD")
	cmd.Dir = repoDir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")

	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	var files []string
	for _, line := range lines {
		if line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}

func GetFileDiffStats(ctx context.Context, repoDir string) ([]FileDiffStat, error) {
	diffCtx, cancel := context.WithTimeout(ctx, gitTimeoutSec*time.Second)
	defer cancel()

	cmd := exec.CommandContext(diffCtx, "git", "diff", "--numstat", "HEAD")
	cmd.Dir = repoDir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")

	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	var stats []FileDiffStat
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 3 {
			continue
		}
		additions := 0
		deletions := 0
		fmt.Sscanf(parts[0], "%d", &additions)
		fmt.Sscanf(parts[1], "%d", &deletions)
		stats = append(stats, FileDiffStat{
			Path:      parts[2],
			Additions: additions,
			Deletions: deletions,
		})
	}
	return stats, nil
}

func ClearCloneCache() {
	cloneCacheMu.Lock()
	defer cloneCacheMu.Unlock()

	for repo, entry := range cloneCache {
		os.RemoveAll(entry.path)
		delete(cloneCache, repo)
	}
	inflightClones = make(map[string]chan struct{})
}
