package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

type MatchResult struct {
	Start  int
	End    int
	Method string
}

type FileEdit struct {
	FilePath    string `json:"filePath"`
	SearchText  string `json:"searchText"`
	ReplaceText string `json:"replaceText"`
}

type NewFile struct {
	FilePath string `json:"filePath"`
	Content  string `json:"content"`
}

type ApplyResultEntry struct {
	FilePath           string
	Success            bool
	Error              string
	OriginalSearchText string
}

type ApplyResult struct {
	Applied      []ApplyResultEntry
	Created      []ApplyResultEntry
	TotalApplied int
	TotalFailed  int
}

var (
	safePathPattern = regexp.MustCompile(`^[a-zA-Z0-9_.\-/]+$`)
	levenshteinPool = sync.Pool{
		New: func() interface{} {
			return make([]int, 0, 1024)
		},
	}
)

func rejectUnsafePath(filePath string) error {
	if strings.Contains(filePath, "..") || strings.HasPrefix(filePath, "/") {
		return fmt.Errorf("unsafe file path rejected: %s", filePath)
	}
	if !safePathPattern.MatchString(filePath) {
		return fmt.Errorf("invalid characters in file path: %s", filePath)
	}
	return nil
}

func normalizeLines(text string) string {
	lines := strings.Split(text, "\n")
	var b strings.Builder
	b.Grow(len(text))
	for i, line := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(strings.TrimRight(line, " \t\r"))
	}
	return b.String()
}

func stripBlankEdges(text string) string {
	lines := strings.Split(text, "\n")
	start := 0
	end := len(lines) - 1
	for start <= end && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	for end >= start && strings.TrimSpace(lines[end]) == "" {
		end--
	}
	if start > end {
		return ""
	}
	return strings.Join(lines[start:end+1], "\n")
}

func LevenshteinRatio(a, b string, threshold float64) float64 {
	if a == b {
		return 1
	}
	firstLength := len(a)
	secondLength := len(b)
	if firstLength == 0 && secondLength == 0 {
		return 1
	}
	if firstLength == 0 || secondLength == 0 {
		return 0
	}

	maxLen := firstLength
	if secondLength > maxLen {
		maxLen = secondLength
	}
	earlyTerminationThreshold := maxLen * 15 / 100

	prevSlice := levenshteinPool.Get().([]int)
	currSlice := levenshteinPool.Get().([]int)
	defer func() {
		if cap(prevSlice) <= 2048 {
			levenshteinPool.Put(prevSlice[:0])
		}
		if cap(currSlice) <= 2048 {
			levenshteinPool.Put(currSlice[:0])
		}
	}()

	if cap(prevSlice) < secondLength+1 {
		prevSlice = make([]int, secondLength+1)
	} else {
		prevSlice = prevSlice[:secondLength+1]
	}
	if cap(currSlice) < secondLength+1 {
		currSlice = make([]int, secondLength+1)
	} else {
		currSlice = currSlice[:secondLength+1]
	}

	prev := prevSlice
	curr := currSlice

	for j := 0; j <= secondLength; j++ {
		prev[j] = j
	}

	for i := 1; i <= firstLength; i++ {
		curr[0] = i
		rowMin := i
		for j := 1; j <= secondLength; j++ {
			if a[i-1] == b[j-1] {
				curr[j] = prev[j-1]
			} else {
				minVal := prev[j-1]
				if prev[j] < minVal {
					minVal = prev[j]
				}
				if curr[j-1] < minVal {
					minVal = curr[j-1]
				}
				curr[j] = 1 + minVal
			}
			if curr[j] < rowMin {
				rowMin = curr[j]
			}
		}
		if rowMin > earlyTerminationThreshold {
			return 0
		}

		minPossibleDistance := 0
		if firstLength > secondLength {
			minPossibleDistance = firstLength - secondLength
		} else {
			minPossibleDistance = secondLength - firstLength
		}
		if i < firstLength {
			minPossibleDistance += firstLength - i
		}
		bestPossibleRatio := 1.0 - float64(minPossibleDistance)/float64(maxLen)
		if bestPossibleRatio < threshold {
			return 0
		}

		for j := 0; j <= secondLength; j++ {
			prev[j] = curr[j]
		}
	}

	distance := prev[secondLength]
	return 1.0 - float64(distance)/float64(maxLen)
}

func computeLineHash(line string) uint32 {
	var hash uint32 = 0
	for i := 0; i < len(line); i++ {
		hash = hash*31 + uint32(line[i])
	}
	return hash
}

