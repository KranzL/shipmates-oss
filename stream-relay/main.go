package main

import (
	"bytes"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log"
	"net"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	_ "github.com/lib/pq"
	"github.com/KranzL/shipmates-oss/stream-relay/relay"
	"golang.org/x/time/rate"
)

var (
	hub              *relay.Hub
	db               *sql.DB
	relaySecret      string
	historyStmt      *sql.Stmt
	persistChannels  []chan persistJob
	persistDone      chan struct{}
	sessionIDRe      = regexp.MustCompile(`^[a-z0-9]{20,36}$`)
	allowedOrigins   []string
	requireOriginChk bool
	trustedProxy     string
	trustedProxyNet  *net.IPNet
	persistDrops     atomic.Int64
	batchQueries     [51]string
)

var upgrader websocket.Upgrader

const (
	wsReadDeadline        = 60 * time.Second
	wsPingInterval        = 30 * time.Second
	wsWriteDeadline       = 10 * time.Second
	sessionInactivityTime = 10 * time.Minute
)

type persistJob struct {
	sessionID string
	sequence  int
	content   string
}

type ipLimiter struct {
	limiter  *rate.Limiter
	lastSeen atomic.Int64
}

var allowedStatuses = map[string]bool{
	"running": true, "waiting_for_clarification": true,
	"done": true, "error": true, "cancelled": true, "cancelling": true, "pending": true,
}

var (
	httpLimiters   sync.Map
	wsLimiters     sync.Map
	statusLimiters sync.Map
)

func getStatusLimiter(sessionID string) *rate.Limiter {
	if v, ok := statusLimiters.Load(sessionID); ok {
		return v.(*rate.Limiter)
	}
	limiter := rate.NewLimiter(1, 2)
	actual, _ := statusLimiters.LoadOrStore(sessionID, limiter)
	return actual.(*rate.Limiter)
}

func getHTTPLimiter(ip string) *rate.Limiter {
	now := time.Now().UnixNano()
	val, ok := httpLimiters.Load(ip)
	if ok {
		entry := val.(*ipLimiter)
		entry.lastSeen.Store(now)
		return entry.limiter
	}
	limiter := rate.NewLimiter(100, 100)
	entry := &ipLimiter{limiter: limiter}
	entry.lastSeen.Store(now)
	actual, loaded := httpLimiters.LoadOrStore(ip, entry)
	if loaded {
		actual.(*ipLimiter).lastSeen.Store(now)
		return actual.(*ipLimiter).limiter
	}
	return limiter
}

func getWSLimiter(ip string) *rate.Limiter {
	now := time.Now().UnixNano()
	val, ok := wsLimiters.Load(ip)
	if ok {
		entry := val.(*ipLimiter)
		entry.lastSeen.Store(now)
		return entry.limiter
	}
	limiter := rate.NewLimiter(rate.Every(6*time.Second), 10)
	entry := &ipLimiter{limiter: limiter}
	entry.lastSeen.Store(now)
	actual, loaded := wsLimiters.LoadOrStore(ip, entry)
	if loaded {
		actual.(*ipLimiter).lastSeen.Store(now)
		return actual.(*ipLimiter).limiter
	}
	return limiter
}

func cleanupLimiters() {
	ticker := time.NewTicker(5 * time.Minute)
	for range ticker.C {
		cutoff := time.Now().Add(-5 * time.Minute).UnixNano()
		httpLimiters.Range(func(key, value any) bool {
			if value.(*ipLimiter).lastSeen.Load() < cutoff {
				httpLimiters.Delete(key)
			}
			return true
		})
		wsLimiters.Range(func(key, value any) bool {
			if value.(*ipLimiter).lastSeen.Load() < cutoff {
				wsLimiters.Delete(key)
			}
			return true
		})
	}
}

func extractIP(r *http.Request) string {
	remoteHost, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		remoteHost = r.RemoteAddr
	}

	if trustedProxyNet != nil {
		remoteIP := net.ParseIP(remoteHost)
		if remoteIP != nil && trustedProxyNet.Contains(remoteIP) {
			if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
				parts := strings.Split(xff, ",")
				for i := len(parts) - 1; i >= 0; i-- {
					candidate := strings.TrimSpace(parts[i])
					if ip := net.ParseIP(candidate); ip != nil && !isPrivateIP(ip) {
						return candidate
					}
				}
			}
		}
	} else if trustedProxy != "" {
		if remoteHost == trustedProxy {
			if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
				parts := strings.Split(xff, ",")
				for i := len(parts) - 1; i >= 0; i-- {
					candidate := strings.TrimSpace(parts[i])
					if ip := net.ParseIP(candidate); ip != nil && !isPrivateIP(ip) {
						return candidate
					}
				}
			}
		}
	}

	return remoteHost
}

