package goshared

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

type SourceFile struct {
	Path    string
	Content string
}

func CollectFiles(dir string, prefix string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read dir %s: %w", dir, err)
	}

	var result []string
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}

		relPath := entry.Name()
		if prefix != "" {
			relPath = prefix + "/" + entry.Name()
		}

		if entry.IsDir() {
			if !SkipDirs[entry.Name()] {
				subFiles, err := CollectFiles(filepath.Join(dir, entry.Name()), relPath)
				if err != nil {
					return nil, err
				}
				result = append(result, subFiles...)
			}
		} else {
			result = append(result, relPath)
		}
	}

	return result, nil
}

func FilterByScope(files []string, scope *AgentScope) []string {
	if scope == nil {
		return files
	}

	filtered := files

	if len(scope.Directories) > 0 {
		var matched []string
		for _, f := range filtered {
			for _, dir := range scope.Directories {
				if strings.HasPrefix(f, dir) {
					matched = append(matched, f)
					break
				}
			}
		}
		filtered = matched
	}

	if len(scope.Languages) > 0 {
		allowedExts := make(map[string]bool)
		for _, lang := range scope.Languages {
			exts, ok := LanguageExtensions[strings.ToLower(lang)]
			if ok {
				for _, ext := range exts {
					allowedExts[ext] = true
				}
			}
		}

		if len(allowedExts) > 0 {
			var matched []string
			for _, f := range filtered {
				ext := filepath.Ext(f)
				if allowedExts[ext] {
					matched = append(matched, f)
				}
			}
			filtered = matched
		}
	}

	return filtered
}

func IsTestFile(filePath string) bool {
	for _, pattern := range TestFilePatterns {
		if pattern.MatchString(filePath) {
			return true
		}
	}
	return false
}

func IsSourceFile(filePath string) bool {
	parts := strings.Split(filePath, "/")
	if len(parts) > 0 {
		if SkipFiles[parts[len(parts)-1]] {
			return false
		}
	}

	ext := filepath.Ext(filePath)
	return SourceExtensions[ext]
}

func ReadSourceFiles(repoDir string, allFiles []string, task string, maxBytes int, scope *AgentScope) ([]SourceFile, error) {
	var sourceFiles []string
	for _, f := range allFiles {
		if IsSourceFile(f) {
			sourceFiles = append(sourceFiles, f)
		}
	}

	taskLower := strings.ToLower(task)
	taskWords := strings.Fields(taskLower)
	var filteredWords []string
	for _, w := range taskWords {
		if len(w) > 3 {
			filteredWords = append(filteredWords, w)
		}
	}

	type scoredFile struct {
		file  string
		score int
	}

	scored := make([]scoredFile, 0, len(sourceFiles))
	for _, f := range sourceFiles {
		score := 0
		fLower := strings.ToLower(f)

		for _, word := range filteredWords {
			if strings.Contains(fLower, word) {
				score += 10
			}
		}

		if scope != nil && len(scope.Directories) > 0 {
			for _, dir := range scope.Directories {
				if strings.HasPrefix(f, dir) {
					score += 5
					break
				}
			}
		}

		depth := len(strings.Split(f, "/"))
		score -= depth

		scored = append(scored, scoredFile{file: f, score: score})
	}

	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	const maxPerFile = 50 * 1024
	const maxConcurrentReads = 16

	var mu sync.Mutex
	var result []SourceFile
	var totalBytes atomic.Int64

	sem := make(chan struct{}, maxConcurrentReads)
	var wg sync.WaitGroup

	absRepoDir, err := filepath.Abs(repoDir)
	if err != nil {
		return nil, fmt.Errorf("resolve repo dir: %w", err)
	}

	for _, sf := range scored {
		if totalBytes.Load() >= int64(maxBytes) {
			break
		}

		wg.Add(1)
		go func(file string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			fullPath := filepath.Join(absRepoDir, file)

			resolvedPath, err := filepath.EvalSymlinks(fullPath)
			if err != nil {
				return
			}

			cleanResolved, err := filepath.Abs(resolvedPath)
			if err != nil {
				return
			}

			if !strings.HasPrefix(cleanResolved, absRepoDir+string(os.PathSeparator)) && cleanResolved != absRepoDir {
				return
			}

			raw, err := os.ReadFile(fullPath)
			if err != nil || len(raw) == 0 {
				return
			}

			content := string(raw)
			if len(content) > maxPerFile {
				content = content[:maxPerFile]
			}

			mu.Lock()
			defer mu.Unlock()

			newTotal := totalBytes.Load() + int64(len(content))
			if newTotal <= int64(maxBytes) {
				result = append(result, SourceFile{
					Path:    file,
					Content: content,
				})
				totalBytes.Store(newTotal)
			}
		}(sf.file)
	}

	wg.Wait()

	return result, nil
}
