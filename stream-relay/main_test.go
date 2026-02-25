package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	_ "github.com/lib/pq"
	"github.com/KranzL/shipmates-oss/stream-relay/relay"
)

var testDB *sql.DB

func TestMain(m *testing.M) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		fmt.Println("DATABASE_URL not set, skipping integration tests")
		os.Exit(0)
	}

	var err error
	testDB, err = sql.Open("postgres", dbURL)
	if err != nil {
		fmt.Printf("failed to connect to test db: %v\n", err)
		os.Exit(1)
	}

	db = testDB
	hub = relay.NewHub(1000)
	relaySecret = "test-secret"

	upgrader = websocket.Upgrader{
		CheckOrigin:       func(r *http.Request) bool { return true },
		EnableCompression: true,
	}

	historyStmt, err = db.Prepare(`
		SELECT sequence, content, created_at FROM session_outputs
		WHERE session_id = $1 AND sequence > $2
		ORDER BY sequence ASC LIMIT 5000
	`)
	if err != nil {
		fmt.Printf("failed to prepare history: %v\n", err)
		os.Exit(1)
	}

	startPersistWorkers(4)

	os.Exit(m.Run())
}

func setupMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /sessions/{sessionId}/chunks", handleChunk)
	mux.HandleFunc("POST /sessions/{sessionId}/end", handleEnd)
	mux.HandleFunc("GET /ingest", handleIngest)
	mux.HandleFunc("GET /ws", handleWebSocket)
	mux.HandleFunc("GET /health", handleHealth)
	return mux
}

func cleanupSession(t *testing.T, sessionID string) {
	t.Helper()
	testDB.Exec("DELETE FROM session_outputs WHERE session_id = $1", sessionID)
}

func TestHealthEndpoint(t *testing.T) {
	mux := setupMux()
	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Body.String() != "ok" {
		t.Fatalf("expected 'ok', got '%s'", w.Body.String())
	}
}

func TestChunkWithValidSecret(t *testing.T) {
	sessionID := "testchunkvalid" + fmt.Sprintf("%d", time.Now().UnixNano()%1000000000)
	defer cleanupSession(t, sessionID)

	mux := setupMux()
	body := `{"content":"hello world"}`
	req := httptest.NewRequest("POST", "/sessions/"+sessionID+"/chunks", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Relay-Secret", "test-secret")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != 204 {
		t.Fatalf("expected 204, got %d", w.Code)
	}

	time.Sleep(200 * time.Millisecond)

	var content string
	err := testDB.QueryRow("SELECT content FROM session_outputs WHERE session_id = $1", sessionID).Scan(&content)
	if err != nil {
		t.Fatalf("failed to query persisted chunk: %v", err)
	}
	if content != "hello world" {
		t.Fatalf("expected 'hello world', got '%s'", content)
	}
}

