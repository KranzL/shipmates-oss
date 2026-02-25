package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	_ "github.com/lib/pq"
)

var (
	db                    *sql.DB
	encryptionKey         string
	authSecret            string
	httpClient            = &http.Client{Timeout: 10 * time.Second}
	prReviewServiceURL    = "http://pr-review:8092"
	ciDiagnosisServiceURL = "http://ci-diagnosis:8093"
	forwardWg             sync.WaitGroup
	forwardSem            = make(chan struct{}, 50)
)

const (
	rateLimitWindowMS  = 60 * 1000
	rateLimitMaxPerIP  = 15
	rateLimitMaxGlobal = 300
	cleanupIntervalMS  = 60 * 1000
	maxRequestBodySize = 1 * 1024 * 1024
)

type ipRateLimiter struct {
	timestamps []int64
	mu         sync.Mutex
}

var (
	rateLimitMap sync.Map
	lastCleanup  int64
	globalMu     sync.Mutex
	globalCount  int64
	globalStart  int64
)

func checkRateLimit(ip string) bool {
	now := time.Now().UnixMilli()
	windowStart := now - rateLimitWindowMS

	globalMu.Lock()
	if now-globalStart > rateLimitWindowMS {
		globalCount = 0
		globalStart = now
	}
	if globalCount >= rateLimitMaxGlobal {
		globalMu.Unlock()
		return false
	}
	globalMu.Unlock()

	val, _ := rateLimitMap.LoadOrStore(ip, &ipRateLimiter{})
	limiter := val.(*ipRateLimiter)

	limiter.mu.Lock()
	defer limiter.mu.Unlock()

	recent := limiter.timestamps[:0]
	for _, ts := range limiter.timestamps {
		if ts > windowStart {
			recent = append(recent, ts)
		}
	}
	limiter.timestamps = recent

	if len(limiter.timestamps) >= rateLimitMaxPerIP {
		return false
	}

	limiter.timestamps = append(limiter.timestamps, now)

	globalMu.Lock()
	globalCount++
	globalMu.Unlock()

	if now-atomic.LoadInt64(&lastCleanup) > cleanupIntervalMS {
		atomic.StoreInt64(&lastCleanup, now)
		go cleanupRateLimits(windowStart)
	}

	return true
}

func cleanupRateLimits(windowStart int64) {
	rateLimitMap.Range(func(key, value any) bool {
		limiter := value.(*ipRateLimiter)
		limiter.mu.Lock()
		allExpired := true
		for _, ts := range limiter.timestamps {
			if ts > windowStart {
				allExpired = false
				break
			}
		}
		limiter.mu.Unlock()
		if allExpired {
			rateLimitMap.Delete(key)
		}
		return true
	})
}

