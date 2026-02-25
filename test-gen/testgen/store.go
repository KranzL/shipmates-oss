package testgen

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/lib/pq"
)

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) CreateTestGen(req TestGenRequest) (string, error) {
	id := GenerateID()
	now := time.Now()
	_, err := s.db.Exec(`
		INSERT INTO test_gens (id, user_id, agent_id, repo, task, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, 'pending', $6, $6)
	`, id, req.UserID, req.AgentID, req.Repo, req.Task, now)
	if err != nil {
		return "", fmt.Errorf("create test gen: %w", err)
	}
	return id, nil
}

func (s *Store) UpdateTestGenResult(id string, result *TestGenResult) error {
	coverageData := map[string]interface{}{
		"before": map[string]float64{
			"lines": result.BaselineCoverage,
		},
		"after": map[string]float64{
			"lines": result.FinalCoverage,
		},
		"iterations":       result.Iterations,
		"testFilesCreated": result.TestsCreated,
		"topFileDeltas":    []interface{}{},
	}

	coverageJSON, err := json.Marshal(coverageData)
	if err != nil {
		return fmt.Errorf("marshal coverage data: %w", err)
	}

	_, err = s.db.Exec(`
		UPDATE test_gens
		SET final_coverage = $1, pr_url = $2, branch_name = $3, tests_created = $4,
		    coverage_data = $5, input_tokens = $6, output_tokens = $7,
		    iterations = $8, baseline_coverage = $9, status = $10, updated_at = $11
		WHERE id = $12
	`, result.FinalCoverage, result.PRURL, result.BranchName, pq.Array(result.TestsCreated),
		coverageJSON, result.InputTokens, result.OutputTokens,
		result.Iterations, result.BaselineCoverage, "done", time.Now(), id)
	if err != nil {
		return fmt.Errorf("update test gen result: %w", err)
	}
	return nil
}

func (s *Store) UpdateTestGenStatus(id string, status string) error {
	_, err := s.db.Exec(`
		UPDATE test_gens
		SET status = $1, updated_at = $2
		WHERE id = $3
	`, status, time.Now(), id)
	if err != nil {
		return fmt.Errorf("update test gen status: %w", err)
	}
	return nil
}

func (s *Store) ListTestGens(userID string, limit int, offset int) ([]StoredTestGen, int, error) {
	var total int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM test_gens WHERE user_id = $1`, userID).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count test gens: %w", err)
	}

	rows, err := s.db.Query(`
		SELECT id, user_id, agent_id, repo, task, status, iterations,
		       baseline_coverage, final_coverage, tests_created, branch_name, pr_url,
		       input_tokens, output_tokens, created_at, updated_at
		FROM test_gens
		WHERE user_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`, userID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list test gens: %w", err)
	}
	defer rows.Close()

	var testGens []StoredTestGen
	for rows.Next() {
		var tg StoredTestGen
		var branchName sql.NullString
		var prURL sql.NullString
		if err := rows.Scan(&tg.ID, &tg.UserID, &tg.AgentID, &tg.Repo, &tg.Task, &tg.Status,
			&tg.Iterations, &tg.BaselineCoverage, &tg.FinalCoverage, pq.Array(&tg.TestsCreated),
			&branchName, &prURL, &tg.InputTokens, &tg.OutputTokens, &tg.CreatedAt, &tg.UpdatedAt); err != nil {
			log.Printf("scan test gen row: %v", err)
			continue
		}
		if branchName.Valid {
			tg.BranchName = branchName.String
		}
		if prURL.Valid {
			tg.PRURL = prURL.String
		}
		testGens = append(testGens, tg)
	}

	return testGens, total, nil
}

func (s *Store) GetTestGen(id string, userID string) (*StoredTestGen, error) {
	var tg StoredTestGen
	var branchName sql.NullString
	var prURL sql.NullString
	err := s.db.QueryRow(`
		SELECT id, user_id, agent_id, repo, task, status, iterations,
		       baseline_coverage, final_coverage, tests_created, branch_name, pr_url,
		       input_tokens, output_tokens, created_at, updated_at
		FROM test_gens
		WHERE id = $1 AND user_id = $2
	`, id, userID).Scan(&tg.ID, &tg.UserID, &tg.AgentID, &tg.Repo, &tg.Task, &tg.Status,
		&tg.Iterations, &tg.BaselineCoverage, &tg.FinalCoverage, pq.Array(&tg.TestsCreated),
		&branchName, &prURL, &tg.InputTokens, &tg.OutputTokens, &tg.CreatedAt, &tg.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get test gen: %w", err)
	}
	if branchName.Valid {
		tg.BranchName = branchName.String
	}
	if prURL.Valid {
		tg.PRURL = prURL.String
	}
	return &tg, nil
}

func (s *Store) CreateIteration(testGenID string, iteration int, tests []string, covTotal float64, covDelta float64, inputTokens int, outputTokens int) error {
	id := GenerateID()
	now := time.Now()
	_, err := s.db.Exec(`
		INSERT INTO test_gen_iterations (id, test_gen_id, iteration, tests_generated, coverage_total, coverage_delta, input_tokens, output_tokens, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, id, testGenID, iteration, pq.Array(tests), covTotal, covDelta, inputTokens, outputTokens, now)
	if err != nil {
		return fmt.Errorf("create iteration: %w", err)
	}
	return nil
}
