package slack

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
	"golang.org/x/time/rate"
)

const (
	MaxSlackMessageLength      = 4000
	MaxSlackSendLength         = 3000
	SlackRatePerUser           = 20
	SlackBurstPerUser          = 5
	rateLimiterEvictAfter      = 30 * time.Minute
	rateLimiterCleanupInterval = 10 * time.Minute
	workspaceCacheMaxEntries   = 10000
	workspaceCacheTTL          = 5 * time.Minute
	maxConcurrentHandlers      = 50
)

var slackMentionPattern = regexp.MustCompile(`<@[A-Z0-9]+>`)

type LogFunc func(ctx context.Context, message string) (string, error)

type MessageHandler interface {
	HandleMessage(ctx context.Context, text, workspaceID, userID, platform string, logFn LogFunc, threadID string) (string, error)
}

type WorkspaceChecker interface {
	WorkspaceExists(ctx context.Context, platform, platformID string) (bool, error)
}

type rateLimiterEntry struct {
	limiter  *rate.Limiter
	lastUsed atomic.Int64
}

type workspaceCacheEntry struct {
	exists    bool
	expiresAt time.Time
}

type lruNode struct {
	key  string
	prev *lruNode
	next *lruNode
}

type Bot struct {
	client           *slack.Client
	socketClient     *socketmode.Client
	messageHandler   MessageHandler
	workspaceChecker WorkspaceChecker
	botUserID        string
	cachedTeamID     atomic.Value

	rateLimiters   map[string]rateLimiterEntry
	rateLimitersMu sync.RWMutex

	workspaceCache      map[string]workspaceCacheEntry
	workspaceCacheMu    sync.RWMutex
	workspaceLRUHead    *lruNode
	workspaceLRUTail    *lruNode
	workspaceLRUNodeMap map[string]*lruNode

	handlerSemaphore chan struct{}
}

type BotConfig struct {
	BotToken         string
	AppToken         string
	MessageHandler   MessageHandler
	WorkspaceChecker WorkspaceChecker
}

func NewBot(ctx context.Context, cfg BotConfig) (*Bot, error) {
	if cfg.BotToken == "" {
		return nil, fmt.Errorf("slack bot token is required")
	}
	if cfg.AppToken == "" {
		return nil, fmt.Errorf("slack app token is required")
	}
	if cfg.MessageHandler == nil {
		return nil, fmt.Errorf("message handler is required")
	}

	client := slack.New(
		cfg.BotToken,
		slack.OptionAppLevelToken(cfg.AppToken),
	)

	socketClient := socketmode.New(
		client,
		socketmode.OptionLog(log.New(log.Writer(), "[slack-socket] ", log.LstdFlags)),
	)

	bot := &Bot{
		client:              client,
		socketClient:        socketClient,
		messageHandler:      cfg.MessageHandler,
		workspaceChecker:    cfg.WorkspaceChecker,
		rateLimiters:        make(map[string]rateLimiterEntry),
		workspaceCache:      make(map[string]workspaceCacheEntry),
		workspaceLRUNodeMap: make(map[string]*lruNode),
		handlerSemaphore:    make(chan struct{}, maxConcurrentHandlers),
	}

	go bot.cleanupEvictionLoop(ctx)

	return bot, nil
}

func (b *Bot) cleanupEvictionLoop(ctx context.Context) {
	ticker := time.NewTicker(rateLimiterCleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			b.evictStaleRateLimiters()
			b.evictStaleWorkspaceCache()
		}
	}
}

func (b *Bot) evictStaleRateLimiters() {
	nowNano := time.Now().UnixNano()
	thresholdNano := time.Duration(rateLimiterEvictAfter).Nanoseconds()

	b.rateLimitersMu.Lock()
	for key, entry := range b.rateLimiters {
		lastUsedNano := entry.lastUsed.Load()
		if nowNano-lastUsedNano > thresholdNano {
			delete(b.rateLimiters, key)
		}
	}
	b.rateLimitersMu.Unlock()
}

func (b *Bot) evictStaleWorkspaceCache() {
	now := time.Now()
	b.workspaceCacheMu.Lock()
	defer b.workspaceCacheMu.Unlock()

	for key, entry := range b.workspaceCache {
		if now.After(entry.expiresAt) {
			delete(b.workspaceCache, key)
			b.removeLRUNode(key)
		}
	}

	for len(b.workspaceCache) > workspaceCacheMaxEntries {
		b.evictLRU()
	}
}