func verifyHMACSignature(payload []byte, signature string, secret string) bool {
	if !strings.HasPrefix(signature, "sha256=") {
		return false
	}

	expectedMAC, err := hex.DecodeString(strings.TrimPrefix(signature, "sha256="))
	if err != nil {
		return false
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	actualMAC := mac.Sum(nil)

	return hmac.Equal(expectedMAC, actualMAC)
}

type routePayload struct {
	UserID      string `json:"userId"`
	AgentID     string `json:"agentId"`
	AgentName   string `json:"agentName"`
	AgentRole   string `json:"agentRole"`
	Personality string `json:"personality"`
	GithubToken string `json:"githubToken"`
	Provider    string `json:"provider"`
	APIKey      string `json:"apiKey"`
	BaseURL     string `json:"baseUrl,omitempty"`
	ModelID     string `json:"modelId,omitempty"`
}

func buildRoutePayload(ctx *webhookContext) (*routePayload, error) {
	githubToken, err := decryptAES256GCM(ctx.GithubToken, encryptionKey)
	if err != nil {
		return nil, fmt.Errorf("decrypt github token: %w", err)
	}

	apiKey, err := decryptAES256GCM(ctx.EncryptedKey, encryptionKey)
	if err != nil {
		return nil, fmt.Errorf("decrypt api key: %w", err)
	}

	return &routePayload{
		UserID:      ctx.UserID,
		AgentID:     ctx.AgentID,
		AgentName:   ctx.AgentName,
		AgentRole:   ctx.AgentRole,
		Personality: ctx.Personality,
		GithubToken: githubToken,
		Provider:    ctx.Provider,
		APIKey:      apiKey,
		BaseURL:     ctx.BaseURL,
		ModelID:     ctx.ModelID,
	}, nil
}

func forwardToService(ctx context.Context, serviceURL string, path string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", serviceURL+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+authSecret)

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("forward request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		limitedBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("service returned %d: %s", resp.StatusCode, string(limitedBody))
	}

	return nil
}

func boundedForward(serviceURL string, path string, payload any, label string) {
	select {
	case forwardSem <- struct{}{}:
		forwardWg.Add(1)
		go func() {
			defer forwardWg.Done()
			defer func() { <-forwardSem }()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := forwardToService(ctx, serviceURL, path, payload); err != nil {
				log.Printf("forward to %s: %v", label, err)
			}
		}()
	default:
		log.Printf("forward to %s dropped: worker pool full", label)
	}
}

type reviewRequest struct {
	routePayload
	Repo     string `json:"repo"`
	PRNumber int    `json:"prNumber"`
	PRTitle  string `json:"prTitle"`
	PRBody   string `json:"prBody"`
	Action   string `json:"action"`
}

type diagnosisRequest struct {
	routePayload
	Repo         string `json:"repo"`
	PRNumber     int    `json:"prNumber,omitempty"`
	CommitSHA    string `json:"commitSha"`
	WorkflowName string `json:"workflowName"`
	RunID        int64  `json:"runId"`
}

func handleWebhook(w http.ResponseWriter, r *http.Request) {
	userAgent := r.Header.Get("User-Agent")
	if !strings.HasPrefix(userAgent, "GitHub-Hookshot/") {
		writeError(w, http.StatusForbidden, "invalid User-Agent")
		return
	}

	forwardedFor := r.Header.Get("X-Forwarded-For")
	ip := "unknown"
	if forwardedFor != "" {
		ip = strings.TrimSpace(strings.Split(forwardedFor, ",")[0])
	} else {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err == nil {
			ip = host
		}
	}

	if !checkRateLimit(ip) {
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "rate limit exceeded, retry after 60 seconds")
		return
	}

	bodyReader := http.MaxBytesReader(w, r.Body, maxRequestBodySize)
	body, err := io.ReadAll(bodyReader)
	if err != nil {
		writeError(w, http.StatusBadRequest, "request body too large")
		return
	}

	event := r.Header.Get("X-GitHub-Event")
	signature := r.Header.Get("X-Hub-Signature-256")

	if event != "pull_request" && event != "check_suite" && event != "check_run" && event != "issues" {
		writeJSON(w, http.StatusOK, map[string]bool{"ignored": true})
		return
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON payload")
		return
	}

	repoObj, _ := payload["repository"].(map[string]any)
	repo, _ := repoObj["full_name"].(string)
	if repo == "" {
		writeError(w, http.StatusBadRequest, "missing repository")
		return
	}

	ctx, notFound, err := resolveWebhookContext(db, repo, encryptionKey)
	if err != nil {
		log.Printf("resolve context for %s: %v", repo, err)
		writeError(w, http.StatusInternalServerError, "failed to resolve webhook context")
		return
	}
	if ctx == nil {
		writeError(w, http.StatusNotFound, notFound)
		return
	}

	if !verifyHMACSignature(body, signature, ctx.WebhookSecret) {
		writeError(w, http.StatusUnauthorized, "invalid signature")
		return
	}

	switch event {
	case "pull_request":
		handlePullRequest(w, payload, ctx)
	case "check_suite":
		handleCheckSuite(w, payload, ctx, repo)
	case "check_run":
		handleCheckRun(w, payload, ctx, repo)
	case "issues":
		handleIssues(w, payload, ctx, repo)
	default:
		writeJSON(w, http.StatusOK, map[string]bool{"ignored": true})
	}
}

