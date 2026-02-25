package conversation

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/KranzL/shipmates-oss/go-llm"
	"github.com/KranzL/shipmates-oss/go-shared"
)

const (
	maxClarificationThreads = 1000
	clarificationTTL        = 30 * time.Minute
	handleMessageTimeout    = 60 * time.Second
	messagesPerMinute       = 20
)

var agentEmojis = []string{
	"\U0001F7E3", "\U0001F535", "\U0001F7E2", "\U0001F7E0", "\U0001F534",
	"\U0001F7E1", "\u26AA", "\U0001F7E4",
}

type tokenBucket struct {
	tokens     int
	lastRefill time.Time
}

type Manager struct {
	db                       *goshared.DB
	clarificationThreads     map[string]time.Time
	clarificationThreadsMu   sync.RWMutex
	agentEmojiMap            map[string]string
	agentEmojiMapMu          sync.RWMutex
	pendingDeliveryChoices   map[string]*pendingDeliveryChoice
	pendingDeliveryChoicesMu sync.Mutex
	rateLimiters             map[string]*tokenBucket
	rateLimitersMu           sync.RWMutex
	cancel                   context.CancelFunc
}

type pendingDeliveryChoice struct {
	agentName string
	voiceID   string
	guildID   string
	resolve   func(pref deliveryPreference)
}

type deliveryPreference string

const (
	deliveryVoice deliveryPreference = "voice"
	deliveryChat  deliveryPreference = "chat"
)

func NewManager(db *goshared.DB) *Manager {
	ctx, cancel := context.WithCancel(context.Background())

	m := &Manager{
		db:                     db,
		clarificationThreads:   make(map[string]time.Time),
		agentEmojiMap:          make(map[string]string),
		pendingDeliveryChoices: make(map[string]*pendingDeliveryChoice),
		rateLimiters:           make(map[string]*tokenBucket),
		cancel:                 cancel,
	}

	go m.cleanupClarificationThreads(ctx)
	go m.cleanupRateLimiters(ctx)

	return m
}

func (m *Manager) Close() {
	if m.cancel != nil {
		m.cancel()
	}
}

func (m *Manager) cleanupClarificationThreads(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			threshold := time.Now().Add(-clarificationTTL)

			m.clarificationThreadsMu.RLock()
			expiredKeys := make([]string, 0)
			for threadID, createdAt := range m.clarificationThreads {
				if createdAt.Before(threshold) {
					expiredKeys = append(expiredKeys, threadID)
				}
			}
			m.clarificationThreadsMu.RUnlock()

			if len(expiredKeys) > 0 {
				m.clarificationThreadsMu.Lock()
				for _, key := range expiredKeys {
					delete(m.clarificationThreads, key)
				}
				m.clarificationThreadsMu.Unlock()
			}

			if len(m.clarificationThreads) > maxClarificationThreads {
				m.clarificationThreadsMu.Lock()
				var oldest string
				var oldestTime time.Time
				first := true
				for k, v := range m.clarificationThreads {
					if first || v.Before(oldestTime) {
						oldest = k
						oldestTime = v
						first = false
					}
				}
				if oldest != "" {
					delete(m.clarificationThreads, oldest)
				}
				m.clarificationThreadsMu.Unlock()
			}
		}
	}
}

func (m *Manager) cleanupRateLimiters(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.rateLimitersMu.Lock()
			threshold := time.Now().Add(-2 * time.Minute)
			for key, bucket := range m.rateLimiters {
				if bucket.lastRefill.Before(threshold) {
					delete(m.rateLimiters, key)
				}
			}
			m.rateLimitersMu.Unlock()
		}
	}
}

func (m *Manager) checkRateLimit(userID string) bool {
	m.rateLimitersMu.Lock()
	defer m.rateLimitersMu.Unlock()

	now := time.Now()
	bucket, exists := m.rateLimiters[userID]

	if !exists {
		m.rateLimiters[userID] = &tokenBucket{
			tokens:     messagesPerMinute - 1,
			lastRefill: now,
		}
		return true
	}

	elapsed := now.Sub(bucket.lastRefill)
	tokensToAdd := int(elapsed.Minutes() * messagesPerMinute)

	if tokensToAdd > 0 {
		bucket.tokens += tokensToAdd
		if bucket.tokens > messagesPerMinute {
			bucket.tokens = messagesPerMinute
		}
		bucket.lastRefill = now
	}

	if bucket.tokens > 0 {
		bucket.tokens--
		return true
	}

	return false
}

func (m *Manager) RegisterClarificationThread(threadID string) {
	m.clarificationThreadsMu.Lock()
	m.clarificationThreads[threadID] = time.Now()
	m.clarificationThreadsMu.Unlock()
}

func (m *Manager) UnregisterClarificationThread(threadID string) {
	m.clarificationThreadsMu.Lock()
	delete(m.clarificationThreads, threadID)
	m.clarificationThreadsMu.Unlock()
}

