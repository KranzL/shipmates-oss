package discord

import (
	"context"
	"fmt"
	"log"
	"sync"

	"github.com/bwmarrin/discordgo"
)

type Logger struct {
	session      *discordgo.Session
	channelID    string
	mu           sync.RWMutex
}

func NewLogger(session *discordgo.Session) *Logger {
	return &Logger{
		session: session,
	}
}

func (l *Logger) SetChannel(channelID string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.channelID = channelID
}

func (l *Logger) PostLog(ctx context.Context, message string) (string, error) {
	l.mu.RLock()
	session := l.session
	channelID := l.channelID
	l.mu.RUnlock()

	if session == nil || channelID == "" {
		return "", nil
	}

	truncated := truncateMessage(message, maxDiscordMessageLength)

	msg, err := session.ChannelMessageSend(channelID, truncated)
	if err != nil {
		log.Printf("[discord-logger] failed to post: %v", err)
		return "", fmt.Errorf("post log message: %w", err)
	}

	return msg.ID, nil
}
