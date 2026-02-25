package scheduler

import (
	"container/list"
	"context"
	"fmt"
	"os"
	"regexp"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/slack-go/slack"
	goshared "github.com/KranzL/shipmates-oss/go-shared"
	"golang.org/x/time/rate"
)

const (
	maxCacheSize            = 500
	cacheEvictionIntervalMs = 10 * 60 * 1000
	slackCacheTTLMs         = 5 * 60 * 1000
	workspaceCacheTTLMs     = 30_000
)

type ScheduleResult struct {
	ScheduleName string
	AgentName    string
	Task         string
	Status       string
	DurationMs   int64
	BranchName   string
	SessionID    string
}

type slackChannelEntry struct {
	channelID string
	botToken  string
	expiresAt time.Time
	element   *list.Element
}

type workspaceCacheEntry struct {
	workspaces []goshared.Workspace
	expiresAt  time.Time
	element    *list.Element
}

var (
	discordClientInstance *discordgo.Session
	discordClientMu       sync.RWMutex

	slackChannelCache    = make(map[string]slackChannelEntry)
	slackChannelCacheMu  sync.RWMutex
	slackChannelLRUList  = list.New()
	slackChannelLRUMu    sync.Mutex

	workspaceCache      = make(map[string]workspaceCacheEntry)
	workspaceCacheMu    sync.RWMutex
	workspaceLRUList    = list.New()
	workspaceLRUMu      sync.Mutex

	mentionPattern       = regexp.MustCompile(`@(everyone|here)`)
	discordEscapePattern = regexp.MustCompile(`[*_` + "`" + `~|\\]`)
	slackMentionPattern  = regexp.MustCompile(`<!(?:everyone|here|channel)>`)
	slackEscapePattern   = regexp.MustCompile(`[*_~` + "`" + `]`)

	discordLimiter = rate.NewLimiter(rate.Every(time.Second), 5)
	slackLimiter   = rate.NewLimiter(rate.Every(time.Second), 1)

	stopEviction     chan struct{}
	stopEvictionOnce sync.Once
)

func InitScheduleNotifier(client *discordgo.Session) {
	discordClientMu.Lock()
	discordClientInstance = client
	discordClientMu.Unlock()

	stopEvictionOnce.Do(func() {
		stopEviction = make(chan struct{})
		go func() {
			ticker := time.NewTicker(time.Duration(cacheEvictionIntervalMs) * time.Millisecond)
			defer ticker.Stop()

			for {
				select {
				case <-ticker.C:
					evictExpiredEntries()
				case <-stopEviction:
					return
				}
			}
		}()
	})
}

func ShutdownScheduleNotifier() {
	if stopEviction != nil {
		close(stopEviction)
	}
}

func evictExpiredEntries() {
	now := time.Now()

	slackChannelCacheMu.Lock()
	slackChannelLRUMu.Lock()
	for key, value := range slackChannelCache {
		if value.expiresAt.Before(now) {
			delete(slackChannelCache, key)
			if value.element != nil {
				slackChannelLRUList.Remove(value.element)
			}
		}
	}
	slackChannelLRUMu.Unlock()
	slackChannelCacheMu.Unlock()

	workspaceCacheMu.Lock()
	workspaceLRUMu.Lock()
	for key, value := range workspaceCache {
		if value.expiresAt.Before(now) {
			delete(workspaceCache, key)
			if value.element != nil {
				workspaceLRUList.Remove(value.element)
			}
		}
	}
	workspaceLRUMu.Unlock()
	workspaceCacheMu.Unlock()
}

func escapeDiscordMarkdown(text string) string {
	text = mentionPattern.ReplaceAllString(text, "@\u200B$1")
	text = discordEscapePattern.ReplaceAllStringFunc(text, func(c string) string {
		return "\\" + c
	})
	if len(text) > 200 {
		text = text[:200]
	}
	return text
}

func escapeSlackMarkdown(text string) string {
	text = slackMentionPattern.ReplaceAllString(text, "(mention)")
	text = slackEscapePattern.ReplaceAllString(text, "")
	if len(text) > 200 {
		text = text[:200]
	}
	return text
}

