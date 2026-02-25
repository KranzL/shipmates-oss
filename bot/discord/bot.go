package discord

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bwmarrin/discordgo"
	"golang.org/x/time/rate"
)

const (
	maxDiscordMessageLength    = 1900
	messageRatePerUser         = 20
	messageBurstPerUser        = 5
	workspaceCacheTTL          = 60 * time.Second
	typingTickInterval         = 9 * time.Second
	messageHandleTimeout       = 5 * time.Minute
	joinTimeout                = 30 * time.Second
	clarificationThreadTTL     = 60
	inputLengthLimit           = 4000
	rateLimiterEvictAfter      = 30 * time.Minute
	rateLimiterCleanupInterval = 10 * time.Minute
	workspaceCacheMaxEntries   = 10000
)

var mentionPattern = regexp.MustCompile(`<@!?\d+>`)

type LogFunc func(ctx context.Context, message string) (string, error)

type MessageHandler interface {
	HandleMessage(ctx context.Context, text, workspaceID, userID, platform string, logFn LogFunc, threadID string) (string, error)
}

type SpeechHandler interface {
	HandleSpeech(ctx context.Context, audioData []byte, guildID, userID string) ([]byte, error)
}

type WorkspaceChecker interface {
	WorkspaceExists(ctx context.Context, platform, platformID string) (bool, error)
}

type rateLimiterEntry struct {
	limiter  *rate.Limiter
	lastUsed time.Time
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
	session          *discordgo.Session
	messageHandler   MessageHandler
	speechHandler    SpeechHandler
	workspaceChecker WorkspaceChecker
	voiceManager     *VoiceManager
	botUserID        string
	ctx              context.Context
	cancel           context.CancelFunc

	rateLimiters   map[string]rateLimiterEntry
	rateLimitersMu sync.Mutex

	workspaceCache       map[string]workspaceCacheEntry
	workspaceCacheMu     sync.RWMutex
	workspaceLRUHead     *lruNode
	workspaceLRUTail     *lruNode
	workspaceLRUNodeMap  map[string]*lruNode
	shuttingDown         atomic.Bool
}

type BotConfig struct {
	Token            string
	ClientID         string
	MessageHandler   MessageHandler
	SpeechHandler    SpeechHandler
	WorkspaceChecker WorkspaceChecker
}

