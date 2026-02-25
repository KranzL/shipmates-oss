package voice

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	goshared "github.com/KranzL/shipmates-oss/go-shared"
	"github.com/KranzL/shipmates-oss/bot/conversation"
	"github.com/KranzL/shipmates-oss/bot/discord"
)

const (
	defaultVoiceID      = "21m00Tcm4TlvDq8ikWAM"
	minAudioSize        = 9600
	workspaceCacheTTL   = 60 * time.Second
	maxWorkspaceCacheSize = 1000
)

type MessageHandler interface {
	HandleMessage(ctx context.Context, text, workspaceID, userID, platform string, logFn discord.LogFunc, threadID string) (string, error)
}

type workspaceCacheEntry struct {
	workspace  *goshared.Workspace
	userConfig *goshared.UserConfig
	expiresAt  time.Time
}

type lruNode struct {
	key  string
	prev *lruNode
	next *lruNode
}

type Handler struct {
	stt            *STTClient
	tts            *TTSClient
	db             *goshared.DB
	msgHandler     MessageHandler
	workspaceCache map[string]*workspaceCacheEntry
	cacheMu        sync.RWMutex
	lruHead        *lruNode
	lruTail        *lruNode
	lruNodes       map[string]*lruNode
	cancel         context.CancelFunc
}

func NewHandler(stt *STTClient, tts *TTSClient, db *goshared.DB, msgHandler MessageHandler) (*Handler, error) {
	if stt == nil {
		return nil, fmt.Errorf("stt client is required")
	}
	if tts == nil {
		return nil, fmt.Errorf("tts client is required")
	}
	if db == nil {
		return nil, fmt.Errorf("db is required")
	}
	if msgHandler == nil {
		return nil, fmt.Errorf("message handler is required")
	}

	ctx, cancel := context.WithCancel(context.Background())
	h := &Handler{
		stt:            stt,
		tts:            tts,
		db:             db,
		msgHandler:     msgHandler,
		workspaceCache: make(map[string]*workspaceCacheEntry),
		lruNodes:       make(map[string]*lruNode),
		cancel:         cancel,
	}

	go h.cleanupLoop(ctx)

	return h, nil
}

func (h *Handler) cleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.evictExpiredAndEnforceMax()
		}
	}
}

func (h *Handler) evictExpiredAndEnforceMax() {
	now := time.Now()
	h.cacheMu.Lock()
	defer h.cacheMu.Unlock()

	for key, entry := range h.workspaceCache {
		if now.After(entry.expiresAt) {
			delete(h.workspaceCache, key)
			h.removeLRUNode(key)
		}
	}

	for len(h.workspaceCache) > maxWorkspaceCacheSize {
		h.evictLRU()
	}
}

func (h *Handler) HandleSpeech(ctx context.Context, audioData []byte, guildID, userID string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	if len(audioData) > MaxSTTRequestBytes {
		return nil, nil
	}

	if len(audioData) < minAudioSize {
		return nil, nil
	}

	transcript, err := h.stt.Transcribe(ctx, audioData)
	if err != nil {
		if errors.Is(err, ErrLowConfidence) {
			return nil, nil
		}
		return nil, fmt.Errorf("transcribe audio: %w", err)
	}

	if transcript == "" {
		return nil, nil
	}

	workspace, userConfig, err := h.getWorkspaceAndConfig(ctx, guildID)
	if err != nil {
		return nil, fmt.Errorf("get workspace and config: %w", err)
	}

	if workspace == nil {
		return h.speakError(ctx, "This server isn't connected to Shipmates yet")
	}

	if userConfig == nil || len(userConfig.Agents) == 0 {
		return h.speakError(ctx, "No agents are configured. Visit shipmates dot dev slash dashboard to set them up")
	}

	voiceID := h.getVoiceIDForMessage(userConfig.Agents)

	noOpLogFn := func(ctx context.Context, message string) (string, error) {
		return "", nil
	}

	responseText, err := h.msgHandler.HandleMessage(ctx, transcript, workspace.ID, userID, "discord", noOpLogFn, "")
	if err != nil {
		return h.speakError(ctx, "Something went wrong. Try again")
	}

	if responseText == "" {
		return nil, nil
	}

	audioResponse, err := h.tts.Synthesize(ctx, responseText, voiceID)
	if err != nil {
		return h.speakError(ctx, "I have a response but could not generate audio")
	}

	return audioResponse, nil
}