func fetchWorkspaces(ctx context.Context, db *goshared.DB, userID string) ([]goshared.Workspace, error) {
	workspaceCacheMu.RLock()
	cached, exists := workspaceCache[userID]
	workspaceCacheMu.RUnlock()

	if exists && cached.expiresAt.After(time.Now()) {
		workspaceLRUMu.Lock()
		workspaceLRUList.MoveToFront(cached.element)
		workspaceLRUMu.Unlock()
		return cached.workspaces, nil
	}

	workspaces, err := db.ListWorkspaces(ctx, userID)
	if err != nil {
		return nil, err
	}

	workspaceCacheMu.Lock()
	workspaceLRUMu.Lock()
	if len(workspaceCache) >= maxCacheSize {
		back := workspaceLRUList.Back()
		if back != nil {
			oldKey := back.Value.(string)
			delete(workspaceCache, oldKey)
			workspaceLRUList.Remove(back)
		}
	}
	elem := workspaceLRUList.PushFront(userID)
	workspaceCache[userID] = workspaceCacheEntry{
		workspaces: workspaces,
		expiresAt:  time.Now().Add(time.Duration(workspaceCacheTTLMs) * time.Millisecond),
		element:    elem,
	}
	workspaceLRUMu.Unlock()
	workspaceCacheMu.Unlock()

	return workspaces, nil
}

func notifyScheduleResult(ctx context.Context, db *goshared.DB, userID string, result ScheduleResult) {
	workspaces, err := fetchWorkspaces(ctx, db, userID)
	if err != nil {
		fmt.Printf("[scheduler] Failed to fetch workspaces for user %s: %v\n", userID, err)
		return
	}

	durationSeconds := result.DurationMs / 1000
	minutes := durationSeconds / 60
	seconds := durationSeconds % 60

	durationStr := ""
	if minutes > 0 {
		durationStr = fmt.Sprintf("%dm %ds", minutes, seconds)
	} else {
		durationStr = fmt.Sprintf("%ds", seconds)
	}

	statusLabel := "Completed"
	if result.Status == "error" {
		statusLabel = "Failed"
	} else if result.Status == "disabled" {
		statusLabel = "Auto-Disabled After 5 Failures"
	}

	sessionURL := ""
	if result.SessionID != "" {
		appBaseURL := os.Getenv("NEXTAUTH_URL")
		if appBaseURL == "" {
			appBaseURL = "https://app.shipmates.dev"
		}
		sessionURL = fmt.Sprintf("%s/dashboard/sessions/%s", appBaseURL, result.SessionID)
	}

	for _, workspace := range workspaces {
		if workspace.Platform == goshared.PlatformDiscord {
			safeName := escapeDiscordMarkdown(result.ScheduleName)
			safeAgent := escapeDiscordMarkdown(result.AgentName)
			truncatedTask := result.Task
			if len(truncatedTask) > 150 {
				truncatedTask = truncatedTask[:150]
			}
			safeTask := escapeDiscordMarkdown(truncatedTask)
			branchLine := ""
			if result.BranchName != "" {
				safeBranch := escapeDiscordMarkdown(result.BranchName)
				branchLine = fmt.Sprintf("\nBranch: `%s`", safeBranch)
			}

			viewLine := ""
			if sessionURL != "" {
				viewLine = fmt.Sprintf("\nView: %s", sessionURL)
			}

			durationLine := ""
			if result.DurationMs > 0 {
				durationLine = fmt.Sprintf("\nDuration: %s", durationStr)
			}

			message := fmt.Sprintf(
				"**Scheduled Task %s**\nSchedule: %s\nAgent: %s\nTask: %s%s%s%s",
				statusLabel, safeName, safeAgent, safeTask, durationLine, branchLine, viewLine,
			)

			if err := sendDiscordNotification(ctx, workspace.PlatformID, message); err != nil {
				fmt.Printf("[scheduler] Failed to send Discord notification: %v\n", err)
			}
		} else if workspace.Platform == goshared.PlatformSlack {
			safeName := escapeSlackMarkdown(result.ScheduleName)
			safeAgent := escapeSlackMarkdown(result.AgentName)
			truncatedTask := result.Task
			if len(truncatedTask) > 150 {
				truncatedTask = truncatedTask[:150]
			}
			safeTask := escapeSlackMarkdown(truncatedTask)
			branchLine := ""
			if result.BranchName != "" {
				safeBranch := escapeSlackMarkdown(result.BranchName)
				branchLine = fmt.Sprintf("\nBranch: `%s`", safeBranch)
			}

			viewLine := ""
			if sessionURL != "" {
				viewLine = fmt.Sprintf("\nView: %s", sessionURL)
			}

			durationLine := ""
			if result.DurationMs > 0 {
				durationLine = fmt.Sprintf("\nDuration: %s", durationStr)
			}

			message := fmt.Sprintf(
				"*Scheduled Task %s*\nSchedule: %s\nAgent: %s\nTask: %s%s%s%s",
				statusLabel, safeName, safeAgent, safeTask, durationLine, branchLine, viewLine,
			)

			if err := sendSlackNotification(ctx, db, workspace.PlatformID, message); err != nil {
				fmt.Printf("[scheduler] Failed to send Slack notification: %v\n", err)
			}
		}
	}
}