func FindMatch(fileContent, searchText string) *MatchResult {
	exactIndex := strings.Index(fileContent, searchText)
	if exactIndex != -1 {
		secondIndex := strings.Index(fileContent[exactIndex+1:], searchText)
		if secondIndex == -1 {
			return &MatchResult{Start: exactIndex, End: exactIndex + len(searchText), Method: "exact"}
		}
	}

	normalizedFile := normalizeLines(fileContent)
	normalizedSearch := normalizeLines(searchText)

	normalizedIndex := strings.Index(normalizedFile, normalizedSearch)
	if normalizedIndex != -1 {
		secondNormalizedIndex := strings.Index(normalizedFile[normalizedIndex+1:], normalizedSearch)
		if secondNormalizedIndex == -1 {
			return &MatchResult{Start: normalizedIndex, End: normalizedIndex + len(normalizedSearch), Method: "normalized"}
		}
	}

	strippedSearch := stripBlankEdges(normalizedSearch)
	if strippedSearch != normalizedSearch {
		strippedIndex := strings.Index(normalizedFile, strippedSearch)
		if strippedIndex != -1 {
			secondStrippedIndex := strings.Index(normalizedFile[strippedIndex+1:], strippedSearch)
			if secondStrippedIndex == -1 {
				return &MatchResult{Start: strippedIndex, End: strippedIndex + len(strippedSearch), Method: "normalized"}
			}
		}
	}

	fileLines := strings.Split(normalizedFile, "\n")
	searchLines := strings.Split(strippedSearch, "\n")
	windowSize := len(searchLines)

	if windowSize == 0 || windowSize > len(fileLines) {
		return nil
	}

	if len(fileLines) > 20000 {
		return nil
	}

	const FUZZY_THRESHOLD = 0.85
	const LARGE_FILE_THRESHOLD = 500
	const MAX_CANDIDATES = 10

	type candidatePos struct {
		position int
		score    float64
	}

	var candidatePositions []candidatePos

	if len(fileLines) > LARGE_FILE_THRESHOLD {
		searchHashes := make(map[uint32]bool)
		for _, line := range searchLines {
			searchHashes[computeLineHash(line)] = true
		}

		fileLineHashes := make([]uint32, len(fileLines))
		for i, line := range fileLines {
			fileLineHashes[i] = computeLineHash(line)
		}

		for i := 0; i <= len(fileLines)-windowSize; i++ {
			overlap := 0
			for j := i; j < i+windowSize; j++ {
				if searchHashes[fileLineHashes[j]] {
					overlap++
				}
			}
			score := float64(overlap) / float64(len(searchHashes))
			if score >= 0.5 {
				candidatePositions = append(candidatePositions, candidatePos{position: i, score: score})
			}
		}

		sort.Slice(candidatePositions, func(i, j int) bool {
			return candidatePositions[i].score > candidatePositions[j].score
		})

		if len(candidatePositions) > MAX_CANDIDATES {
			candidatePositions = candidatePositions[:MAX_CANDIDATES]
		}
	} else {
		candidatePositions = make([]candidatePos, len(fileLines)-windowSize+1)
		for i := range candidatePositions {
			candidatePositions[i] = candidatePos{position: i, score: 1}
		}
	}

	var matchPosition *int
	matchCount := 0

	for _, candidate := range candidatePositions {
		position := candidate.position
		windowLines := fileLines[position : position+windowSize]
		totalSimilarity := 0.0
		for i := 0; i < windowSize; i++ {
			totalSimilarity += LevenshteinRatio(windowLines[i], searchLines[i], FUZZY_THRESHOLD)
		}
		averageSimilarity := totalSimilarity / float64(windowSize)

		if averageSimilarity >= FUZZY_THRESHOLD {
			matchCount++
			if matchCount > 1 {
				return nil
			}
			pos := position
			matchPosition = &pos
		}
	}

	if matchPosition == nil {
		return nil
	}

	startChar := 0
	for i := 0; i < *matchPosition; i++ {
		startChar += len(fileLines[i]) + 1
	}
	if *matchPosition == 0 {
		startChar = 0
	}

	matchedBlock := strings.Join(fileLines[*matchPosition:*matchPosition+windowSize], "\n")
	return &MatchResult{Start: startChar, End: startChar + len(matchedBlock), Method: "fuzzy"}
}