func (b *Bot) addLRUNode(key string) {
	if _, exists := b.workspaceLRUNodeMap[key]; exists {
		b.moveLRUToFront(key)
		return
	}

	node := &lruNode{key: key}
	b.workspaceLRUNodeMap[key] = node

	if b.workspaceLRUHead == nil {
		b.workspaceLRUHead = node
		b.workspaceLRUTail = node
	} else {
		node.next = b.workspaceLRUHead
		b.workspaceLRUHead.prev = node
		b.workspaceLRUHead = node
	}
}

func (b *Bot) moveLRUToFront(key string) {
	node, exists := b.workspaceLRUNodeMap[key]
	if !exists || node == b.workspaceLRUHead {
		return
	}

	if node.prev != nil {
		node.prev.next = node.next
	}
	if node.next != nil {
		node.next.prev = node.prev
	}
	if node == b.workspaceLRUTail {
		b.workspaceLRUTail = node.prev
	}

	node.prev = nil
	node.next = b.workspaceLRUHead
	b.workspaceLRUHead.prev = node
	b.workspaceLRUHead = node
}

func (b *Bot) removeLRUNode(key string) {
	node, exists := b.workspaceLRUNodeMap[key]
	if !exists {
		return
	}

	if node.prev != nil {
		node.prev.next = node.next
	}
	if node.next != nil {
		node.next.prev = node.prev
	}
	if node == b.workspaceLRUHead {
		b.workspaceLRUHead = node.next
	}
	if node == b.workspaceLRUTail {
		b.workspaceLRUTail = node.prev
	}

	delete(b.workspaceLRUNodeMap, key)
}

func (b *Bot) evictLRU() {
	if b.workspaceLRUTail == nil {
		return
	}

	key := b.workspaceLRUTail.key
	delete(b.workspaceCache, key)
	b.removeLRUNode(key)
}

func (b *Bot) Run(ctx context.Context) error {
	authResp, err := b.client.AuthTestContext(ctx)
	if err != nil {
		return fmt.Errorf("slack auth test: %w", err)
	}
	b.botUserID = authResp.UserID
	b.cachedTeamID.Store(authResp.TeamID)

	go b.handleEvents(ctx)

	log.Printf("[slack] Bot online (Socket Mode) as %s", authResp.User)
	return b.socketClient.RunContext(ctx)
}

func (b *Bot) handleEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-b.socketClient.Events:
			if !ok {
				return
			}
			b.routeEvent(ctx, evt)
		}
	}
}

func (b *Bot) routeEvent(ctx context.Context, evt socketmode.Event) {
	switch evt.Type {
	case socketmode.EventTypeSlashCommand:
		cmd, ok := evt.Data.(slack.SlashCommand)
		if !ok {
			return
		}
		b.socketClient.Ack(*evt.Request)

		select {
		case b.handlerSemaphore <- struct{}{}:
			go func() {
				defer func() { <-b.handlerSemaphore }()
				b.handleSlashCommand(ctx, cmd)
			}()
		case <-ctx.Done():
			return
		}

	case socketmode.EventTypeEventsAPI:
		eventsAPIEvent, ok := evt.Data.(slackevents.EventsAPIEvent)
		if !ok {
			return
		}
		b.socketClient.Ack(*evt.Request)

		select {
		case b.handlerSemaphore <- struct{}{}:
			go func() {
				defer func() { <-b.handlerSemaphore }()
				b.handleEventsAPI(ctx, eventsAPIEvent)
			}()
		case <-ctx.Done():
			return
		}
	}
}

