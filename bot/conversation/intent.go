package conversation

import (
	"regexp"
	"strconv"
	"strings"
	"sync"
)

var workPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(build|create|implement|write|code|fix|debug|refactor|deploy|add|remove|delete|update)\b`),
	regexp.MustCompile(`(?i)\b(make me|set up|work on|take care of|handle|figure out|look into|investigate|analyze|review|check|explore|plan|design|improve|optimize)\b`),
	regexp.MustCompile(`(?i)\b(go|can you|could you|please|would you)\b.*\b(the|this|our|my|a)\b`),
}

var testGenKeywords = []string{
	"generate tests",
	"write tests",
	"add tests",
	"create tests",
	"test coverage",
	"increase coverage",
	"improve coverage",
	"test generation",
	"generate test suite",
}

var coveragePattern = regexp.MustCompile(`(?i)(?:target|coverage|aim for)\s+(\d+)%`)

var bigramMapPool = sync.Pool{
	New: func() interface{} {
		return make(map[string]int)
	},
}

func DetectWorkRequest(text string) bool {
	for _, p := range workPatterns {
		if p.MatchString(text) {
			return true
		}
	}
	return false
}

type TestGenIntent struct {
	Detected       bool
	TargetCoverage int
}

func DetectTestGenIntent(text string) TestGenIntent {
	textLower := strings.ToLower(text)

	detected := false
	for _, keyword := range testGenKeywords {
		if strings.Contains(textLower, keyword) {
			detected = true
			break
		}
	}

	if !detected {
		return TestGenIntent{Detected: false}
	}

	match := coveragePattern.FindStringSubmatch(text)
	if match != nil && len(match) > 1 {
		coverage, err := strconv.Atoi(match[1])
		if err == nil && coverage > 0 && coverage <= 100 {
			return TestGenIntent{
				Detected:       true,
				TargetCoverage: coverage,
			}
		}
	}

	return TestGenIntent{
		Detected:       true,
		TargetCoverage: 80,
	}
}

func bigramSimilarity(a, b string) float64 {
	aLower := strings.ToLower(a)
	bLower := strings.ToLower(b)

	if aLower == bLower {
		return 1.0
	}
	if len(aLower) < 2 || len(bLower) < 2 {
		return 0.0
	}

	bigrams := bigramMapPool.Get().(map[string]int)
	for k := range bigrams {
		delete(bigrams, k)
	}

	for i := 0; i < len(aLower)-1; i++ {
		bigram := aLower[i : i+2]
		bigrams[bigram]++
	}

	intersect := 0
	for i := 0; i < len(bLower)-1; i++ {
		bigram := bLower[i : i+2]
		if count, exists := bigrams[bigram]; exists && count > 0 {
			bigrams[bigram]--
			intersect++
		}
	}

	bigramMapPool.Put(bigrams)

	return (2.0 * float64(intersect)) / float64(len(aLower)-1+len(bLower)-1)
}

type repoInfo struct {
	original       string
	lowerName      string
	lowerFull      string
	nameNoDashes   string
}

func DetectRepo(text string, repos []string) string {
	textLower := strings.ToLower(text)

	repoCache := make([]repoInfo, len(repos))
	for i, repo := range repos {
		parts := strings.Split(repo, "/")
		repoName := ""
		if len(parts) > 0 {
			repoName = strings.ToLower(parts[len(parts)-1])
		}
		repoCache[i] = repoInfo{
			original:       repo,
			lowerName:      repoName,
			lowerFull:      strings.ToLower(repo),
			nameNoDashes:   strings.ReplaceAll(strings.ReplaceAll(repoName, "-", ""), "_", ""),
		}
	}

	for i := range repoCache {
		if strings.Contains(textLower, repoCache[i].lowerFull) || strings.Contains(textLower, repoCache[i].lowerName) {
			return repoCache[i].original
		}
	}

	textWords := strings.Fields(textLower)
	var bestRepo string
	bestScore := 0.0

	for i := range repoCache {
		for length := 2; length <= min(len(textWords), 5); length++ {
			for j := 0; j <= len(textWords)-length; j++ {
				phrase := strings.Join(textWords[j:j+length], "")
				score := bigramSimilarity(phrase, repoCache[i].nameNoDashes)
				if score > bestScore {
					bestScore = score
					bestRepo = repoCache[i].original
				}
			}
		}
	}

	if bestScore >= 0.4 {
		return bestRepo
	}

	return ""
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