func handlePullRequest(w http.ResponseWriter, payload map[string]any, ctx *webhookContext) {
	action, _ := payload["action"].(string)
	if action != "opened" && action != "synchronize" {
		writeJSON(w, http.StatusOK, map[string]bool{"ignored": true})
		return
	}

	pr, _ := payload["pull_request"].(map[string]any)
	if pr == nil {
		writeError(w, http.StatusBadRequest, "missing pull_request data")
		return
	}

	prNumber, _ := pr["number"].(float64)
	prTitle, _ := pr["title"].(string)
	prBody, _ := pr["body"].(string)
	repoObj, _ := payload["repository"].(map[string]any)
	repo, _ := repoObj["full_name"].(string)

	routeData, err := buildRoutePayload(ctx)
	if err != nil {
		log.Printf("build route payload: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to decrypt credentials")
		return
	}

	req := reviewRequest{
		routePayload: *routeData,
		Repo:         repo,
		PRNumber:     int(prNumber),
		PRTitle:      prTitle,
		PRBody:       prBody,
		Action:       action,
	}

	boundedForward(prReviewServiceURL, "/reviews", req, "pr-review")
	writeJSON(w, http.StatusAccepted, map[string]bool{"forwarded": true})
}

func handleCheckSuite(w http.ResponseWriter, payload map[string]any, ctx *webhookContext, repo string) {
	action, _ := payload["action"].(string)
	if action != "completed" {
		writeJSON(w, http.StatusOK, map[string]bool{"ignored": true})
		return
	}

	checkSuite, _ := payload["check_suite"].(map[string]any)
	if checkSuite == nil {
		writeJSON(w, http.StatusOK, map[string]bool{"ignored": true})
		return
	}

	conclusion, _ := checkSuite["conclusion"].(string)
	if conclusion != "failure" {
		writeJSON(w, http.StatusOK, map[string]bool{"ignored": true})
		return
	}

	headSHA, _ := checkSuite["head_sha"].(string)

	var prNumber int
	if pullRequests, ok := checkSuite["pull_requests"].([]any); ok && len(pullRequests) > 0 {
		if first, ok := pullRequests[0].(map[string]any); ok {
			if n, ok := first["number"].(float64); ok {
				prNumber = int(n)
			}
		}
	}

	routeData, err := buildRoutePayload(ctx)
	if err != nil {
		log.Printf("build route payload: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to decrypt credentials")
		return
	}

	req := diagnosisRequest{
		routePayload: *routeData,
		Repo:         repo,
		PRNumber:     prNumber,
		CommitSHA:    headSHA,
		WorkflowName: "check_suite",
		RunID:        0,
	}

	boundedForward(ciDiagnosisServiceURL, "/diagnoses", req, "ci-diagnosis")
	writeJSON(w, http.StatusAccepted, map[string]bool{"forwarded": true})
}

func handleCheckRun(w http.ResponseWriter, payload map[string]any, ctx *webhookContext, repo string) {
	action, _ := payload["action"].(string)
	if action != "completed" {
		writeJSON(w, http.StatusOK, map[string]bool{"ignored": true})
		return
	}

	checkRun, _ := payload["check_run"].(map[string]any)
	if checkRun == nil {
		writeJSON(w, http.StatusOK, map[string]bool{"ignored": true})
		return
	}

	conclusion, _ := checkRun["conclusion"].(string)
	if conclusion != "failure" {
		writeJSON(w, http.StatusOK, map[string]bool{"ignored": true})
		return
	}

	headSHA, _ := checkRun["head_sha"].(string)
	runName, _ := checkRun["name"].(string)

	checkSuiteObj, _ := checkRun["check_suite"].(map[string]any)
	var prNumber int
	if checkSuiteObj != nil {
		if pullRequests, ok := checkSuiteObj["pull_requests"].([]any); ok && len(pullRequests) > 0 {
			if first, ok := pullRequests[0].(map[string]any); ok {
				if n, ok := first["number"].(float64); ok {
					prNumber = int(n)
				}
			}
		}
	}

	routeData, err := buildRoutePayload(ctx)
	if err != nil {
		log.Printf("build route payload: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to decrypt credentials")
		return
	}

	req := diagnosisRequest{
		routePayload: *routeData,
		Repo:         repo,
		PRNumber:     prNumber,
		CommitSHA:    headSHA,
		WorkflowName: runName,
		RunID:        0,
	}

	boundedForward(ciDiagnosisServiceURL, "/diagnoses", req, "ci-diagnosis")
	writeJSON(w, http.StatusAccepted, map[string]bool{"forwarded": true})
}