var privateRanges []*net.IPNet

func init() {
	privCIDRs := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"127.0.0.0/8",
		"::1/128",
		"fc00::/7",
	}
	for _, cidr := range privCIDRs {
		_, network, _ := net.ParseCIDR(cidr)
		privateRanges = append(privateRanges, network)
	}
}

func isPrivateIP(ip net.IP) bool {
	for _, network := range privateRanges {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func buildBatchQuery(size int) string {
	var builder strings.Builder
	builder.WriteString("INSERT INTO session_outputs (id, session_id, sequence, content, created_at) VALUES ")
	for i := 0; i < size; i++ {
		if i > 0 {
			builder.WriteString(", ")
		}
		base := i * 3
		fmt.Fprintf(&builder, "(gen_random_uuid(), $%d, $%d, $%d, NOW())", base+1, base+2, base+3)
	}
	builder.WriteString(" ON CONFLICT DO NOTHING")
	return builder.String()
}

func main() {
	hub = relay.NewHub(1000)
	relaySecret = os.Getenv("RELAY_SECRET")
	if relaySecret == "" {
		log.Fatal("RELAY_SECRET is required")
	}

	if origins := os.Getenv("ALLOWED_ORIGINS"); origins != "" {
		for _, o := range strings.Split(origins, ",") {
			trimmed := strings.TrimSpace(o)
			if trimmed != "" {
				allowedOrigins = append(allowedOrigins, trimmed)
			}
		}
	}

	requireOriginChk = os.Getenv("REQUIRE_ORIGIN_CHECK") != "false"
	trustedProxy = os.Getenv("TRUSTED_PROXY")

	if trustedProxy != "" {
		if strings.Contains(trustedProxy, "/") {
			_, network, err := net.ParseCIDR(trustedProxy)
			if err == nil {
				trustedProxyNet = network
			}
		}
	}

	for i := 1; i <= 50; i++ {
		batchQueries[i] = buildBatchQuery(i)
	}

	upgrader = websocket.Upgrader{
		CheckOrigin:       checkOrigin,
		EnableCompression: true,
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL is required")
	}

	var err error
	db, err = sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.Ping(); err != nil {
		log.Fatalf("failed to ping database: %v", err)
	}

	historyStmt, err = db.Prepare(`
		SELECT sequence, content, created_at FROM session_outputs
		WHERE session_id = $1 AND sequence > $2
		ORDER BY sequence ASC LIMIT 1000
	`)
	if err != nil {
		log.Fatalf("failed to prepare history statement: %v", err)
	}

	persistDone = make(chan struct{})
	startPersistWorkers(4)
	go cleanupLimiters()
	hub.StartSequenceCleanup(5*time.Minute, persistDone)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /sessions/{sessionId}/chunks", handleChunk)
	mux.HandleFunc("POST /sessions/{sessionId}/status", handleStatus)
	mux.HandleFunc("POST /sessions/{sessionId}/end", handleEnd)
	mux.HandleFunc("GET /ingest", handleIngest)
	mux.HandleFunc("GET /ws", handleWebSocket)
	mux.HandleFunc("GET /health", handleHealth)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("stream-relay listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}

func checkOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if len(allowedOrigins) == 0 {
		if requireOriginChk && origin != "" {
			return false
		}
		return true
	}
	for _, allowed := range allowedOrigins {
		if origin == allowed {
			return true
		}
	}
	return false
}

func authenticateRelay(r *http.Request) bool {
	got := r.Header.Get("X-Relay-Secret")
	return subtle.ConstantTimeCompare([]byte(got), []byte(relaySecret)) == 1
}

func validSessionID(id string) bool {
	return sessionIDRe.MatchString(id)
}

func publishChunk(sessionID string, content string, chunkType string) ([]byte, int) {
	seq := hub.NextSequence(sessionID)
	chunk := relay.Chunk{
		SessionID: sessionID,
		Sequence:  seq,
		Content:   content,
		Type:      chunkType,
		Timestamp: time.Now().UnixMilli(),
	}
	data, err := json.Marshal(chunk)
	if err != nil {
		log.Printf("failed to marshal chunk: %v", err)
		return nil, 0
	}
	hub.Publish(sessionID, data)
	return data, seq
}

var sessionTimers sync.Map

func resetSessionTimer(sessionID string) {
	if existing, ok := sessionTimers.Load(sessionID); ok {
		existing.(*time.Timer).Reset(sessionInactivityTime)
		return
	}
	t := time.AfterFunc(sessionInactivityTime, func() {
		sessionTimers.Delete(sessionID)
		_, seq := publishChunk(sessionID, "", "end")
		enqueuePersist(sessionID, seq, "")
		hub.CleanupSequence(sessionID)
		log.Printf("session %s auto-closed due to inactivity", sessionID)
	})
	actual, loaded := sessionTimers.LoadOrStore(sessionID, t)
	if loaded {
		t.Stop()
		actual.(*time.Timer).Reset(sessionInactivityTime)
	}
}

func cancelSessionTimer(sessionID string) {
	if existing, ok := sessionTimers.LoadAndDelete(sessionID); ok {
		existing.(*time.Timer).Stop()
	}
}

func handleChunk(w http.ResponseWriter, r *http.Request) {
	if !authenticateRelay(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	ip := extractIP(r)
	if !getHTTPLimiter(ip).Allow() {
		http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
		return
	}

	sessionID := r.PathValue("sessionId")
	if sessionID == "" || !validSessionID(sessionID) {
		http.Error(w, "invalid sessionId", http.StatusBadRequest)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var body struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	_, seq := publishChunk(sessionID, body.Content, "data")
	enqueuePersist(sessionID, seq, body.Content)
	resetSessionTimer(sessionID)

	w.WriteHeader(http.StatusNoContent)
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	if !authenticateRelay(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	ip := extractIP(r)
	if !getHTTPLimiter(ip).Allow() {
		http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
		return
	}

	sessionID := r.PathValue("sessionId")
	if sessionID == "" || !validSessionID(sessionID) {
		http.Error(w, "invalid sessionId", http.StatusBadRequest)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<10)

	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Status == "" {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	if !allowedStatuses[body.Status] {
		http.Error(w, "invalid status", http.StatusBadRequest)
		return
	}

	if !getStatusLimiter(sessionID).Allow() {
		http.Error(w, "status rate limit exceeded", http.StatusTooManyRequests)
		return
	}

	publishChunk(sessionID, body.Status, "status")
	resetSessionTimer(sessionID)

	w.WriteHeader(http.StatusNoContent)
}

func handleEnd(w http.ResponseWriter, r *http.Request) {
	if !authenticateRelay(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	sessionID := r.PathValue("sessionId")
	if sessionID == "" || !validSessionID(sessionID) {
		http.Error(w, "invalid sessionId", http.StatusBadRequest)
		return
	}

	cancelSessionTimer(sessionID)
	_, seq := publishChunk(sessionID, "", "end")
	enqueuePersist(sessionID, seq, "")
	hub.CleanupSequence(sessionID)

	w.WriteHeader(http.StatusNoContent)
}

func handleIngest(w http.ResponseWriter, r *http.Request) {
	if !authenticateRelay(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	ip := extractIP(r)
	if !getWSLimiter(ip).Allow() {
		http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
		return
	}

	sessionID := r.URL.Query().Get("sessionId")
	if sessionID == "" || !validSessionID(sessionID) {
		http.Error(w, "invalid sessionId", http.StatusBadRequest)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	conn.SetReadLimit(1 << 20)
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(wsReadDeadline))
	})

	pingTicker := time.NewTicker(wsPingInterval)
	defer pingTicker.Stop()

	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		for {
			if err := conn.SetReadDeadline(time.Now().Add(wsReadDeadline)); err != nil {
				return
			}
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			content := string(msg)
			if len(msg) > 0 && msg[0] == '{' {
				var typed struct {
					Type    string `json:"type"`
					Content string `json:"content"`
				}
				if json.Unmarshal(msg, &typed) == nil && typed.Type == "status" && allowedStatuses[typed.Content] {
					if getStatusLimiter(sessionID).Allow() {
						publishChunk(sessionID, typed.Content, "status")
						resetSessionTimer(sessionID)
					}
					continue
				}
			}
			seq := hub.NextSequence(sessionID)
			chunk := relay.Chunk{
				SessionID: sessionID,
				Sequence:  seq,
				Content:   content,
				Type:      "data",
				Timestamp: time.Now().UnixMilli(),
			}
			data, _ := json.Marshal(chunk)
			hub.Publish(sessionID, data)
			enqueuePersist(sessionID, seq, content)
		}
	}()

	for {
		select {
		case <-readDone:
			_, seq := publishChunk(sessionID, "", "end")
			enqueuePersist(sessionID, seq, "")
			hub.CleanupSequence(sessionID)
			return
		case <-pingTicker.C:
			conn.SetWriteDeadline(time.Now().Add(wsWriteDeadline))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("sessionId")
	if sessionID == "" || !validSessionID(sessionID) {
		http.Error(w, "invalid sessionId", http.StatusBadRequest)
		return
	}

	ip := extractIP(r)
	if !getWSLimiter(ip).Allow() {
		http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	conn.EnableWriteCompression(true)

	afterSequence := 0
	if lastSequenceParam := r.URL.Query().Get("lastSequence"); lastSequenceParam != "" {
		if parsed, parseErr := strconv.Atoi(lastSequenceParam); parseErr == nil && parsed >= 0 {
			afterSequence = parsed
		}
	}

	if err := streamHistory(conn, sessionID, afterSequence); err != nil {
		return
	}

	sub, err := hub.Subscribe(sessionID)
	if err != nil {
		conn.WriteJSON(map[string]string{"error": "subscription unavailable"})
		return
	}

	var once sync.Once
	cleanup := func() {
		once.Do(func() {
			hub.Unsubscribe(sessionID, sub)
		})
	}
	defer cleanup()

	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(wsReadDeadline))
	})

	pingTicker := time.NewTicker(wsPingInterval)
	defer pingTicker.Stop()

	go func() {
		defer cleanup()
		for {
			if err := conn.SetReadDeadline(time.Now().Add(wsReadDeadline)); err != nil {
				return
			}
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	endMarker := []byte(`"type":"end"`)
	for {
		select {
		case data, ok := <-sub.Send:
			if !ok {
				return
			}
			conn.SetWriteDeadline(time.Now().Add(wsWriteDeadline))
			if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
				return
			}
			if bytes.Contains(data, endMarker) {
				return
			}
		case <-sub.Done:
			return
		case <-pingTicker.C:
			conn.SetWriteDeadline(time.Now().Add(wsWriteDeadline))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func streamHistory(conn *websocket.Conn, sessionID string, afterSequence int) error {
	rows, err := historyStmt.Query(sessionID, afterSequence)
	if err != nil {
		conn.WriteJSON(map[string]string{"error": "failed to load history"})
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var seq int
		var content string
		var createdAt time.Time
		if err := rows.Scan(&seq, &content, &createdAt); err != nil {
			return err
		}
		chunk := relay.Chunk{
			SessionID: sessionID,
			Sequence:  seq,
			Content:   content,
			Type:      "data",
			Timestamp: createdAt.UnixMilli(),
		}
		data, err := json.Marshal(chunk)
		if err != nil {
			return err
		}
		conn.SetWriteDeadline(time.Now().Add(wsWriteDeadline))
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			return err
		}
	}
	return rows.Err()
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]any{
		"status":       "ok",
		"subscribers":  hub.TotalSubscribers(),
		"connections":  hub.ActiveConnections(),
		"persistDrops": persistDrops.Load(),
	})
}

func hashSessionID(sessionID string) int {
	hasher := fnv.New32a()
	hasher.Write([]byte(sessionID))
	return int(hasher.Sum32())
}

func startPersistWorkers(n int) {
	persistChannels = make([]chan persistJob, n)
	for i := 0; i < n; i++ {
		persistChannels[i] = make(chan persistJob, 1024)
		go persistWorker(persistChannels[i], persistDone)
	}
}

func persistWorker(ch chan persistJob, done <-chan struct{}) {
	batch := make([]persistJob, 0, 50)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case job := <-ch:
			batch = append(batch, job)
			if len(batch) >= 50 {
				flushBatch(batch)
				batch = batch[:0]
			}
		case <-ticker.C:
			if len(batch) > 0 {
				flushBatch(batch)
				batch = batch[:0]
			}
		case <-done:
			for {
				select {
				case job := <-ch:
					batch = append(batch, job)
				default:
					if len(batch) > 0 {
						flushBatch(batch)
					}
					return
				}
			}
		}
	}
}

func flushBatch(batch []persistJob) {
	if len(batch) == 0 {
		return
	}
	size := len(batch)
	if size > 50 {
		size = 50
	}
	query := batchQueries[size]
	args := make([]any, 0, size*3)
	for i := 0; i < size; i++ {
		args = append(args, batch[i].sessionID, batch[i].sequence, batch[i].content)
	}
	if _, err := db.Exec(query, args...); err != nil {
		log.Printf("batch persist failed: %v", err)
	}
	if len(batch) > 50 {
		flushBatch(batch[50:])
	}
}

func enqueuePersist(sessionID string, sequence int, content string) {
	idx := hashSessionID(sessionID) % len(persistChannels)
	select {
	case persistChannels[idx] <- persistJob{sessionID: sessionID, sequence: sequence, content: content}:
	default:
		dropped := persistDrops.Add(1)
		log.Printf("persist channel full, dropping chunk for session %s seq %d (total drops: %d)", sessionID, sequence, dropped)
	}
}