func TestChunkWithInvalidSecret(t *testing.T) {
	mux := setupMux()
	body := `{"content":"hello"}`
	req := httptest.NewRequest("POST", "/sessions/testsessionabcdefghij/chunks", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Relay-Secret", "wrong-secret")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != 403 {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestChunkWithMalformedJSON(t *testing.T) {
	mux := setupMux()
	req := httptest.NewRequest("POST", "/sessions/testsessionabcdefghij/chunks", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Relay-Secret", "test-secret")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != 400 {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestChunkWithInvalidSessionID(t *testing.T) {
	mux := setupMux()
	body := `{"content":"hello"}`
	req := httptest.NewRequest("POST", "/sessions/INVALID-ID!/chunks", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Relay-Secret", "test-secret")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != 400 {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestEndWithValidSecret(t *testing.T) {
	sessionID := "testendsession" + fmt.Sprintf("%d", time.Now().UnixNano()%1000000000)
	mux := setupMux()

	sub, _ := hub.Subscribe(sessionID)
	defer hub.Unsubscribe(sessionID, sub)

	req := httptest.NewRequest("POST", "/sessions/"+sessionID+"/end", nil)
	req.Header.Set("X-Relay-Secret", "test-secret")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != 204 {
		t.Fatalf("expected 204, got %d", w.Code)
	}

	select {
	case data := <-sub.Send:
		var chunk relay.Chunk
		json.Unmarshal(data, &chunk)
		if chunk.Type != "end" {
			t.Fatalf("expected type 'end', got '%s'", chunk.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for end event")
	}
}

func TestEndWithInvalidSecret(t *testing.T) {
	mux := setupMux()
	req := httptest.NewRequest("POST", "/sessions/testsessionabcdefghij/end", nil)
	req.Header.Set("X-Relay-Secret", "wrong-secret")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != 403 {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestWebSocketReceivesChunks(t *testing.T) {
	sessionID := "testwslive" + fmt.Sprintf("%06d", time.Now().UnixNano()%1000000000)

	mux := setupMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws?sessionId=" + sessionID
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect websocket: %v", err)
	}
	defer conn.Close()

	data, _ := json.Marshal(relay.Chunk{SessionID: sessionID, Sequence: 1, Content: "live data", Type: "data"})
	hub.Publish(sessionID, data)

	var chunk relay.Chunk
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read chunk: %v", err)
	}
	json.Unmarshal(msg, &chunk)

	if chunk.Content != "live data" {
		t.Fatalf("expected 'live data', got '%s'", chunk.Content)
	}
	if chunk.Type != "data" {
		t.Fatalf("expected type 'data', got '%s'", chunk.Type)
	}
}

func TestWebSocketSendsHistoryThenLive(t *testing.T) {
	sessionID := "testwshistory" + fmt.Sprintf("%06d", time.Now().UnixNano()%1000000000)
	defer cleanupSession(t, sessionID)

	testDB.Exec("INSERT INTO session_outputs (id, session_id, sequence, content, created_at) VALUES (gen_random_uuid(), $1, 1, 'history-1', NOW())", sessionID)
	testDB.Exec("INSERT INTO session_outputs (id, session_id, sequence, content, created_at) VALUES (gen_random_uuid(), $1, 2, 'history-2', NOW())", sessionID)

	mux := setupMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws?sessionId=" + sessionID
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect websocket: %v", err)
	}
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))

	var chunk1, chunk2 relay.Chunk
	_, msg1, _ := conn.ReadMessage()
	_, msg2, _ := conn.ReadMessage()
	json.Unmarshal(msg1, &chunk1)
	json.Unmarshal(msg2, &chunk2)

	if chunk1.Content != "history-1" || chunk2.Content != "history-2" {
		t.Fatalf("expected history chunks, got '%s' and '%s'", chunk1.Content, chunk2.Content)
	}
	if chunk1.Type != "data" || chunk2.Type != "data" {
		t.Fatal("expected type 'data' for history chunks")
	}
}

func TestWebSocketReceivesEnd(t *testing.T) {
	sessionID := "testwsend" + fmt.Sprintf("%06d", time.Now().UnixNano()%1000000000)

	mux := setupMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws?sessionId=" + sessionID
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect websocket: %v", err)
	}
	defer conn.Close()

	data, _ := json.Marshal(relay.Chunk{SessionID: sessionID, Sequence: 1, Type: "end"})
	hub.Publish(sessionID, data)

	var chunk relay.Chunk
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg, _ := conn.ReadMessage()
	json.Unmarshal(msg, &chunk)
	if chunk.Type != "end" {
		t.Fatalf("expected type 'end', got '%s'", chunk.Type)
	}
}

func TestWebSocketMissingSessionId(t *testing.T) {
	mux := setupMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	resp, err := http.Get(server.URL + "/ws")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestWebSocketInvalidSessionId(t *testing.T) {
	mux := setupMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	resp, err := http.Get(server.URL + "/ws?sessionId=INVALID!")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestMultipleWebSocketClientsReceiveChunks(t *testing.T) {
	sessionID := "testwsmulti" + fmt.Sprintf("%06d", time.Now().UnixNano()%1000000000)

	mux := setupMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws?sessionId=" + sessionID

	conn1, _, _ := websocket.DefaultDialer.Dial(wsURL, nil)
	conn2, _, _ := websocket.DefaultDialer.Dial(wsURL, nil)
	defer conn1.Close()
	defer conn2.Close()

	time.Sleep(50 * time.Millisecond)

	data, _ := json.Marshal(relay.Chunk{SessionID: sessionID, Sequence: 1, Content: "multi", Type: "data"})
	hub.Publish(sessionID, data)

	conn1.SetReadDeadline(time.Now().Add(2 * time.Second))
	conn2.SetReadDeadline(time.Now().Add(2 * time.Second))

	var c1, c2 relay.Chunk
	_, msg1, _ := conn1.ReadMessage()
	_, msg2, _ := conn2.ReadMessage()
	json.Unmarshal(msg1, &c1)
	json.Unmarshal(msg2, &c2)

	if c1.Content != "multi" || c2.Content != "multi" {
		t.Fatal("expected both clients to receive chunk")
	}
}

func TestClientDisconnectDoesNotAffectOthers(t *testing.T) {
	sessionID := "testwsdisconnect" + fmt.Sprintf("%06d", time.Now().UnixNano()%1000000000)

	mux := setupMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws?sessionId=" + sessionID

	conn1, _, _ := websocket.DefaultDialer.Dial(wsURL, nil)
	conn2, _, _ := websocket.DefaultDialer.Dial(wsURL, nil)
	defer conn2.Close()

	time.Sleep(50 * time.Millisecond)
	conn1.Close()
	time.Sleep(50 * time.Millisecond)

	data, _ := json.Marshal(relay.Chunk{SessionID: sessionID, Sequence: 1, Content: "after-disconnect", Type: "data"})
	hub.Publish(sessionID, data)

	conn2.SetReadDeadline(time.Now().Add(2 * time.Second))
	var chunk relay.Chunk
	_, msg, err := conn2.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read from remaining client: %v", err)
	}
	json.Unmarshal(msg, &chunk)
	if chunk.Content != "after-disconnect" {
		t.Fatalf("expected 'after-disconnect', got '%s'", chunk.Content)
	}
}

func TestDuplicateSequencePersistence(t *testing.T) {
	sessionID := "testdupseq" + fmt.Sprintf("%06d", time.Now().UnixNano()%1000000000)
	defer cleanupSession(t, sessionID)

	testDB.Exec("INSERT INTO session_outputs (id, session_id, sequence, content, created_at) VALUES (gen_random_uuid(), $1, 1, 'first', NOW())", sessionID)
	testDB.Exec("INSERT INTO session_outputs (id, session_id, sequence, content, created_at) VALUES (gen_random_uuid(), $1, 1, 'duplicate', NOW()) ON CONFLICT DO NOTHING", sessionID)

	var count int
	testDB.QueryRow("SELECT COUNT(*) FROM session_outputs WHERE session_id = $1 AND sequence = 1", sessionID).Scan(&count)
	if count != 1 {
		t.Fatalf("expected 1 row for duplicate sequence, got %d", count)
	}
}

func TestChunkEndToEnd(t *testing.T) {
	sessionID := "teste2e" + fmt.Sprintf("%06d", time.Now().UnixNano()%1000000000)
	defer cleanupSession(t, sessionID)

	mux := setupMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws?sessionId=" + sessionID
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	time.Sleep(50 * time.Millisecond)

	body, _ := json.Marshal(map[string]string{"content": "e2e-test"})
	req, _ := http.NewRequest("POST", server.URL+"/sessions/"+sessionID+"/chunks", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Relay-Secret", "test-secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("chunk request failed: %v", err)
	}
	if resp.StatusCode != 204 {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var chunk relay.Chunk
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read chunk via ws: %v", err)
	}
	json.Unmarshal(msg, &chunk)
	if chunk.Content != "e2e-test" {
		t.Fatalf("expected 'e2e-test', got '%s'", chunk.Content)
	}
	if chunk.Type != "data" {
		t.Fatalf("expected type 'data', got '%s'", chunk.Type)
	}
	if chunk.Timestamp == 0 {
		t.Fatal("expected non-zero timestamp")
	}
}

func TestConnectionLimitOnWebSocket(t *testing.T) {
	limitedHub := relay.NewHub(2)
	originalHub := hub
	hub = limitedHub
	defer func() { hub = originalHub }()

	mux := setupMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws?sessionId=testconnlimit00000000"

	conn1, _, _ := websocket.DefaultDialer.Dial(wsURL, nil)
	conn2, _, _ := websocket.DefaultDialer.Dial(wsURL, nil)
	defer conn1.Close()
	defer conn2.Close()

	time.Sleep(50 * time.Millisecond)

	conn3, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err == nil {
		conn3.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		_, msg, readErr := conn3.ReadMessage()
		if readErr == nil {
			var errMsg map[string]string
			json.Unmarshal(msg, &errMsg)
			if errMsg["error"] == "" {
				t.Fatal("expected connection limit error")
			}
		}
		conn3.Close()
	}
}

func TestIngestEndpoint(t *testing.T) {
	sessionID := "testingest" + fmt.Sprintf("%06d", time.Now().UnixNano()%1000000000)
	defer cleanupSession(t, sessionID)

	mux := setupMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	sub, _ := hub.Subscribe(sessionID)
	defer hub.Unsubscribe(sessionID, sub)

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ingest?sessionId=" + sessionID
	headers := http.Header{}
	headers.Set("X-Relay-Secret", "test-secret")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, headers)
	if err != nil {
		t.Fatalf("failed to connect ingest ws: %v", err)
	}

	conn.WriteMessage(websocket.TextMessage, []byte("ingest chunk 1"))
	conn.WriteMessage(websocket.TextMessage, []byte("ingest chunk 2"))

	for i := 0; i < 2; i++ {
		select {
		case data := <-sub.Send:
			var chunk relay.Chunk
			json.Unmarshal(data, &chunk)
			if chunk.Type != "data" {
				t.Fatalf("expected type 'data', got '%s'", chunk.Type)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for ingest chunk")
		}
	}

	conn.Close()
	time.Sleep(100 * time.Millisecond)

	select {
	case data := <-sub.Send:
		var chunk relay.Chunk
		json.Unmarshal(data, &chunk)
		if chunk.Type != "end" {
			t.Fatalf("expected end event after ingest close, got '%s'", chunk.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for end event")
	}
}

func TestIngestWithInvalidSecret(t *testing.T) {
	mux := setupMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	resp, err := http.Get(server.URL + "/ingest?sessionId=testsessionabcdefghij")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != 403 {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

func TestMaxBytesReader(t *testing.T) {
	mux := setupMux()
	bigBody := strings.Repeat("x", 2*1024*1024)
	body := `{"content":"` + bigBody + `"}`
	req := httptest.NewRequest("POST", "/sessions/testsessionabcdefghij/chunks", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Relay-Secret", "test-secret")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != 400 {
		t.Fatalf("expected 400 for oversized body, got %d", w.Code)
	}
}
