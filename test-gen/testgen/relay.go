package testgen

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type RelayClient struct {
	conn      *websocket.Conn
	sessionID string
	relayURL  string
	secret    string
	mu        sync.Mutex
	buffer    []RelayEvent
	maxBuffer int
}

func NewRelayClient(relayURL, secret, sessionID string) (*RelayClient, error) {
	if relayURL == "" {
		return nil, nil
	}

	client := &RelayClient{
		sessionID: sessionID,
		relayURL:  relayURL,
		secret:    secret,
		maxBuffer: 100,
		buffer:    make([]RelayEvent, 0),
	}

	if err := client.connect(); err != nil {
		return nil, err
	}

	return client, nil
}

func (rc *RelayClient) connect() error {
	u, err := url.Parse(rc.relayURL)
	if err != nil {
		return fmt.Errorf("invalid relay URL: %w", err)
	}

	if u.Scheme == "http" {
		u.Scheme = "ws"
	} else if u.Scheme == "https" {
		u.Scheme = "wss"
	}

	u.Path = "/ingest"
	query := u.Query()
	query.Set("sessionId", rc.sessionID)
	query.Set("secret", rc.secret)
	u.RawQuery = query.Encode()

	dialer := &websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	conn, _, err := dialer.Dial(u.String(), nil)
	if err != nil {
		return fmt.Errorf("failed to connect to relay: %w", err)
	}

	rc.conn = conn
	return nil
}

func (rc *RelayClient) EmitEvent(event RelayEvent) {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	data, err := json.Marshal(event)
	if err != nil {
		return
	}

	if rc.conn == nil {
		rc.bufferEvent(event)
		if err := rc.reconnect(); err == nil {
			rc.flushBuffer()
		}
		return
	}

	if err := rc.conn.WriteMessage(websocket.TextMessage, data); err != nil {
		rc.bufferEvent(event)
		rc.conn = nil
	}
}

func (rc *RelayClient) PushStatus(status string) {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	statusEvent := map[string]string{
		"type":    "status",
		"content": status,
	}

	data, err := json.Marshal(statusEvent)
	if err != nil {
		return
	}

	if rc.conn == nil {
		return
	}

	rc.conn.WriteMessage(websocket.TextMessage, data)
}

func (rc *RelayClient) bufferEvent(event RelayEvent) {
	if len(rc.buffer) >= rc.maxBuffer {
		rc.buffer = rc.buffer[1:]
	}
	rc.buffer = append(rc.buffer, event)
}

func (rc *RelayClient) flushBuffer() {
	if rc.conn == nil {
		return
	}

	for _, event := range rc.buffer {
		data, err := json.Marshal(event)
		if err != nil {
			continue
		}
		rc.conn.WriteMessage(websocket.TextMessage, data)
	}
	rc.buffer = rc.buffer[:0]
}

func (rc *RelayClient) reconnect() error {
	if rc.conn != nil {
		rc.conn.Close()
		rc.conn = nil
	}

	if err := rc.connect(); err != nil {
		return err
	}

	return nil
}

func (rc *RelayClient) Close() {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	if rc.conn != nil {
		rc.conn.Close()
		rc.conn = nil
	}
}
