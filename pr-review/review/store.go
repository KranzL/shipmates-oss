package review

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"
)

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) CreateReview(userID string, agentID string, repo string, prNumber int) (string, error) {
	id := GenerateID()
	now := time.Now()
	_, err := s.db.Exec(`
		INSERT INTO pr_reviews (id, user_id, agent_id, repo, pr_number, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, 'processing', $6, $6)
	`, id, userID, agentID, repo, prNumber, now)
	if err != nil {
		return "", fmt.Errorf("create review: %w", err)
	}
	return id, nil
}

func (s *Store) UpdateReviewResult(reviewID string, severityCounts map[string]int, inputTokens int, outputTokens int, status string) error {
	var countsJSON []byte
	var err error
	if severityCounts == nil {
		countsJSON = []byte("{}")
	} else {
		countsJSON, err = json.Marshal(severityCounts)
		if err != nil {
			return fmt.Errorf("marshal severity counts: %w", err)
		}
	}

	_, err = s.db.Exec(`
		UPDATE pr_reviews
		SET severity_counts = $1, input_tokens = $2, output_tokens = $3, status = $4, updated_at = $5
		WHERE id = $6
	`, string(countsJSON), inputTokens, outputTokens, status, time.Now(), reviewID)
	if err != nil {
		return fmt.Errorf("update review: %w", err)
	}

	return nil
}

func (s *Store) CreateComment(reviewID string, comment ReviewComment) (string, error) {
	id := GenerateID()
	_, err := s.db.Exec(`
		INSERT INTO review_comments (id, review_id, file_path, line_number, severity, category, body, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, id, reviewID, comment.FilePath, comment.Line, string(comment.Severity), string(comment.Category), comment.Body, time.Now())
	if err != nil {
		return "", fmt.Errorf("create comment: %w", err)
	}
	return id, nil
}

func (s *Store) ListReviews(userID string, limit int, offset int) ([]StoredReview, int, error) {
	var total int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM pr_reviews WHERE user_id = $1`, userID).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count reviews: %w", err)
	}

	rows, err := s.db.Query(`
		SELECT id, user_id, agent_id, repo, pr_number, severity_counts, status, input_tokens, output_tokens, created_at, updated_at
		FROM pr_reviews
		WHERE user_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`, userID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list reviews: %w", err)
	}
	defer rows.Close()

	var reviews []StoredReview
	for rows.Next() {
		var r StoredReview
		var countsJSON sql.NullString
		if err := rows.Scan(&r.ID, &r.UserID, &r.AgentID, &r.Repo, &r.PRNumber, &countsJSON, &r.Status, &r.InputTokens, &r.OutputTokens, &r.CreatedAt, &r.UpdatedAt); err != nil {
			log.Printf("scan review row: %v", err)
			continue
		}
		if countsJSON.Valid {
			if err := json.Unmarshal([]byte(countsJSON.String), &r.SeverityCounts); err != nil {
				log.Printf("unmarshal severity counts: %v", err)
				r.SeverityCounts = make(map[string]int)
			}
		}
		if r.SeverityCounts == nil {
			r.SeverityCounts = make(map[string]int)
		}
		reviews = append(reviews, r)
	}

	return reviews, total, nil
}

func (s *Store) GetReview(reviewID string, userID string) (*StoredReview, []StoredComment, error) {
	var r StoredReview
	var countsJSON sql.NullString
	err := s.db.QueryRow(`
		SELECT id, user_id, agent_id, repo, pr_number, severity_counts, status, input_tokens, output_tokens, created_at, updated_at
		FROM pr_reviews
		WHERE id = $1 AND user_id = $2
	`, reviewID, userID).Scan(&r.ID, &r.UserID, &r.AgentID, &r.Repo, &r.PRNumber, &countsJSON, &r.Status, &r.InputTokens, &r.OutputTokens, &r.CreatedAt, &r.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("get review: %w", err)
	}

	if countsJSON.Valid {
		if err := json.Unmarshal([]byte(countsJSON.String), &r.SeverityCounts); err != nil {
			log.Printf("unmarshal severity counts: %v", err)
			r.SeverityCounts = make(map[string]int)
		}
	}
	if r.SeverityCounts == nil {
		r.SeverityCounts = make(map[string]int)
	}

	rows, err := s.db.Query(`
		SELECT id, review_id, file_path, line_number, severity, category, body, github_comment_id, created_at
		FROM review_comments
		WHERE review_id = $1
		ORDER BY file_path, line_number
	`, reviewID)
	if err != nil {
		log.Printf("query comments: %v", err)
		return &r, nil, fmt.Errorf("query comments: %w", err)
	}
	defer rows.Close()

	var comments []StoredComment
	for rows.Next() {
		var c StoredComment
		if err := rows.Scan(&c.ID, &c.ReviewID, &c.FilePath, &c.LineNumber, &c.Severity, &c.Category, &c.Body, &c.GithubCommentID, &c.CreatedAt); err != nil {
			log.Printf("scan comment row: %v", err)
			continue
		}
		comments = append(comments, c)
	}

	return &r, comments, nil
}

func (s *Store) CreateFeedback(reviewID string, userID string, commentID string, feedbackType string, reason string) error {
	id := GenerateID()
	_, err := s.db.Exec(`
		INSERT INTO review_feedback (id, review_id, comment_id, user_id, feedback_type, reason, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, id, reviewID, commentID, userID, feedbackType, reason, time.Now())
	if err != nil {
		return fmt.Errorf("create feedback: %w", err)
	}
	return nil
}
