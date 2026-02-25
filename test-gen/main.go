package main

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	_ "github.com/lib/pq"
	"github.com/KranzL/shipmates-oss/test-gen/testgen"
)

var (
	db              *sql.DB
	generator       *testgen.Generator
	authSecret      string
	workerSemaphore chan struct{}
	workerWg        sync.WaitGroup
	shutdownCtx     context.Context
)

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

func authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			writeError(w, http.StatusUnauthorized, "missing or invalid authorization header")
			return
		}
		token := strings.TrimPrefix(authHeader, "Bearer ")
		if subtle.ConstantTimeCompare([]byte(token), []byte(authSecret)) != 1 {
			writeError(w, http.StatusUnauthorized, "invalid token")
			return
		}
		next(w, r)
	}
}

func handleCreateTestGen(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 512*1024)

	var req testgen.TestGenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var missingFields []string
	if req.Repo == "" {
		missingFields = append(missingFields, "repo")
	}
	if req.GithubToken == "" {
		missingFields = append(missingFields, "githubToken")
	}
	if req.APIKey == "" {
		missingFields = append(missingFields, "apiKey")
	}
	if req.Provider == "" {
		missingFields = append(missingFields, "provider")
	}
	if req.UserID == "" {
		missingFields = append(missingFields, "userId")
	}
	if len(missingFields) > 0 {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("missing required fields: %s", strings.Join(missingFields, ", ")))
		return
	}

	if req.SessionID == "" {
		req.SessionID = testgen.GenerateID()
	}

	select {
	case workerSemaphore <- struct{}{}:
		workerWg.Add(1)
		go func() {
			defer workerWg.Done()
			defer func() { <-workerSemaphore }()

			result, err := generator.ProcessTestGen(shutdownCtx, req)
			if err != nil {
				log.Printf("process test gen for %s: %v", req.Repo, err)
				return
			}
			log.Printf("test gen %s completed for %s: %.1f%% -> %.1f%% (%d iterations, %d tests)",
				result.ID, req.Repo, result.BaselineCoverage, result.FinalCoverage,
				result.Iterations, len(result.TestsCreated))
		}()
		writeJSON(w, http.StatusAccepted, map[string]string{
			"status":    "processing",
			"sessionId": req.SessionID,
		})
	default:
		w.Header().Set("Retry-After", "30")
		writeError(w, http.StatusServiceUnavailable, "test generation queue is full, try again later")
	}
}

func handleListTestGens(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("userId")
	if userID == "" {
		writeError(w, http.StatusBadRequest, "userId is required")
		return
	}

	limit := 20
	if v := r.URL.Query().Get("limit"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}

	offset := 0
	if v := r.URL.Query().Get("offset"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	store := testgen.NewStore(db)
	testGens, total, err := store.ListTestGens(userID, limit, offset)
	if err != nil {
		log.Printf("list test gens: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to list test generations")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"testGens": testGens,
		"total":    total,
		"limit":    limit,
		"offset":   offset,
	})
}

func handleGetTestGen(w http.ResponseWriter, r *http.Request) {
	testGenID := r.PathValue("id")
	if testGenID == "" {
		writeError(w, http.StatusBadRequest, "test gen id is required")
		return
	}

	userID := r.URL.Query().Get("userId")
	if userID == "" {
		writeError(w, http.StatusBadRequest, "userId is required")
		return
	}

	store := testgen.NewStore(db)
	storedTestGen, err := store.GetTestGen(testGenID, userID)
	if err != nil {
		log.Printf("get test gen: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to get test generation")
		return
	}

	if storedTestGen == nil {
		writeError(w, http.StatusNotFound, "test generation not found")
		return
	}

	writeJSON(w, http.StatusOK, storedTestGen)
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
		"service": "test-gen",
	})
}

func main() {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL is required")
	}

	authSecret = os.Getenv("AUTH_SECRET")
	if authSecret == "" {
		log.Fatal("AUTH_SECRET is required")
	}

	workerSemaphore = make(chan struct{}, 3)

	var err error
	db, err = sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}

	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.Ping(); err != nil {
		log.Fatalf("ping database: %v", err)
	}

	memoryURL := os.Getenv("MEMORY_SERVICE_URL")
	memorySecret := os.Getenv("MEMORY_SECRET")
	memoryClient := testgen.NewMemoryClient(memoryURL, memorySecret)

	relayURL := os.Getenv("RELAY_URL")
	relaySecret := os.Getenv("RELAY_SECRET")

	generator = testgen.NewGenerator(db, memoryClient, relayURL, relaySecret)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /test-gens", authMiddleware(handleCreateTestGen))
	mux.HandleFunc("GET /test-gens", authMiddleware(handleListTestGens))
	mux.HandleFunc("GET /test-gens/{id}", authMiddleware(handleGetTestGen))
	mux.HandleFunc("GET /health", handleHealth)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8095"
	}

	server := &http.Server{
		Addr:         ":" + port,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	var shutdownCancel context.CancelFunc
	shutdownCtx, shutdownCancel = context.WithCancel(context.Background())

	shutdownChan := make(chan os.Signal, 1)
	signal.Notify(shutdownChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		log.Printf("test-gen listening on :%s", port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	<-shutdownChan
	log.Println("shutting down test-gen")
	shutdownCancel()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	go func() {
		server.Shutdown(shutdownCtx)
	}()

	done := make(chan struct{})
	go func() {
		workerWg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Println("all workers completed")
	case <-shutdownCtx.Done():
		log.Println("shutdown timeout, forcing exit")
	}

	db.Close()
	log.Println("test-gen stopped")
}
