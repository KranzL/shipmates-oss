package main

import (
	"context"
	"database/sql"
	"encoding/json"
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
	"github.com/KranzL/shipmates-oss/ci-diagnosis/diagnosis"
)

var (
	db              *sql.DB
	diagnoser       *diagnosis.Diagnoser
	authSecret      string
	workerSemaphore chan struct{}
	workerWg        sync.WaitGroup
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
		if token != authSecret {
			writeError(w, http.StatusUnauthorized, "invalid token")
			return
		}
		next(w, r)
	}
}

func handleCreateDiagnosis(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 512*1024)

	var req diagnosis.DiagnosisRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Repo == "" || req.CommitSHA == "" || req.GithubToken == "" || req.APIKey == "" {
		writeError(w, http.StatusBadRequest, "missing required fields")
		return
	}

	select {
	case workerSemaphore <- struct{}{}:
		workerWg.Add(1)
		go func() {
			defer workerWg.Done()
			defer func() { <-workerSemaphore }()

			result, err := diagnoser.ProcessDiagnosis(req)
			if err != nil {
				log.Printf("process diagnosis for %s@%s: %v", req.Repo, req.CommitSHA, err)
				return
			}
			log.Printf("diagnosis %s completed for %s@%s: category=%s confidence=%.2f",
				result.ID, req.Repo, req.CommitSHA, result.ErrorCategory, result.Confidence)
		}()
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "processing"})
	default:
		w.Header().Set("Retry-After", "30")
		writeError(w, http.StatusServiceUnavailable, "diagnosis queue is full, try again later")
	}
}

func handleListDiagnoses(w http.ResponseWriter, r *http.Request) {
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

	store := diagnosis.NewStore(db)
	diagnoses, total, err := store.ListDiagnoses(userID, limit, offset)
	if err != nil {
		log.Printf("list diagnoses: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to list diagnoses")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"diagnoses": diagnoses,
		"total":     total,
		"limit":     limit,
		"offset":    offset,
	})
}

func handleGetDiagnosis(w http.ResponseWriter, r *http.Request) {
	diagnosisID := r.PathValue("id")
	if diagnosisID == "" {
		writeError(w, http.StatusBadRequest, "diagnosis id is required")
		return
	}

	userID := r.URL.Query().Get("userId")
	if userID == "" {
		writeError(w, http.StatusBadRequest, "userId is required")
		return
	}

	store := diagnosis.NewStore(db)
	storedDiag, err := store.GetDiagnosis(diagnosisID, userID)
	if err != nil {
		log.Printf("get diagnosis: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to get diagnosis")
		return
	}

	if storedDiag == nil {
		writeError(w, http.StatusNotFound, "diagnosis not found")
		return
	}

	writeJSON(w, http.StatusOK, storedDiag)
}

func handleApproveFix(w http.ResponseWriter, r *http.Request) {
	diagnosisID := r.PathValue("id")
	if diagnosisID == "" {
		writeError(w, http.StatusBadRequest, "diagnosis id is required")
		return
	}

	userID := r.URL.Query().Get("userId")
	if userID == "" {
		writeError(w, http.StatusBadRequest, "userId is required")
		return
	}

	store := diagnosis.NewStore(db)
	storedDiag, err := store.GetDiagnosis(diagnosisID, userID)
	if err != nil || storedDiag == nil {
		writeError(w, http.StatusNotFound, "diagnosis not found")
		return
	}

	if storedDiag.Status != "awaiting_approval" {
		writeError(w, http.StatusBadRequest, "diagnosis is not awaiting approval")
		return
	}

	if err := store.UpdateDiagnosisStatus(diagnosisID, "fix_approved"); err != nil {
		log.Printf("update diagnosis status: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to approve fix")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "fix_approved",
		"message": "Auto-fix approved. Implementation pending.",
	})
}

func handleGetAutoFix(w http.ResponseWriter, r *http.Request) {
	fixID := r.PathValue("id")
	if fixID == "" {
		writeError(w, http.StatusBadRequest, "auto-fix id is required")
		return
	}

	store := diagnosis.NewStore(db)
	autoFix, err := store.GetAutoFix(fixID)
	if err != nil {
		log.Printf("get auto fix: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to get auto-fix")
		return
	}

	if autoFix == nil {
		writeError(w, http.StatusNotFound, "auto-fix not found")
		return
	}

	writeJSON(w, http.StatusOK, autoFix)
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
		"service": "ci-diagnosis",
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

	workerSemaphore = make(chan struct{}, 10)

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
	memoryClient := diagnosis.NewMemoryClient(memoryURL, memorySecret)

	diagnoser = diagnosis.NewDiagnoser(db, memoryClient)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /diagnoses", authMiddleware(handleCreateDiagnosis))
	mux.HandleFunc("GET /diagnoses", authMiddleware(handleListDiagnoses))
	mux.HandleFunc("GET /diagnoses/{id}", authMiddleware(handleGetDiagnosis))
	mux.HandleFunc("POST /diagnoses/{id}/approve-fix", authMiddleware(handleApproveFix))
	mux.HandleFunc("GET /auto-fixes/{id}", authMiddleware(handleGetAutoFix))
	mux.HandleFunc("GET /health", handleHealth)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8093"
	}

	server := &http.Server{
		Addr:         ":" + port,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	shutdownChan := make(chan os.Signal, 1)
	signal.Notify(shutdownChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		log.Printf("ci-diagnosis listening on :%s", port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	<-shutdownChan
	log.Println("shutting down ci-diagnosis")

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
	log.Println("ci-diagnosis stopped")
}
