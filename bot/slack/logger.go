package slack

import (
	"context"
	"fmt"
	"log"
	"sync"

	"github.com/slack-go/slack"
)

type Logger struct {
	client    *slack.Client
	channel   string
	threadTs  string
	mu        sync.RWMutex
}

func NewLogger(client *slack.Client) *Logger {
	return &Logger{
		client: client,
	}
}

func (l *Logger) SetContext(channel, threadTs string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.channel = channel
	l.threadTs = threadTs
}

func (l *Logger) PostLog(ctx context.Context, message string) (string, error) {
	l.mu.RLock()
	client := l.client
	channel := l.channel
	threadTs := l.threadTs
	l.mu.RUnlock()

	if client == nil || channel == "" || threadTs == "" {
		return "", nil
	}

	truncated := truncateMessage(message, MaxSlackSendLength)

	opts := []slack.MsgOption{
		slack.MsgOptionText(truncated, false),
		slack.MsgOptionTS(threadTs),
	}

	_, ts, err := client.PostMessageContext(ctx, channel, opts...)
	if err != nil {
		log.Printf("[slack-logger] failed to post: %v", err)
		return "", fmt.Errorf("post slack log: %w", err)
	}

	return ts, nil
}