func NewBot(ctx context.Context, cfg BotConfig) (*Bot, error) {
	if cfg.Token == "" {
		return nil, fmt.Errorf("discord token is required")
	}
	if cfg.ClientID == "" {
		return nil, fmt.Errorf("discord client ID is required")
	}
	if cfg.MessageHandler == nil {
		return nil, fmt.Errorf("message handler is required")
	}

	session, err := discordgo.New("Bot " + cfg.Token)
	if err != nil {
		return nil, fmt.Errorf("create discord session: %w", err)
	}

	session.Identify.Intents = discordgo.IntentsGuilds |
		discordgo.IntentsGuildVoiceStates |
		discordgo.IntentsGuildMessages |
		discordgo.IntentsMessageContent

	botCtx, cancel := context.WithCancel(ctx)

	bot := &Bot{
		session:             session,
		messageHandler:      cfg.MessageHandler,
		speechHandler:       cfg.SpeechHandler,
		workspaceChecker:    cfg.WorkspaceChecker,
		voiceManager:        NewVoiceManager(botCtx),
		ctx:                 botCtx,
		cancel:              cancel,
		rateLimiters:        make(map[string]rateLimiterEntry),
		workspaceCache:      make(map[string]workspaceCacheEntry),
		workspaceLRUNodeMap: make(map[string]*lruNode),
	}

	go bot.cleanupEvictionLoop(botCtx)

	session.AddHandler(bot.handleReady)
	session.AddHandler(bot.handleInteractionCreate)
	session.AddHandler(bot.handleMessageCreate)

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
	now := time.Now()
	b.rateLimitersMu.Lock()
	for key, entry := range b.rateLimiters {
		if now.Sub(entry.lastUsed) > rateLimiterEvictAfter {
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

func (b *Bot) Open() error {
	return b.session.Open()
}

func (b *Bot) Close() error {
	if b.cancel != nil {
		b.cancel()
	}
	if b.voiceManager != nil {
		b.voiceManager.Close()
		b.voiceManager.DisconnectAll()
	}
	return b.session.Close()
}

func (b *Bot) SetShuttingDown() {
	b.shuttingDown.Store(true)
}

func (b *Bot) IsShuttingDown() bool {
	return b.shuttingDown.Load()
}

func (b *Bot) Session() *discordgo.Session {
	return b.session
}

func (b *Bot) RegisterCommands() error {
	commands := []*discordgo.ApplicationCommand{
		{
			Name:        "join",
			Description: "Join your voice channel so agents can listen and respond",
		},
		{
			Name:        "leave",
			Description: "Disconnect the bot from the voice channel",
		},
		{
			Name:        "help",
			Description: "Show available commands and usage",
		},
	}

	for _, cmd := range commands {
		_, err := b.session.ApplicationCommandCreate(b.session.State.User.ID, "", cmd)
		if err != nil {
			return fmt.Errorf("register command %s: %w", cmd.Name, err)
		}
	}

	return nil
}

func (b *Bot) handleReady(_ *discordgo.Session, r *discordgo.Ready) {
	b.botUserID = r.User.ID
	log.Printf("[discord] Bot ready as %s", r.User.Username)
}

func (b *Bot) handleInteractionCreate(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if b.IsShuttingDown() {
		return
	}

	if i.Type != discordgo.InteractionApplicationCommand {
		return
	}

	switch i.ApplicationCommandData().Name {
	case "join":
		b.handleJoinCommand(s, i)
	case "leave":
		b.handleLeaveCommand(s, i)
	case "help":
		b.handleHelpCommand(s, i)
	}
}

func (b *Bot) handleJoinCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if b.IsShuttingDown() {
		respondEphemeral(s, i, "Bot is restarting. Please try again in a moment.")
		return
	}

	if i.GuildID == "" {
		respondEphemeral(s, i, "This command can only be used in a server.")
		return
	}

	guild, err := s.State.Guild(i.GuildID)
	if err != nil {
		respondEphemeral(s, i, "Could not access server information. Try again in a moment.")
		return
	}

	var voiceChannelID string
	var voiceChannelName string
	for _, vs := range guild.VoiceStates {
		if vs.UserID == i.Member.User.ID {
			voiceChannelID = vs.ChannelID
			channel, chErr := s.Channel(vs.ChannelID)
			if chErr == nil {
				voiceChannelName = channel.Name
			}
			break
		}
	}

	if voiceChannelID == "" {
		respondEphemeral(s, i, "You need to be in a voice channel first.")
		return
	}

	permissions, permErr := s.State.UserChannelPermissions(s.State.User.ID, voiceChannelID)
	if permErr == nil {
		missingConnect := permissions&discordgo.PermissionVoiceConnect == 0
		missingSpeak := permissions&discordgo.PermissionVoiceSpeak == 0
		if missingConnect || missingSpeak {
			respondEphemeral(s, i, "I need Connect and Speak permissions in that voice channel. Ask a server admin to update my role.")
			return
		}
	}

	if b.workspaceChecker != nil {
		ctx, cancel := context.WithTimeout(context.Background(), joinTimeout)
		defer cancel()

		exists, checkErr := b.checkWorkspaceCache(ctx, i.GuildID)
		if checkErr != nil {
			log.Printf("[discord] workspace check error for guild %s: %v", i.GuildID, checkErr)
		}
		if !exists {
			respondEphemeral(s, i, "No workspace is linked to this server. Connect one at https://shipmates.dev/dashboard.")
			return
		}
	}

	err = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	})
	if err != nil {
		log.Printf("[discord] failed to defer reply: %v", err)
		return
	}

	voiceConn, joinErr := s.ChannelVoiceJoin(i.GuildID, voiceChannelID, false, false)
	if joinErr != nil {
		editInteractionResponse(s, i, "Could not join the voice channel. Make sure I have the correct permissions and try again.")
		return
	}

	b.voiceManager.Register(i.GuildID, voiceConn)

	if b.speechHandler != nil {
		go b.voiceManager.ListenForSpeech(b.ctx, i.GuildID, b.speechHandler)
	}

	editInteractionResponse(s, i, fmt.Sprintf("Joined **%s**. Listening for your agents.", voiceChannelName))
}