func (b *Bot) handleSlashCommand(ctx context.Context, cmd slack.SlashCommand) {
	text := strings.TrimSpace(cmd.Text)
	teamID := cmd.TeamID
	userID := cmd.UserID
	channelID := cmd.ChannelID

	if teamID == "" {
		return
	}

	if runes := []rune(text); len(runes) > MaxSlackMessageLength {
		text = string(runes[:MaxSlackMessageLength])
	}

	if !b.allowMessage(teamID, userID) {
		b.postMessage(ctx, channelID, "", "You're sending messages too quickly (limit: 20 per minute). Please wait a moment before trying again.")
		return
	}

	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	if b.workspaceChecker != nil {
		exists, err := b.checkWorkspaceCache(ctx, teamID)
		if err != nil {
			log.Printf("[slack] workspace check error for team %s: %v", teamID, err)
		}
		if !exists {
			b.postMessage(ctx, channelID, "", "This workspace isn't connected to Shipmates yet. Visit https://shipmates.dev/dashboard to set up your workspace.")
			return
		}
	}

	logFn := b.createLogFunc(channelID, "")

	parts := strings.Fields(text)
	if len(parts) == 0 {
		b.postMessage(ctx, channelID, "", formatHelp())
		return
	}

	subcommand := strings.ToLower(parts[0])

	switch subcommand {
	case "help":
		b.postMessage(ctx, channelID, "", formatHelp())
		return

	case "review":
		repo := strings.TrimSpace(strings.Join(parts[1:], " "))
		if repo == "" {
			b.postMessage(ctx, channelID, "", "Usage: `/shipmates review <owner/repo>`")
			return
		}
		if !isValidRepoFormat(repo) {
			b.postMessage(ctx, channelID, "", "Invalid repository format. Use `owner/repo`.")
			return
		}
		response, err := b.messageHandler.HandleMessage(ctx, "review the "+repo+" repository", teamID, userID, "slack", logFn, "")
		if err != nil {
			log.Printf("[slack] handle review error: %v", err)
			b.postMessage(ctx, channelID, "", "Something went wrong. Check https://shipmates.dev/dashboard/sessions for details.")
			return
		}
		if response != "" {
			b.postMessage(ctx, channelID, "", truncateMessage(response, MaxSlackSendLength))
		}

	case "ask":
		if len(parts) < 3 {
			b.postMessage(ctx, channelID, "", "Usage: `/shipmates ask <agent-name> <question>`")
			return
		}
		agentName := parts[1]
		if !isValidAgentName(agentName) {
			b.postMessage(ctx, channelID, "", "Invalid agent name. Use alphanumeric characters, hyphens, and underscores.")
			return
		}
		question := strings.TrimSpace(strings.Join(parts[2:], " "))
		response, err := b.messageHandler.HandleMessage(ctx, agentName+", "+question, teamID, userID, "slack", logFn, "")
		if err != nil {
			log.Printf("[slack] handle ask error: %v", err)
			b.postMessage(ctx, channelID, "", "Something went wrong. Check https://shipmates.dev/dashboard/sessions for details.")
			return
		}
		if response != "" {
			b.postMessage(ctx, channelID, "", truncateMessage(response, MaxSlackSendLength))
		}

	default:
		b.postMessage(ctx, channelID, "", formatHelp())
	}
}

func (b *Bot) handleEventsAPI(ctx context.Context, event slackevents.EventsAPIEvent) {
	switch event.Type {
	case slackevents.CallbackEvent:
		b.handleCallbackEvent(ctx, event)
	}
}

func (b *Bot) handleCallbackEvent(ctx context.Context, event slackevents.EventsAPIEvent) {
	innerEvent := event.InnerEvent

	switch ev := innerEvent.Data.(type) {
	case *slackevents.AppMentionEvent:
		b.handleAppMention(ctx, ev)
	case *slackevents.MessageEvent:
		b.handleMessageEvent(ctx, ev)
	}
}

func (b *Bot) handleAppMention(ctx context.Context, event *slackevents.AppMentionEvent) {
	text := slackMentionPattern.ReplaceAllString(event.Text, "")
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}

	if runes := []rune(text); len(runes) > MaxSlackMessageLength {
		text = string(runes[:MaxSlackMessageLength])
	}

	teamID := b.resolveTeamID(ctx)
	if teamID == "" {
		log.Printf("[slack] could not determine workspace ID for app_mention in channel %s", event.Channel)
		b.postMessage(ctx, event.Channel, event.TimeStamp, "Could not determine your workspace. Try reinstalling the Shipmates app from https://shipmates.dev/dashboard.")
		return
	}

	userID := event.User
	if userID == "" {
		return
	}

	if !b.allowMessage(teamID, userID) {
		b.postMessage(ctx, event.Channel, event.TimeStamp, "You're sending messages too quickly (limit: 20 per minute). Please wait a moment before trying again.")
		return
	}

	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	if b.workspaceChecker != nil {
		exists, err := b.checkWorkspaceCache(ctx, teamID)
		if err != nil {
			log.Printf("[slack] workspace check error for team %s: %v", teamID, err)
		}
		if !exists {
			b.postMessage(ctx, event.Channel, event.TimeStamp, "This workspace isn't connected to Shipmates yet. Visit https://shipmates.dev/dashboard to set up your workspace.")
			return
		}
	}

	logFn := b.createLogFunc(event.Channel, event.TimeStamp)

	response, err := b.messageHandler.HandleMessage(ctx, text, teamID, userID, "slack", logFn, event.TimeStamp)
	if err != nil {
		log.Printf("[slack] handle app_mention error: %v", err)
		b.postMessage(ctx, event.Channel, event.TimeStamp, "Something went wrong. Check https://shipmates.dev/dashboard/sessions for details.")
		return
	}

	if response != "" {
		b.postMessage(ctx, event.Channel, event.TimeStamp, truncateMessage(response, MaxSlackSendLength))
	}
}

