package conversation

import (
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/KranzL/shipmates-oss/go-llm"
)

const (
	maxHistoryKeys      = 1000
	maxMessagesPerAgent = 20
)

type historyEntry struct {
	messages     []llm.ChatMessage
	lastAccessed atomic.Int64
}

type historyStore struct {
	mu      sync.RWMutex
	history map[string]*historyEntry
}

var history = &historyStore{
	history: make(map[string]*historyEntry),
}

func getHistoryKey(workspaceID, userID, agentName string) string {
	var builder strings.Builder
	builder.Grow(len(workspaceID) + len(userID) + len(agentName) + 2)
	builder.WriteString(workspaceID)
	builder.WriteString(":")
	builder.WriteString(userID)
	builder.WriteString(":")
	builder.WriteString(agentName)
	return builder.String()
}

func (h *historyStore) get(workspaceID, userID, agentName string) []llm.ChatMessage {
	key := getHistoryKey(workspaceID, userID, agentName)
	h.mu.RLock()
	entry, exists := h.history[key]
	h.mu.RUnlock()

	if !exists {
		return nil
	}

	entry.lastAccessed.Store(time.Now().Unix())

	result := make([]llm.ChatMessage, len(entry.messages))
	copy(result, entry.messages)
	return result
}

func (h *historyStore) add(workspaceID, userID, agentName, role, content string) {
	key := getHistoryKey(workspaceID, userID, agentName)
	h.mu.Lock()
	defer h.mu.Unlock()

	now := time.Now().Unix()

	entry, exists := h.history[key]
	if !exists {
		if len(h.history) >= maxHistoryKeys {
			h.evictOldestLocked()
		}
		entry = &historyEntry{
			messages: []llm.ChatMessage{},
		}
		entry.lastAccessed.Store(now)
		h.history[key] = entry
	}

	entry.messages = append(entry.messages, llm.ChatMessage{
		Role:    role,
		Content: content,
	})
	entry.lastAccessed.Store(now)

	if len(entry.messages) > maxMessagesPerAgent {
		messages := entry.messages
		excess := len(messages) - maxMessagesPerAgent
		copy(messages, messages[excess:])
		entry.messages = messages[:maxMessagesPerAgent]
	}
}

type historyEntryWithKey struct {
	key          string
	lastAccessed int64
}

func (h *historyStore) evictOldestLocked() {
	if len(h.history) == 0 {
		return
	}

	toDelete := len(h.history) - maxHistoryKeys + 1

	entries := make([]historyEntryWithKey, 0, len(h.history))
	for k, v := range h.history {
		entries = append(entries, historyEntryWithKey{
			key:          k,
			lastAccessed: v.lastAccessed.Load(),
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].lastAccessed < entries[j].lastAccessed
	})

	for i := 0; i < toDelete && i < len(entries); i++ {
		delete(h.history, entries[i].key)
	}
}

func GetHistory(workspaceID, userID, agentName string) []llm.ChatMessage {
	return history.get(workspaceID, userID, agentName)
}

func AddToHistory(workspaceID, userID, agentName, role, content string) {
	history.add(workspaceID, userID, agentName, role, content)
}