func (b *Bot) handleLeaveCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if b.IsShuttingDown() {
		respondEphemeral(s, i, "Bot is restarting. Please try again in a moment.")
		return
	}

	if i.GuildID == "" {
		respondEphemeral(s, i, "This command can only be used in a server.")
		return
	}

	if _, connected := b.voiceManager.GetConnection(i.GuildID); !connected {
		respondEphemeral(s, i, "I'm not in a voice channel.")
		return
	}

	b.voiceManager.Disconnect(i.GuildID)

	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: "Left the voice channel.",
		},
	})
	if err != nil {
		log.Printf("[discord] respond to leave command: %v", err)
	}
}

func (b *Bot) handleHelpCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if b.IsShuttingDown() {
		respondEphemeral(s, i, "Bot is restarting. Please try again in a moment.")
		return
	}

	respondEphemeral(s, i, formatHelpMessage())
}

func formatHelpMessage() string {
	return "**Shipmates** -- AI coding agents in your voice and text channels\n\n" +
		"**Commands:**\n" +
		"`/join` -- Join your voice channel\n" +
		"`/leave` -- Leave the voice channel\n" +
		"`/help` -- Show this help message\n\n" +
		"**Chat:**\n" +
		"@Shipmates <message> -- Talk to your agents\n" +
		"Reply in a thread to continue a conversation"
}

func (b *Bot) handleMessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if b.IsShuttingDown() {
		return
	}

	if m.Author == nil || m.Author.Bot || m.GuildID == "" || b.botUserID == "" {
		return
	}

	isThread := isThreadChannel(s, m.ChannelID)
	threadID := ""
	if isThread {
		threadID = m.ChannelID
	}

	isMentioned := false
	for _, mention := range m.Mentions {
		if mention.ID == b.botUserID {
			isMentioned = true
			break
		}
	}

	if !isThread && !isMentioned {
		return
	}

	text := mentionPattern.ReplaceAllString(m.Content, "")
	text = strings.TrimSpace(text)
	if text == "" {
		if isMentioned {
			sendTruncated(s, m.ChannelID, formatHelpMessage())
		}
		return
	}

	if runes := []rune(text); len(runes) > inputLengthLimit {
		text = string(runes[:inputLengthLimit])
	}

	if !b.allowMessage(m.GuildID, m.Author.ID) {
		sendTruncated(s, m.ChannelID, fmt.Sprintf("<@%s> You're sending messages too quickly (limit: 20 per minute). Please wait a moment before trying again.", m.Author.ID))
		return
	}

	if b.workspaceChecker != nil {
		ctx, cancel := context.WithTimeout(context.Background(), joinTimeout)
		exists, checkErr := b.checkWorkspaceCache(ctx, m.GuildID)
		cancel()
		if checkErr != nil {
			log.Printf("[discord] workspace check error for guild %s: %v", m.GuildID, checkErr)
		}
		if !exists {
			sendTruncated(s, m.ChannelID, "This server isn't connected to Shipmates yet. Visit https://shipmates.dev/dashboard to set up your workspace.")
			return
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), messageHandleTimeout)
	stopTyping := startTypingIndicator(ctx, s, m.ChannelID)

	logFn := b.createLogFunc(s, m.ChannelID)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[discord] message handler panic: %v", r)
				stopTyping()
			}
		}()
		defer cancel()
		defer stopTyping()

		response, err := b.messageHandler.HandleMessage(ctx, text, m.GuildID, m.Author.ID, "discord", logFn, threadID)
		if err != nil {
			log.Printf("[discord] message handling error: %v", err)
			sendTruncated(s, m.ChannelID, "Something went wrong processing your message. Try again, or check the dashboard for details.")
			return
		}

		if response != "" {
			sendTruncated(s, m.ChannelID, response)
		}
	}()
}