func sendDiscordNotification(ctx context.Context, guildID, message string) error {
	if err := discordLimiter.Wait(ctx); err != nil {
		return fmt.Errorf("rate limit wait: %w", err)
	}

	discordClientMu.RLock()
	client := discordClientInstance
	discordClientMu.RUnlock()

	if client == nil {
		return fmt.Errorf("discord client not initialized")
	}

	if client.State == nil || client.State.User == nil {
		return fmt.Errorf("discord client state not initialized")
	}

	guild, err := client.Guild(guildID)
	if err != nil {
		return fmt.Errorf("get guild: %w", err)
	}

	var targetChannelID string

	if guild.SystemChannelID != "" {
		targetChannelID = guild.SystemChannelID
	} else {
		channels, err := client.GuildChannels(guildID)
		if err != nil {
			return fmt.Errorf("get guild channels: %w", err)
		}

		for _, ch := range channels {
			if ch.Type == discordgo.ChannelTypeGuildText && ch.Name == "shipmates" {
				targetChannelID = ch.ID
				break
			}
		}

		if targetChannelID == "" {
			for _, ch := range channels {
				if ch.Type == discordgo.ChannelTypeGuildText {
					perms, err := client.UserChannelPermissions(client.State.User.ID, ch.ID)
					if err == nil && (perms&discordgo.PermissionSendMessages) != 0 {
						targetChannelID = ch.ID
						break
					}
				}
			}
		}
	}

	if targetChannelID == "" {
		return fmt.Errorf("no writable text channel found in guild %s", guildID)
	}

	truncated := message
	if len(truncated) > 1900 {
		truncated = truncated[:1900] + "..."
	}

	_, err = client.ChannelMessageSend(targetChannelID, truncated)
	if err != nil {
		return fmt.Errorf("send message: %w", err)
	}

	return nil
}

func resolveSlackChannel(ctx context.Context, db *goshared.DB, teamID string) (*slackChannelEntry, error) {
	slackChannelCacheMu.RLock()
	cached, exists := slackChannelCache[teamID]
	slackChannelCacheMu.RUnlock()

	if exists && cached.expiresAt.After(time.Now()) {
		slackChannelLRUMu.Lock()
		slackChannelLRUList.MoveToFront(cached.element)
		slackChannelLRUMu.Unlock()
		return &cached, nil
	}

	resolveCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	workspace, err := db.GetWorkspaceByPlatform(resolveCtx, goshared.PlatformSlack, teamID)
	if err != nil {
		return nil, fmt.Errorf("get workspace: %w", err)
	}
	if workspace == nil || workspace.BotToken == nil {
		return nil, fmt.Errorf("workspace or bot token not found")
	}

	client := slack.New(*workspace.BotToken)
	result, _, err := client.GetConversationsContext(resolveCtx, &slack.GetConversationsParameters{
		Types: []string{"public_channel"},
		Limit: 100,
	})
	if err != nil {
		return nil, fmt.Errorf("get conversations: %w", err)
	}

	var targetChannelID string
	for _, ch := range result {
		if ch.Name == "shipmates" {
			targetChannelID = ch.ID
			break
		}
	}

	if targetChannelID == "" {
		for _, ch := range result {
			if ch.Name == "general" {
				targetChannelID = ch.ID
				break
			}
		}
	}

	if targetChannelID == "" && len(result) > 0 {
		targetChannelID = result[0].ID
	}

	if targetChannelID == "" {
		return nil, fmt.Errorf("no suitable channel found")
	}

	slackChannelCacheMu.Lock()
	slackChannelLRUMu.Lock()
	if len(slackChannelCache) >= maxCacheSize {
		back := slackChannelLRUList.Back()
		if back != nil {
			oldKey := back.Value.(string)
			delete(slackChannelCache, oldKey)
			slackChannelLRUList.Remove(back)
		}
	}
	elem := slackChannelLRUList.PushFront(teamID)
	entry := slackChannelEntry{
		channelID: targetChannelID,
		botToken:  *workspace.BotToken,
		expiresAt: time.Now().Add(time.Duration(slackCacheTTLMs) * time.Millisecond),
		element:   elem,
	}
	slackChannelCache[teamID] = entry
	slackChannelLRUMu.Unlock()
	slackChannelCacheMu.Unlock()

	return &entry, nil
}

func sendSlackNotification(ctx context.Context, db *goshared.DB, teamID, message string) error {
	if err := slackLimiter.Wait(ctx); err != nil {
		return fmt.Errorf("rate limit wait: %w", err)
	}

	resolved, err := resolveSlackChannel(ctx, db, teamID)
	if err != nil {
		return err
	}

	sendCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	client := slack.New(resolved.botToken)
	_, _, err = client.PostMessageContext(sendCtx, resolved.channelID, slack.MsgOptionText(message, false))
	if err != nil {
		return fmt.Errorf("post message: %w", err)
	}

	return nil
}
