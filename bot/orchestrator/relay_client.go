package orchestrator

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	maxBufferSize = 5 * 1024 * 1024
	flushInterval = 16 * time.Millisecond
)

type RelayConnection struct {
	ws             *websocket.Conn
	mu             sync.Mutex
	writeMu        sync.Mutex
	chunks         []string
	chunksTotalLen int
	flushTimer     *time.Timer
	closed         bool
	sessionID      string
	relayURL       string
	relaySecret    string
}

func NewRelayConnection(sessionID, relayURL, relaySecret string) *RelayConnection {
	wsURL := strings.Replace(relayURL, "http://", "ws://", 1)
	wsURL = strings.Replace(wsURL, "https://", "wss://", 1)
	wsURL = fmt.Sprintf("%s/ingest?sessionId=%s", wsURL, sessionID)

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	headers := http.Header{}
	headers.Set("X-Relay-Secret", relaySecret)

	conn, _, err := dialer.Dial(wsURL, headers)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[relay] failed to connect for %s: %v\n", sessionID, err)
		return &RelayConnection{
			sessionID:   sessionID,
			relayURL:    relayURL,
			relaySecret: relaySecret,
			closed:      true,
		}
	}

	return &RelayConnection{
		ws:          conn,
		sessionID:   sessionID,
		relayURL:    relayURL,
		relaySecret: relaySecret,
		chunks:      make([]string, 0),
	}
}

func (rc *RelayConnection) Push(content string) {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	if rc.closed {
		return
	}

	rc.chunks = append(rc.chunks, content)
	rc.chunksTotalLen += len(content)

	if rc.chunksTotalLen > maxBufferSize {
		joined := strings.Join(rc.chunks, "")
		if len(joined) > maxBufferSize {
			joined = joined[len(joined)-maxBufferSize:]
		}
		rc.chunks = []string{joined}
		rc.chunksTotalLen = len(joined)
	}

	if rc.ws != nil {
		if rc.flushTimer != nil {
			rc.flushTimer.Stop()
		}
		rc.flushTimer = time.AfterFunc(flushInterval, func() {
			rc.flush()
		})
	}
}

func (rc *RelayConnection) flush() {
	rc.mu.Lock()
	rc.flushTimer = nil

	if rc.closed || len(rc.chunks) == 0 {
		rc.mu.Unlock()
		return
	}

	data := strings.Join(rc.chunks, "")
	rc.chunks = rc.chunks[:0]
	rc.chunksTotalLen = 0
	rc.mu.Unlock()

	rc.writeMu.Lock()
	defer rc.writeMu.Unlock()
	if rc.ws != nil {
		if err := rc.ws.WriteMessage(websocket.TextMessage, []byte(data)); err != nil {
			fmt.Fprintf(os.Stderr, "[relay] write error for %s: %v\n", rc.sessionID, err)
		}
	}
}

func (rc *RelayConnection) PushStatus(status string) {
	rc.mu.Lock()
	if rc.closed {
		rc.mu.Unlock()
		return
	}
	hasWS := rc.ws != nil
	rc.mu.Unlock()

	payload := map[string]string{
		"type":    "status",
		"content": status,
	}
	data, _ := json.Marshal(payload)

	if hasWS {
		rc.writeMu.Lock()
		err := rc.ws.WriteMessage(websocket.TextMessage, data)
		rc.writeMu.Unlock()
		if err != nil {
			pushStatusHTTP(rc.sessionID, rc.relayURL, rc.relaySecret, status)
		}
	} else {
		pushStatusHTTP(rc.sessionID, rc.relayURL, rc.relaySecret, status)
	}
}

func (rc *RelayConnection) Close() {
	rc.mu.Lock()
	if rc.closed {
		rc.mu.Unlock()
		return
	}
	rc.closed = true

	if rc.flushTimer != nil {
		rc.flushTimer.Stop()
		rc.flushTimer = nil
	}

	data := ""
	if len(rc.chunks) > 0 {
		data = strings.Join(rc.chunks, "")
		rc.chunks = rc.chunks[:0]
		rc.chunksTotalLen = 0
	}
	rc.mu.Unlock()

	if data != "" {
		rc.writeMu.Lock()
		if rc.ws != nil {
			rc.ws.WriteMessage(websocket.TextMessage, []byte(data))
		}
		rc.writeMu.Unlock()
	}

	endSessionHTTP(rc.sessionID, rc.relayURL, rc.relaySecret)

	rc.writeMu.Lock()
	if rc.ws != nil {
		rc.ws.Close()
		rc.ws = nil
	}
	rc.writeMu.Unlock()
}

func pushStatusHTTP(sessionID, relayURL, relaySecret, status string) {
	url := fmt.Sprintf("%s/sessions/%s/status", relayURL, sessionID)
	payload := map[string]string{"status": status}
	data, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST", url, bytes.NewReader(data))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Relay-Secret", relaySecret)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[relay] pushStatus HTTP error for %s: %v\n", sessionID, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		fmt.Fprintf(os.Stderr, "[relay] pushStatus failed for %s: %d\n", sessionID, resp.StatusCode)
	}
}

func endSessionHTTP(sessionID, relayURL, relaySecret string) {
	url := fmt.Sprintf("%s/sessions/%s/end", relayURL, sessionID)

	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return
	}
	req.Header.Set("X-Relay-Secret", relaySecret)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[relay] endSession HTTP error for %s: %v\n", sessionID, err)
		return
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= 400 {
		fmt.Fprintf(os.Stderr, "[relay] endSession failed for %s: %d\n", sessionID, resp.StatusCode)
	}
}