func handleIssues(w http.ResponseWriter, payload map[string]any, ctx *webhookContext, repo string) {
	action, _ := payload["action"].(string)

	issue, _ := payload["issue"].(map[string]any)
	if issue == nil {
		writeError(w, http.StatusBadRequest, "missing issue data")
		return
	}

	issueNumberFloat, _ := issue["number"].(float64)
	issueNumber := int(issueNumberFloat)
	issueTitle, _ := issue["title"].(string)
	issueBody, _ := issue["body"].(string)
	labelsRaw, _ := issue["labels"].([]any)

	var labels []string
	for _, l := range labelsRaw {
		if labelMap, ok := l.(map[string]any); ok {
			if name, ok := labelMap["name"].(string); ok {
				labels = append(labels, name)
			}
		}
	}

	shouldQueue := false
	if action == "labeled" {
		if labelPayload, ok := payload["label"].(map[string]any); ok {
			if name, ok := labelPayload["name"].(string); ok && name == ctx.IssueLabel {
				shouldQueue = true
			}
		}
	} else if action == "opened" {
		for _, l := range labels {
			if l == ctx.IssueLabel {
				shouldQueue = true
				break
			}
		}
	}

	if !shouldQueue {
		writeJSON(w, http.StatusOK, map[string]bool{"ignored": true})
		return
	}

	if err := createIssuePipeline(db, ctx.UserID, ctx.AgentID, repo, issueNumber, issueTitle, issueBody, labels); err != nil {
		log.Printf("create issue pipeline: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to queue issue")
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"queued": true})
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	status := "ok"
	httpStatus := http.StatusOK
	if err := db.Ping(); err != nil {
		status = "degraded"
		httpStatus = http.StatusServiceUnavailable
	}
	writeJSON(w, httpStatus, map[string]any{
		"status":  status,
		"service": "webhook-router",
	})
}

func main() {
	encryptionKey = os.Getenv("ENCRYPTION_KEY")
	if encryptionKey == "" || len(encryptionKey) < 32 {
		log.Fatal("ENCRYPTION_KEY is required (min 32 characters)")
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL is required")
	}

	authSecret = os.Getenv("AUTH_SECRET")
	if authSecret == "" {
		log.Fatal("AUTH_SECRET is required")
	}

	if v := os.Getenv("PR_REVIEW_SERVICE_URL"); v != "" {
		prReviewServiceURL = v
	}
	if v := os.Getenv("CI_DIAGNOSIS_SERVICE_URL"); v != "" {
		ciDiagnosisServiceURL = v
	}

	var err error
	db, err = sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}

	maxOpenConns := 10
	if v := os.Getenv("MAX_OPEN_CONNS"); v != "" {
		if parsed, parseErr := strconv.Atoi(v); parseErr == nil && parsed > 0 {
			maxOpenConns = parsed
		}
	}

	db.SetMaxOpenConns(maxOpenConns)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.Ping(); err != nil {
		log.Fatalf("ping database: %v", err)
	}

	globalStart = time.Now().UnixMilli()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /webhook", handleWebhook)
	mux.HandleFunc("GET /health", handleHealth)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8094"
	}

	server := &http.Server{
		Addr:         ":" + port,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	shutdownChan := make(chan os.Signal, 1)
	signal.Notify(shutdownChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		log.Printf("webhook-router listening on :%s", port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	<-shutdownChan
	log.Println("shutting down webhook-router")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	server.Shutdown(shutdownCtx)

	log.Println("waiting for in-flight forwards to complete")
	done := make(chan struct{})
	go func() {
		forwardWg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Println("all forwards completed")
	case <-shutdownCtx.Done():
		log.Println("shutdown timeout, forcing exit")
	}

	db.Close()
	log.Println("webhook-router stopped")
}