func (b *Bot) handleMessageEvent(ctx context.Context, event *slackevents.MessageEvent) {
	if event.BotID != "" {
		return
	}
	if event.SubType != "" {
		return
	}
	if event.Text == "" {
		return
	}

	isDM := event.ChannelType == "im"
	isThreadReply := event.ThreadTimeStamp != ""

	if !isDM && !isThreadReply {
		return
	}

	teamID := b.resolveTeamID(ctx)
	if teamID == "" {
		log.Printf("[slack] could not determine workspace ID for message in channel %s", event.Channel)
		return
	}

	userID := event.User
	if userID == "" {
		return
	}

	threadTs := event.ThreadTimeStamp
	if threadTs == "" {
		threadTs = event.TimeStamp
	}

	if !b.allowMessage(teamID, userID) {
		b.postMessage(ctx, event.Channel, threadTs, "You're sending messages too quickly (limit: 20 per minute). Please wait a moment before trying again.")
		return
	}

	text := slackMentionPattern.ReplaceAllString(event.Text, "")
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}

	if runes := []rune(text); len(runes) > MaxSlackMessageLength {
		text = string(runes[:MaxSlackMessageLength])
	}

	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	if b.workspaceChecker != nil {
		exists, err := b.checkWorkspaceCache(ctx, teamID)
		if err != nil {
			log.Printf("[slack] workspace check error for team %s: %v", teamID, err)
		}
		if !exists {
			return
		}
	}

	logFn := b.createLogFunc(event.Channel, threadTs)

	response, err := b.messageHandler.HandleMessage(ctx, text, teamID, userID, "slack", logFn, threadTs)
	if err != nil {
		log.Printf("[slack] handle message error: %v", err)
		b.postMessage(ctx, event.Channel, threadTs, "Something went wrong. Check https://shipmates.dev/dashboard/sessions for details.")
		return
	}

	if response != "" {
		b.postMessage(ctx, event.Channel, threadTs, truncateMessage(response, MaxSlackSendLength))
	}
}

func (b *Bot) createLogFunc(channelID, threadTs string) LogFunc {
	return func(ctx context.Context, message string) (string, error) {
		truncated := truncateMessage(message, MaxSlackSendLength)
		opts := []slack.MsgOption{
			slack.MsgOptionText(truncated, false),
		}
		if threadTs != "" {
			opts = append(opts, slack.MsgOptionTS(threadTs))
		}

		_, ts, err := b.client.PostMessageContext(ctx, channelID, opts...)
		if err != nil {
			return "", fmt.Errorf("post log message: %w", err)
		}
		return ts, nil
	}
}

func (b *Bot) postMessage(ctx context.Context, channelID, threadTs, text string) {
	opts := []slack.MsgOption{
		slack.MsgOptionText(text, false),
	}
	if threadTs != "" {
		opts = append(opts, slack.MsgOptionTS(threadTs))
	}

	_, _, err := b.client.PostMessageContext(ctx, channelID, opts...)
	if err != nil {
		log.Printf("[slack] failed to post message: %v", err)
	}
}

func (b *Bot) resolveTeamID(ctx context.Context) string {
	if val := b.cachedTeamID.Load(); val != nil {
		if teamID, ok := val.(string); ok && teamID != "" {
			return teamID
		}
	}
	info, err := b.client.AuthTestContext(ctx)
	if err != nil {
		return ""
	}
	b.cachedTeamID.Store(info.TeamID)
	return info.TeamID
}

