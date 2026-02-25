package diagnosis

import (
	"crypto/rand"
	"database/sql"
	"encoding/base32"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/lib/pq"
)

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func generateID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand failure: %v", err))
	}
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b))[:25]
}

func (s *Store) CreateDiagnosis(req DiagnosisRequest) (string, error) {
	id := generateID()
	now := time.Now()
	_, err := s.db.Exec(`
		INSERT INTO ci_diagnoses (id, user_id, agent_id, repo, pr_number, commit_sha, workflow_name, run_id, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'processing', $9, $9)
	`, id, req.UserID, req.AgentID, req.Repo, req.PRNumber, req.CommitSHA, req.WorkflowName, req.RunID, now)
	if err != nil {
		return "", fmt.Errorf("create diagnosis: %w", err)
	}
	return id, nil
}

func (s *Store) UpdateDiagnosisResult(id string, category ErrorCategory, diag string, rootCause string, confidence float64, inputTokens int, outputTokens int, status string) error {
	_, err := s.db.Exec(`
		UPDATE ci_diagnoses
		SET error_category = $1, diagnosis = $2, root_cause = $3, confidence = $4,
		    input_tokens = $5, output_tokens = $6, status = $7, updated_at = $8
		WHERE id = $9
	`, string(category), diag, rootCause, confidence, inputTokens, outputTokens, status, time.Now(), id)
	if err != nil {
		return fmt.Errorf("update diagnosis: %w", err)
	}
	return nil
}

func (s *Store) ListDiagnoses(userID string, limit int, offset int) ([]StoredDiagnosis, int, error) {
	var total int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM ci_diagnoses WHERE user_id = $1`, userID).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count diagnoses: %w", err)
	}

	rows, err := s.db.Query(`
		SELECT id, user_id, agent_id, repo, pr_number, commit_sha, workflow_name, run_id,
		       error_category, COALESCE(diagnosis, ''), COALESCE(root_cause, ''), confidence,
		       status, input_tokens, output_tokens, created_at, updated_at
		FROM ci_diagnoses
		WHERE user_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`, userID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list diagnoses: %w", err)
	}
	defer rows.Close()

	var diagnoses []StoredDiagnosis
	for rows.Next() {
		var d StoredDiagnosis
		var prNum sql.NullInt64
		if err := rows.Scan(&d.ID, &d.UserID, &d.AgentID, &d.Repo, &prNum,
			&d.CommitSHA, &d.WorkflowName, &d.RunID, &d.ErrorCategory,
			&d.Diagnosis, &d.RootCause, &d.Confidence, &d.Status,
			&d.InputTokens, &d.OutputTokens, &d.CreatedAt, &d.UpdatedAt); err != nil {
			log.Printf("scan diagnosis row: %v", err)
			continue
		}
		if prNum.Valid {
			d.PRNumber = int(prNum.Int64)
		}
		diagnoses = append(diagnoses, d)
	}

	return diagnoses, total, nil
}

func (s *Store) GetDiagnosis(id string, userID string) (*StoredDiagnosis, error) {
	var d StoredDiagnosis
	var prNum sql.NullInt64
	err := s.db.QueryRow(`
		SELECT id, user_id, agent_id, repo, pr_number, commit_sha, workflow_name, run_id,
		       error_category, COALESCE(diagnosis, ''), COALESCE(root_cause, ''), confidence,
		       status, input_tokens, output_tokens, created_at, updated_at
		FROM ci_diagnoses
		WHERE id = $1 AND user_id = $2
	`, id, userID).Scan(&d.ID, &d.UserID, &d.AgentID, &d.Repo, &prNum,
		&d.CommitSHA, &d.WorkflowName, &d.RunID, &d.ErrorCategory,
		&d.Diagnosis, &d.RootCause, &d.Confidence, &d.Status,
		&d.InputTokens, &d.OutputTokens, &d.CreatedAt, &d.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get diagnosis: %w", err)
	}
	if prNum.Valid {
		d.PRNumber = int(prNum.Int64)
	}
	return &d, nil
}

func (s *Store) UpdateDiagnosisStatus(id string, status string) error {
	_, err := s.db.Exec(`
		UPDATE ci_diagnoses
		SET status = $1, updated_at = $2
		WHERE id = $3
	`, status, time.Now(), id)
	if err != nil {
		return fmt.Errorf("update diagnosis status: %w", err)
	}
	return nil
}

func (s *Store) CreateAutoFix(diagnosisID string, repo string, branchName string, fixType string, filesChanged []string) (string, error) {
	id := generateID()
	now := time.Now()
	_, err := s.db.Exec(`
		INSERT INTO ci_auto_fixes (id, diagnosis_id, repo, branch_name, fix_type, files_changed, outcome, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, 'pending', $7, $7)
	`, id, diagnosisID, repo, branchName, fixType, pq.Array(filesChanged), now)
	if err != nil {
		return "", fmt.Errorf("create auto fix: %w", err)
	}
	return id, nil
}

func (s *Store) UpdateAutoFixOutcome(id string, outcome string, prNumber int, prURL string) error {
	_, err := s.db.Exec(`
		UPDATE ci_auto_fixes
		SET outcome = $1, pr_number = $2, pr_url = $3, updated_at = $4
		WHERE id = $5
	`, outcome, prNumber, prURL, time.Now(), id)
	if err != nil {
		return fmt.Errorf("update auto fix: %w", err)
	}
	return nil
}

func (s *Store) GetAutoFix(id string) (*StoredAutoFix, error) {
	var af StoredAutoFix
	var prNum sql.NullInt64
	var prURL sql.NullString
	err := s.db.QueryRow(`
		SELECT id, diagnosis_id, repo, branch_name, pr_number, pr_url, files_changed, fix_type, outcome, created_at, updated_at
		FROM ci_auto_fixes
		WHERE id = $1
	`, id).Scan(&af.ID, &af.DiagnosisID, &af.Repo, &af.BranchName, &prNum, &prURL,
		pq.Array(&af.FilesChanged), &af.FixType, &af.Outcome, &af.CreatedAt, &af.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get auto fix: %w", err)
	}
	if prNum.Valid {
		af.PRNumber = int(prNum.Int64)
	}
	if prURL.Valid {
		af.PRURL = prURL.String
	}
	return &af, nil
}