func ApplyEdits(repoDir string, edits []FileEdit, newFiles []NewFile) (*ApplyResult, error) {
	applied := make([]ApplyResultEntry, 0, len(edits))
	created := make([]ApplyResultEntry, 0, len(newFiles))

	resolvedRepo, err := filepath.EvalSymlinks(repoDir)
	if err != nil {
		return nil, fmt.Errorf("resolve repo dir: %w", err)
	}

	for _, edit := range edits {
		if err := rejectUnsafePath(edit.FilePath); err != nil {
			applied = append(applied, ApplyResultEntry{FilePath: edit.FilePath, Success: false, Error: err.Error()})
			continue
		}

		absolutePath := filepath.Join(repoDir, edit.FilePath)

		if info, err := os.Lstat(absolutePath); err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				applied = append(applied, ApplyResultEntry{FilePath: edit.FilePath, Success: false, Error: "Symlinks not allowed"})
				continue
			}
		} else if !os.IsNotExist(err) {
			applied = append(applied, ApplyResultEntry{FilePath: edit.FilePath, Success: false, Error: err.Error()})
			continue
		}

		resolvedPath, err := filepath.EvalSymlinks(absolutePath)
		if err != nil {
			applied = append(applied, ApplyResultEntry{FilePath: edit.FilePath, Success: false, Error: err.Error()})
			continue
		}

		if !strings.HasPrefix(resolvedPath, resolvedRepo+string(filepath.Separator)) {
			applied = append(applied, ApplyResultEntry{FilePath: edit.FilePath, Success: false, Error: "Path escapes repository"})
			continue
		}

		fileContent, err := os.ReadFile(absolutePath)
		if err != nil {
			applied = append(applied, ApplyResultEntry{FilePath: edit.FilePath, Success: false, Error: err.Error()})
			continue
		}

		fileContentStr := string(fileContent)
		match := FindMatch(fileContentStr, edit.SearchText)
		if match == nil {
			applied = append(applied, ApplyResultEntry{FilePath: edit.FilePath, Success: false, Error: "Search text not found or ambiguous", OriginalSearchText: edit.SearchText})
			continue
		}

		before := fileContentStr[:match.Start]
		after := fileContentStr[match.End:]
		newContent := before + edit.ReplaceText + after

		if err := os.WriteFile(absolutePath, []byte(newContent), 0644); err != nil {
			applied = append(applied, ApplyResultEntry{FilePath: edit.FilePath, Success: false, Error: err.Error()})
			continue
		}

		applied = append(applied, ApplyResultEntry{FilePath: edit.FilePath, Success: true})
	}

	for _, newFile := range newFiles {
		if err := rejectUnsafePath(newFile.FilePath); err != nil {
			created = append(created, ApplyResultEntry{FilePath: newFile.FilePath, Success: false, Error: err.Error()})
			continue
		}

		absolutePath := filepath.Join(repoDir, newFile.FilePath)
		parentDir := filepath.Dir(absolutePath)

		if err := os.MkdirAll(parentDir, 0755); err != nil {
			created = append(created, ApplyResultEntry{FilePath: newFile.FilePath, Success: false, Error: err.Error()})
			continue
		}

		resolvedParent, err := filepath.EvalSymlinks(parentDir)
		if err != nil {
			created = append(created, ApplyResultEntry{FilePath: newFile.FilePath, Success: false, Error: err.Error()})
			continue
		}

		if !strings.HasPrefix(resolvedParent, resolvedRepo+string(filepath.Separator)) && resolvedParent != resolvedRepo {
			created = append(created, ApplyResultEntry{FilePath: newFile.FilePath, Success: false, Error: "Path escapes repository"})
			continue
		}

		if err := os.WriteFile(absolutePath, []byte(newFile.Content), 0644); err != nil {
			created = append(created, ApplyResultEntry{FilePath: newFile.FilePath, Success: false, Error: err.Error()})
			continue
		}

		created = append(created, ApplyResultEntry{FilePath: newFile.FilePath, Success: true})
	}

	totalApplied := 0
	totalFailed := 0
	for _, r := range applied {
		if r.Success {
			totalApplied++
		} else {
			totalFailed++
		}
	}
	for _, r := range created {
		if r.Success {
			totalApplied++
		} else {
			totalFailed++
		}
	}

	return &ApplyResult{
		Applied:      applied,
		Created:      created,
		TotalApplied: totalApplied,
		TotalFailed:  totalFailed,
	}, nil
}