func (b *Bot) allowMessage(teamID, userID string) bool {
	key := teamID + ":" + userID
	nowNano := time.Now().UnixNano()

	b.rateLimitersMu.RLock()
	entry, exists := b.rateLimiters[key]
	b.rateLimitersMu.RUnlock()

	if exists {
		entry.lastUsed.Store(nowNano)
		return entry.limiter.Allow()
	}

	b.rateLimitersMu.Lock()
	entry, exists = b.rateLimiters[key]
	if !exists {
		entry = rateLimiterEntry{
			limiter: rate.NewLimiter(rate.Every(time.Minute/SlackRatePerUser), SlackBurstPerUser),
		}
		entry.lastUsed.Store(nowNano)
		b.rateLimiters[key] = entry
	} else {
		entry.lastUsed.Store(nowNano)
	}
	b.rateLimitersMu.Unlock()

	return entry.limiter.Allow()
}

func (b *Bot) checkWorkspaceCache(ctx context.Context, teamID string) (bool, error) {
	cacheKey := "slack:" + teamID
	now := time.Now()

	b.workspaceCacheMu.RLock()
	entry, found := b.workspaceCache[cacheKey]
	b.workspaceCacheMu.RUnlock()

	if found && now.Before(entry.expiresAt) {
		b.workspaceCacheMu.Lock()
		b.moveLRUToFront(cacheKey)
		b.workspaceCacheMu.Unlock()
		return entry.exists, nil
	}

	exists, err := b.workspaceChecker.WorkspaceExists(ctx, "slack", teamID)
	if err != nil {
		b.workspaceCacheMu.Lock()
		delete(b.workspaceCache, cacheKey)
		b.removeLRUNode(cacheKey)
		b.workspaceCacheMu.Unlock()
		return false, fmt.Errorf("check workspace: %w", err)
	}

	b.workspaceCacheMu.Lock()
	if existingEntry, alreadyInserted := b.workspaceCache[cacheKey]; alreadyInserted {
		if now.Before(existingEntry.expiresAt) {
			b.moveLRUToFront(cacheKey)
			b.workspaceCacheMu.Unlock()
			return existingEntry.exists, nil
		}
	}
	if len(b.workspaceCache) >= workspaceCacheMaxEntries {
		b.evictLRU()
	}
	b.workspaceCache[cacheKey] = workspaceCacheEntry{
		exists:    exists,
		expiresAt: now.Add(workspaceCacheTTL),
	}
	b.addLRUNode(cacheKey)
	b.workspaceCacheMu.Unlock()

	return exists, nil
}

func truncateMessage(message string, maxLength int) string {
	if len(message) <= maxLength {
		return message
	}
	suffix := "\n\n_Message truncated. Full output available in the dashboard._"
	runes := []rune(message)
	cutoff := maxLength - len(suffix)
	if cutoff < 0 {
		cutoff = 0
	}
	if len(runes) <= cutoff {
		return message
	}
	return string(runes[:cutoff]) + suffix
}

func formatHelp() string {
	return "*Shipmates* -- AI coding agents in your chat\n\n" +
		"*Commands:*\n" +
		"`/shipmates review <owner/repo>` -- Start a code review on a repository\n" +
		"    Example: `/shipmates review acme/backend`\n" +
		"`/shipmates ask <agent-name> <question>` -- Ask a specific agent a question\n" +
		"    Example: `/shipmates ask atlas refactor the auth middleware`\n" +
		"`/shipmates help` -- Show this help message\n\n" +
		"*You can also:*\n" +
		"- @mention me in any channel to start a conversation\n" +
		"- Send me a DM directly\n" +
		"- Reply in a thread to continue a conversation\n\n" +
		"*Get started:* https://shipmates.dev/dashboard"
}

func isValidRepoFormat(repo string) bool {
	parts := strings.Split(repo, "/")
	if len(parts) != 2 {
		return false
	}
	owner := parts[0]
	name := parts[1]
	if owner == "" || name == "" {
		return false
	}
	for _, ch := range owner + name {
		if !isAlphanumericOrHyphenDot(ch) {
			return false
		}
	}
	return true
}

func isValidAgentName(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for _, ch := range name {
		if !isAlphanumericOrHyphenUnderscore(ch) {
			return false
		}
	}
	return true
}

func isAlphanumericOrHyphenDot(ch rune) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '-' || ch == '.'
}

func isAlphanumericOrHyphenUnderscore(ch rune) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '-' || ch == '_'
}

func (b *Bot) Close() error {
	return nil
}