func (m *Manager) getAgentEmoji(agentName string, allAgents []goshared.AgentConfig) string {
	m.agentEmojiMapMu.RLock()
	if emoji, exists := m.agentEmojiMap[agentName]; exists {
		m.agentEmojiMapMu.RUnlock()
		return emoji
	}
	m.agentEmojiMapMu.RUnlock()

	idx := -1
	for i := range allAgents {
		if allAgents[i].Name == agentName {
			idx = i
			break
		}
	}

	var emoji string
	if idx >= 0 {
		emoji = agentEmojis[idx%len(agentEmojis)]
	} else {
		m.agentEmojiMapMu.RLock()
		emoji = agentEmojis[len(m.agentEmojiMap)%len(agentEmojis)]
		m.agentEmojiMapMu.RUnlock()
	}

	m.agentEmojiMapMu.Lock()
	m.agentEmojiMap[agentName] = emoji
	m.agentEmojiMapMu.Unlock()

	return emoji
}

func (m *Manager) HandleMessage(ctx context.Context, text, workspaceID, userID, platform string, logFn func(ctx context.Context, message string) (string, error), threadID string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, handleMessageTimeout)
	defer cancel()

	text = sanitizeInput(text)

	if len(text) > 10000 {
		return "Message too long. Please keep messages under 10,000 characters.", nil
	}

	if !m.checkRateLimit(userID) {
		return "You're sending messages too quickly. Please wait a moment before trying again.", nil
	}

	plat := goshared.Platform(platform)

	userConfig, err := GetUserConfigByWorkspace(ctx, m.db, plat, workspaceID)
	if err != nil {
		return "", fmt.Errorf("get user config: %w", err)
	}
	if userConfig == nil || len(userConfig.Agents) == 0 {
		return "No agents are configured for this workspace. Visit https://shipmates.dev/dashboard to set up your agents.", nil
	}

	isTestMode := os.Getenv("TEST_MODE") == "true"

	if threadID != "" {
		m.clarificationThreadsMu.RLock()
		_, isClarificationThread := m.clarificationThreads[threadID]
		m.clarificationThreadsMu.RUnlock()

		if isClarificationThread {
			waitingSession, err := m.db.GetWaitingSessionByThreadID(ctx, userConfig.ID, threadID)
			if err != nil {
				return "", fmt.Errorf("get waiting session by thread id: %w", err)
			}

			if waitingSession != nil {
				if waitingSession.PlatformUserID == nil || *waitingSession.PlatformUserID != userID {
					return "", nil
				}

				clarifications, err := m.db.GetPendingClarifications(ctx, waitingSession.ID)
				if err != nil {
					return "", fmt.Errorf("get pending clarifications: %w", err)
				}

				if len(clarifications) > 0 {
					isSkip := strings.TrimSpace(strings.ToLower(text)) == "skip" ||
						strings.TrimSpace(strings.ToLower(text)) == "nevermind" ||
						strings.TrimSpace(strings.ToLower(text)) == "never mind" ||
						strings.TrimSpace(strings.ToLower(text)) == "cancel" ||
						strings.TrimSpace(strings.ToLower(text)) == "just decide" ||
						strings.TrimSpace(strings.ToLower(text)) == "your call"

					answerText := text
					if isSkip {
						answerText = "Use your best judgment and proceed."
					}

					err := m.db.AnswerClarification(ctx, clarifications[0].ID, userConfig.ID, answerText)
					if err != nil {
						return "", fmt.Errorf("answer clarification: %w", err)
					}

					m.UnregisterClarificationThread(threadID)

					if isSkip {
						return "Skipping -- telling the agent to use its best judgment.", nil
					}
					return "Got it, passing your answer to the agent.", nil
				}
			}
		}
	}

	result := RouteMessage(text, userConfig.Agents, workspaceID, userID)
	if result == nil {
		agentList := ""
		for i := range userConfig.Agents {
			emoji := m.getAgentEmoji(userConfig.Agents[i].Name, userConfig.Agents)
			agentList += fmt.Sprintf("%s *%s* -- %s\n", emoji, userConfig.Agents[i].Name, userConfig.Agents[i].Role)
		}
		return fmt.Sprintf("Who should handle this? Say their name to get started:\n%s", agentList), nil
	}

	agent := result.Agent
	message := result.Message

	isWork := DetectWorkRequest(message)
	testGenResult := DetectTestGenIntent(message)

	AddToHistory(workspaceID, userID, agent.Name, "user", message)

	var responseText string

	if isTestMode {
		responseText = getTestResponse(isWork)
	} else {
		if len(userConfig.APIKeys) == 0 {
			return "", nil
		}

		primaryKey := userConfig.APIKeys[0]
		llmClient, err := llm.NewClient(llm.Provider(primaryKey.Provider), primaryKey.APIKey, &llm.ClientOptions{
			BaseURL: stringValue(primaryKey.BaseURL),
			ModelID: stringValue(primaryKey.ModelID),
		})
		if err != nil {
			return "", fmt.Errorf("create llm client: %w", err)
		}

		histMessages := GetHistory(workspaceID, userID, agent.Name)

		targetRepo := ""
		if isWork {
			targetRepo = DetectRepo(message, userConfig.Repos)
		}

		if isWork && userConfig.GithubToken != nil && targetRepo != "" {
			if testGenResult.Detected && plat == goshared.PlatformDiscord {
				targetCoverageStr := fmt.Sprintf("%d%%", testGenResult.TargetCoverage)
				if testGenResult.TargetCoverage == 80 {
					targetCoverageStr = "optimal"
				}

				responseText = fmt.Sprintf("Starting test generation targeting %s coverage. I'll update you when the PR is ready. Hang up whenever.", targetCoverageStr)

			} else if plat == goshared.PlatformDiscord {
				responseText = "Let me review that. Want me to read my answer on this call, or post it in chat?"

			} else {
				systemPrompt := buildSystemPrompt(agent, plat) + "\nThe user just asked you to do work. Acknowledge briefly and say you're on it."
				chatResult, err := llmClient.Chat(ctx, histMessages[max(0, len(histMessages)-10):], systemPrompt, 100)
				if err != nil {
					fmt.Fprintf(os.Stderr, "llm chat error: %v\n", err)
					return "I had trouble generating a response. Please try again.", nil
				}
				responseText = chatResult.Text
			}
		} else {
			userMessages := append(histMessages, llm.ChatMessage{Role: "user", Content: message})
			systemPrompt := buildSystemPrompt(agent, plat)
			chatResult, err := llmClient.Chat(ctx, userMessages[max(0, len(userMessages)-10):], systemPrompt, 100)
			if err != nil {
				fmt.Fprintf(os.Stderr, "llm chat error: %v\n", err)
				return "I had trouble generating a response. Please try again.", nil
			}
			responseText = chatResult.Text
		}
	}

	AddToHistory(workspaceID, userID, agent.Name, "assistant", responseText)

	if len(userConfig.Agents) > 1 {
		emoji := m.getAgentEmoji(agent.Name, userConfig.Agents)
		boldOpen := "**"
		boldClose := "**"
		if platform == "slack" {
			boldOpen = "*"
			boldClose = "*"
		}
		return fmt.Sprintf("%s %s%s%s: %s", emoji, boldOpen, agent.Name, boldClose, responseText), nil
	}

	return responseText, nil
}