func (b *Bot) createLogFunc(s *discordgo.Session, channelID string) LogFunc {
	return func(ctx context.Context, message string) (string, error) {
		truncated := truncateMessage(message, maxDiscordMessageLength)

		msg, err := s.ChannelMessageSend(channelID, truncated)
		if err != nil {
			return "", fmt.Errorf("send log message: %w", err)
		}

		if strings.Contains(message, "Agent has a question:") {
			thread, threadErr := s.MessageThreadStartComplex(channelID, msg.ID, &discordgo.ThreadStart{
				Name:                "Agent Question",
				AutoArchiveDuration: clarificationThreadTTL,
			})
			if threadErr != nil {
				log.Printf("[discord] create clarification thread: %v", threadErr)
				_, fallbackErr := s.ChannelMessageSend(channelID, "Reply to the message above to answer the agent's question. Type `skip` to let the agent decide.")
				if fallbackErr != nil {
					log.Printf("[discord] send clarification fallback: %v", fallbackErr)
				}
				return msg.ID, nil
			}

			_, guideErr := s.ChannelMessageSend(thread.ID, "Reply in this thread to answer the agent's question. Type `skip` to let the agent decide.")
			if guideErr != nil {
				log.Printf("[discord] send thread guide: %v", guideErr)
			}
			return thread.ID, nil
		}

		return msg.ID, nil
	}
}

func (b *Bot) allowMessage(guildID, userID string) bool {
	key := guildID + ":" + userID
	now := time.Now()

	b.rateLimitersMu.Lock()
	entry, exists := b.rateLimiters[key]
	if !exists {
		entry = rateLimiterEntry{
			limiter: rate.NewLimiter(rate.Every(time.Minute/messageRatePerUser), messageBurstPerUser),
		}
	}
	entry.lastUsed = now
	b.rateLimiters[key] = entry
	b.rateLimitersMu.Unlock()

	return entry.limiter.Allow()
}

func (b *Bot) checkWorkspaceCache(ctx context.Context, guildID string) (bool, error) {
	cacheKey := "discord:" + guildID
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

	exists, err := b.workspaceChecker.WorkspaceExists(ctx, "discord", guildID)
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

func respondEphemeral(s *discordgo.Session, i *discordgo.InteractionCreate, content string) {
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: content,
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
	if err != nil {
		log.Printf("[discord] send ephemeral response: %v", err)
	}
}

func editInteractionResponse(s *discordgo.Session, i *discordgo.InteractionCreate, content string) {
	_, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Content: &content,
	})
	if err != nil {
		log.Printf("[discord] edit interaction response: %v", err)
	}
}

func startTypingIndicator(ctx context.Context, s *discordgo.Session, channelID string) func() {
	done := make(chan struct{})
	var once sync.Once

	go func() {
		_ = s.ChannelTyping(channelID)

		ticker := time.NewTicker(typingTickInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case <-ticker.C:
				_ = s.ChannelTyping(channelID)
			}
		}
	}()

	return func() {
		once.Do(func() {
			close(done)
		})
	}
}

func isThreadChannel(s *discordgo.Session, channelID string) bool {
	channel, err := s.Channel(channelID)
	if err != nil {
		return false
	}
	return channel.IsThread()
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

func sendTruncated(s *discordgo.Session, channelID, message string) {
	truncated := truncateMessage(message, maxDiscordMessageLength)
	_, err := s.ChannelMessageSend(channelID, truncated)
	if err != nil {
		log.Printf("[discord] send message: %v", err)
	}
}
