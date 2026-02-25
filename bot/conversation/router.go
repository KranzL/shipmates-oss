package conversation

import (
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/KranzL/shipmates-oss/go-shared"
)

const (
	lastAddressedTTL         = 1 * time.Hour
	lastAddressedMaxSize     = 10000
	lastAddressedCleanupInterval = 15 * time.Minute
)

type lastAddressedEntryItem struct {
	agentName    string
	lastAccessed atomic.Int64
}

type lastAddressedEntry struct {
	mu        sync.RWMutex
	addressed map[string]*lastAddressedEntryItem
}

var lastAddressed = lastAddressedEntry{
	addressed: make(map[string]*lastAddressedEntryItem),
}

func init() {
	go func() {
		ticker := time.NewTicker(lastAddressedCleanupInterval)
		defer ticker.Stop()
		for range ticker.C {
			lastAddressed.cleanup()
		}
	}()
}

func (l *lastAddressedEntry) cleanup() {
	l.mu.Lock()
	defer l.mu.Unlock()

	threshold := time.Now().Add(-lastAddressedTTL).Unix()
	for key, entry := range l.addressed {
		if entry.lastAccessed.Load() < threshold {
			delete(l.addressed, key)
		}
	}

	if len(l.addressed) > lastAddressedMaxSize {
		type entryWithKey struct {
			key          string
			lastAccessed int64
		}

		entries := make([]entryWithKey, 0, len(l.addressed))
		for key, entry := range l.addressed {
			entries = append(entries, entryWithKey{
				key:          key,
				lastAccessed: entry.lastAccessed.Load(),
			})
		}

		sort.Slice(entries, func(i, j int) bool {
			return entries[i].lastAccessed < entries[j].lastAccessed
		})

		toDelete := len(l.addressed) - lastAddressedMaxSize
		for i := 0; i < toDelete && i < len(entries); i++ {
			delete(l.addressed, entries[i].key)
		}
	}
}

var soundexCodeMap = map[rune]string{
	'B': "1", 'F': "1", 'P': "1", 'V': "1",
	'C': "2", 'G': "2", 'J': "2", 'K': "2", 'Q': "2", 'S': "2", 'X': "2", 'Z': "2",
	'D': "3", 'T': "3",
	'L': "4",
	'M': "5", 'N': "5",
	'R': "6",
}

func soundex(str string) string {
	upper := strings.ToUpper(str)
	s := strings.Map(func(r rune) rune {
		if r >= 'A' && r <= 'Z' {
			return r
		}
		return -1
	}, upper)

	if len(s) == 0 {
		return "0000"
	}

	var builder strings.Builder
	builder.Grow(4)

	builder.WriteByte(s[0])
	lastCode := soundexCodeMap[rune(s[0])]

	for i := 1; i < len(s) && builder.Len() < 4; i++ {
		code := soundexCodeMap[rune(s[i])]
		if code != "" && code != lastCode {
			builder.WriteString(code)
		}
		if code != "" {
			lastCode = code
		}
	}

	result := builder.String()
	return (result + "0000")[:4]
}

func extractWords(text string) []string {
	lower := strings.ToLower(text)
	cleaned := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || r == ' ' {
			return r
		}
		return -1
	}, lower)

	words := strings.Fields(cleaned)
	filtered := make([]string, 0, len(words))
	for _, w := range words {
		if w != "" {
			filtered = append(filtered, w)
		}
	}
	return filtered
}

func findAgentBySound(word string, agents []goshared.AgentConfig) *goshared.AgentConfig {
	wordSoundex := soundex(word)
	wordLower := strings.ToLower(word)

	for i := range agents {
		nameLower := strings.ToLower(agents[i].Name)
		if wordLower == nameLower {
			return &agents[i]
		}
	}

	for i := range agents {
		if soundex(agents[i].Name) == wordSoundex {
			return &agents[i]
		}
	}

	return nil
}

func getLastAgentKey(guildID, userID string) string {
	return guildID + ":" + userID
}

type RoutingResult struct {
	Agent   goshared.AgentConfig
	Message string
}

func RouteMessage(text string, agents []goshared.AgentConfig, guildID, userID string) *RoutingResult {
	trimmed := strings.TrimSpace(text)
	if len(trimmed) == 0 {
		return nil
	}

	words := extractWords(trimmed)

	for _, word := range words {
		agent := findAgentBySound(word, agents)
		if agent != nil {
			key := getLastAgentKey(guildID, userID)
			now := time.Now().Unix()
			lastAddressed.mu.Lock()
			entry, exists := lastAddressed.addressed[key]
			if !exists {
				entry = &lastAddressedEntryItem{agentName: agent.Name}
				lastAddressed.addressed[key] = entry
			} else {
				entry.agentName = agent.Name
			}
			entry.lastAccessed.Store(now)
			lastAddressed.mu.Unlock()
			return &RoutingResult{
				Agent:   *agent,
				Message: trimmed,
			}
		}
	}

	key := getLastAgentKey(guildID, userID)
	lastAddressed.mu.RLock()
	entry := lastAddressed.addressed[key]
	lastAddressed.mu.RUnlock()

	if entry != nil {
		now := time.Now().Unix()
		entry.lastAccessed.Store(now)
		lastAgentName := entry.agentName
		for i := range agents {
			if agents[i].Name == lastAgentName {
				return &RoutingResult{
					Agent:   agents[i],
					Message: trimmed,
				}
			}
		}
	}

	return nil
}