func buildSystemPrompt(agent goshared.AgentConfig, platform goshared.Platform) string {
	var platformInstructions []string
	if platform == goshared.PlatformDiscord {
		platformInstructions = []string{
			"You are speaking in a Discord voice call. Everything you say is read aloud via text-to-speech so you MUST be extremely brief.",
			"STRICT RULES: Maximum 1-2 short sentences. Never more than 30 words total. No lists. No elaboration unless explicitly asked.",
			"Never reference specific file names, paths, or extensions. Talk about features, components, and systems instead.",
			"Never use markdown formatting, code blocks, bullet points, or special characters - speak naturally as if talking to a colleague.",
		}
	} else {
		platformInstructions = []string{
			"You are responding in a Slack channel. Keep responses concise but you can use markdown formatting.",
			"Keep responses to 2-3 sentences unless the user asks for more detail.",
		}
	}

	parts := []string{
		fmt.Sprintf("You are %s, a %s on a software development team.", agent.Name, agent.Role),
		fmt.Sprintf("Your personality: %s", agent.Personality),
		fmt.Sprintf("You specialize in: %s", strings.Join(agent.Scope.Languages, ", ")),
		fmt.Sprintf("Your focus areas: %s", strings.Join(agent.Scope.Directories, ", ")),
		"",
	}
	parts = append(parts, platformInstructions...)

	return strings.Join(parts, "\n")
}

var testResponses = map[string][]string{
	"default": {
		"Got it, I hear you loud and clear.",
		"Understood. I'm here and ready.",
		"Copy that. What do you need?",
		"I'm listening. Go ahead.",
	},
	"work": {
		"On it. Let me dig into the repo and see what makes sense.",
		"Good call. Give me a minute to look through the codebase.",
		"I'll take a look. Let me explore what we're working with.",
	},
}

func getTestResponse(isWork bool) string {
	var pool []string
	if isWork {
		pool = testResponses["work"]
	} else {
		pool = testResponses["default"]
	}
	return pool[rand.Intn(len(pool))]
}

func stringValue(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func sanitizeInput(text string) string {
	runes := []rune(text)
	filtered := make([]rune, 0, len(runes))

	for _, r := range runes {
		if r == '\n' || r == '\t' {
			filtered = append(filtered, r)
			continue
		}
		if unicode.IsControl(r) {
			continue
		}
		if r >= 0x200B && r <= 0x200F {
			continue
		}
		if r >= 0x202A && r <= 0x202E {
			continue
		}
		if r == 0xFEFF {
			continue
		}
		filtered = append(filtered, r)
	}

	return strings.TrimSpace(string(filtered))
}