func (h *Handler) getVoiceIDForMessage(agents []goshared.AgentConfig) string {
	if len(agents) == 1 && agents[0].VoiceID != "" {
		return agents[0].VoiceID
	}
	return defaultVoiceID
}

func (h *Handler) getWorkspaceAndConfig(ctx context.Context, guildID string) (*goshared.Workspace, *goshared.UserConfig, error) {
	h.cacheMu.RLock()
	entry, found := h.workspaceCache[guildID]
	if found && time.Now().Before(entry.expiresAt) {
		h.cacheMu.RUnlock()
		h.cacheMu.Lock()
		h.moveLRUToFront(guildID)
		h.cacheMu.Unlock()
		return entry.workspace, entry.userConfig, nil
	}
	h.cacheMu.RUnlock()

	workspace, err := h.db.GetWorkspaceByPlatform(ctx, goshared.PlatformDiscord, guildID)
	if err != nil {
		return nil, nil, err
	}
	if workspace == nil {
		return nil, nil, nil
	}

	userConfig, err := conversation.GetUserConfigByWorkspace(ctx, h.db, goshared.PlatformDiscord, workspace.ID)
	if err != nil {
		return nil, nil, err
	}

	h.cacheMu.Lock()
	if len(h.workspaceCache) >= maxWorkspaceCacheSize {
		h.evictLRU()
	}
	h.workspaceCache[guildID] = &workspaceCacheEntry{
		workspace:  workspace,
		userConfig: userConfig,
		expiresAt:  time.Now().Add(workspaceCacheTTL),
	}
	h.addLRUNode(guildID)
	h.cacheMu.Unlock()

	return workspace, userConfig, nil
}

func (h *Handler) addLRUNode(key string) {
	if _, exists := h.lruNodes[key]; exists {
		h.moveLRUToFront(key)
		return
	}

	node := &lruNode{key: key}
	h.lruNodes[key] = node

	if h.lruHead == nil {
		h.lruHead = node
		h.lruTail = node
	} else {
		node.next = h.lruHead
		h.lruHead.prev = node
		h.lruHead = node
	}
}

func (h *Handler) moveLRUToFront(key string) {
	node, exists := h.lruNodes[key]
	if !exists || node == h.lruHead {
		return
	}

	if node.prev != nil {
		node.prev.next = node.next
	}
	if node.next != nil {
		node.next.prev = node.prev
	}
	if node == h.lruTail {
		h.lruTail = node.prev
	}

	node.prev = nil
	node.next = h.lruHead
	h.lruHead.prev = node
	h.lruHead = node
}

func (h *Handler) removeLRUNode(key string) {
	node, exists := h.lruNodes[key]
	if !exists {
		return
	}

	if node.prev != nil {
		node.prev.next = node.next
	}
	if node.next != nil {
		node.next.prev = node.prev
	}
	if node == h.lruHead {
		h.lruHead = node.next
	}
	if node == h.lruTail {
		h.lruTail = node.prev
	}

	delete(h.lruNodes, key)
}

func (h *Handler) evictLRU() {
	if h.lruTail == nil {
		return
	}

	key := h.lruTail.key
	delete(h.workspaceCache, key)
	h.removeLRUNode(key)
}

func (h *Handler) speakError(ctx context.Context, message string) ([]byte, error) {
	audio, err := h.tts.Synthesize(ctx, message, defaultVoiceID)
	if err != nil {
		return nil, nil
	}
	return audio, nil
}

func (h *Handler) Close() {
	if h.cancel != nil {
		h.cancel()
	}
	if h.stt != nil {
		h.stt.Close()
	}
	if h.tts != nil {
		h.tts.Close()
	}
}
