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
	"github.com/KranzL/shipmates-oss/pr-review/review"
)

var (
	db         *sql.DB
	reviewer   *review.Reviewer
	authSecret string
	workerPool chan struct{}
	wg         sync.WaitGroup
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

func authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			writeError(w, http.StatusUnauthorized, "missing authorization header")
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || parts[0] != "Bearer" {
			writeError(w, http.StatusUnauthorized, "invalid authorization format")
			return
		}

		if parts[1] != authSecret {
			writeError(w, http.StatusUnauthorized, "invalid token")
			return
		}

		next.ServeHTTP(w, r)
	})
}

func handleCreateReview(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 512*1024)

	var req review.ReviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Repo == "" || req.PRNumber == 0 || req.GithubToken == "" || req.APIKey == "" {
		writeError(w, http.StatusBadRequest, "missing required fields")
		return
	}

	select {
	case workerPool <- struct{}{}:
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-workerPool }()

			result, err := reviewer.ProcessReview(req)
			if err != nil {
				log.Printf("process review for %s#%d: %v", req.Repo, req.PRNumber, err)
				return
			}
			log.Printf("review %s completed for %s#%d: %d comments", result.ID, req.Repo, req.PRNumber, len(result.Comments))
		}()
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "processing"})
	default:
		writeError(w, http.StatusServiceUnavailable, "review queue is full, try again later")
	}
}

func handleListReviews(w http.ResponseWriter, r *http.Request) {
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

	store := review.NewStore(db)
	reviews, total, err := store.ListReviews(userID, limit, offset)
	if err != nil {
		log.Printf("list reviews: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to list reviews")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"reviews": reviews,
		"total":   total,
		"limit":   limit,
		"offset":  offset,
	})
}

func handleGetReview(w http.ResponseWriter, r *http.Request) {
	reviewID := r.PathValue("id")
	if reviewID == "" {
		writeError(w, http.StatusBadRequest, "review id is required")
		return
	}

	userID := r.URL.Query().Get("userId")
	if userID == "" {
		writeError(w, http.StatusBadRequest, "userId is required")
		return
	}

	store := review.NewStore(db)
	storedReview, comments, err := store.GetReview(reviewID, userID)
	if err != nil {
		log.Printf("get review: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to get review")
		return
	}

	if storedReview == nil {
		writeError(w, http.StatusNotFound, "review not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"review":   storedReview,
		"comments": comments,
	})
}

func handleFeedback(w http.ResponseWriter, r *http.Request) {
	reviewID := r.PathValue("id")
	if reviewID == "" {
		writeError(w, http.StatusBadRequest, "review id is required")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)

	var req review.FeedbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	userID := r.URL.Query().Get("userId")
	if userID == "" {
		writeError(w, http.StatusBadRequest, "userId is required")
		return
	}

	if req.FeedbackType != "approve" && req.FeedbackType != "reject" {
		writeError(w, http.StatusBadRequest, "feedbackType must be 'approve' or 'reject'")
		return
	}

	store := review.NewStore(db)
	if err := store.CreateFeedback(reviewID, userID, req.CommentID, req.FeedbackType, req.Reason); err != nil {
		log.Printf("create feedback: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to save feedback")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]bool{"saved": true})
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
		"service": "pr-review",
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

	workerPool = make(chan struct{}, 10)

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
	memoryClient := review.NewMemoryClient(memoryURL, memorySecret)

	reviewer = review.NewReviewer(db, memoryClient)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", handleHealth)

	authedMux := http.NewServeMux()
	authedMux.HandleFunc("POST /reviews", handleCreateReview)
	authedMux.HandleFunc("GET /reviews", handleListReviews)
	authedMux.HandleFunc("GET /reviews/{id}", handleGetReview)
	authedMux.HandleFunc("POST /reviews/{id}/feedback", handleFeedback)

	mux.Handle("/", authMiddleware(authedMux))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8092"
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
		log.Printf("pr-review listening on :%s", port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	<-shutdownChan
	log.Println("shutting down pr-review")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	go func() {
		server.Shutdown(shutdownCtx)
	}()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Println("all workers finished")
	case <-shutdownCtx.Done():
		log.Println("shutdown timeout, some workers may not have finished")
	}

	db.Close()
	log.Println("pr-review stopped")
}
