package goshared

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/lib/pq"
)

type SessionChainTask struct {
	AgentID string
	Task    string
}

func (d *DB) CreateSession(ctx context.Context, s *Session) (*Session, error) {
	if s.ID == "" {
		s.ID = GenerateID()
	}
	now := time.Now()
	s.CreatedAt = now
	s.UpdatedAt = now

	var outputSQL sql.NullString
	if s.Output != nil {
		outputSQL = sql.NullString{String: *s.Output, Valid: true}
	}
	var branchNameSQL sql.NullString
	if s.BranchName != nil {
		branchNameSQL = sql.NullString{String: *s.BranchName, Valid: true}
	}
	var prURLSQL sql.NullString
	if s.PrURL != nil {
		prURLSQL = sql.NullString{String: *s.PrURL, Valid: true}
	}
	var repoSQL sql.NullString
	if s.Repo != nil {
		repoSQL = sql.NullString{String: *s.Repo, Valid: true}
	}
	var parentSessionIDSQL sql.NullString
	if s.ParentSessionID != nil {
		parentSessionIDSQL = sql.NullString{String: *s.ParentSessionID, Valid: true}
	}
	var chainIDSQL sql.NullString
	if s.ChainID != nil {
		chainIDSQL = sql.NullString{String: *s.ChainID, Valid: true}
	}
	var startedAtSQL sql.NullTime
	if s.StartedAt != nil {
		startedAtSQL = sql.NullTime{Time: *s.StartedAt, Valid: true}
	}
	var finishedAtSQL sql.NullTime
	if s.FinishedAt != nil {
		finishedAtSQL = sql.NullTime{Time: *s.FinishedAt, Valid: true}
	}
	var shareTokenSQL sql.NullString
	if s.ShareToken != nil {
		shareTokenSQL = sql.NullString{String: *s.ShareToken, Valid: true}
	}
	var shareTokenExpiresAtSQL sql.NullTime
	if s.ShareTokenExpiresAt != nil {
		shareTokenExpiresAtSQL = sql.NullTime{Time: *s.ShareTokenExpiresAt, Valid: true}
	}
	var platformThreadIDSQL sql.NullString
	if s.PlatformThreadID != nil {
		platformThreadIDSQL = sql.NullString{String: *s.PlatformThreadID, Valid: true}
	}
	var platformUserIDSQL sql.NullString
	if s.PlatformUserID != nil {
		platformUserIDSQL = sql.NullString{String: *s.PlatformUserID, Valid: true}
	}
	var scheduleIDSQL sql.NullString
	if s.ScheduleID != nil {
		scheduleIDSQL = sql.NullString{String: *s.ScheduleID, Valid: true}
	}

	_, err := d.db.ExecContext(ctx, `
		INSERT INTO sessions (id, user_id, agent_id, task, status, execution_mode, output, branch_name, pr_url, repo, parent_session_id, chain_id, position, started_at, finished_at, share_token, share_token_expires_at, coverage_data, starred, platform_thread_id, platform_user_id, schedule_id, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $23)
	`, s.ID, s.UserID, s.AgentID, s.Task, s.Status, s.ExecutionMode, outputSQL, branchNameSQL, prURLSQL, repoSQL, parentSessionIDSQL, chainIDSQL, s.Position, startedAtSQL, finishedAtSQL, shareTokenSQL, shareTokenExpiresAtSQL, s.CoverageData, s.Starred, platformThreadIDSQL, platformUserIDSQL, scheduleIDSQL, now)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}

	return s, nil
}

func (d *DB) GetSession(ctx context.Context, id, userID string) (*Session, error) {
	var s Session
	row := d.db.QueryRowContext(ctx, `
		SELECT id, user_id, agent_id, task, status, execution_mode, output, branch_name, pr_url, repo, parent_session_id, chain_id, position, started_at, finished_at, share_token, share_token_expires_at, coverage_data, starred, platform_thread_id, platform_user_id, schedule_id, created_at, updated_at
		FROM sessions
		WHERE id = $1 AND user_id = $2
	`, id, userID)

	err := scanSession(row, &s)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}

	return &s, nil
}

func (d *DB) GetSessionByID(ctx context.Context, id string) (*Session, error) {
	var s Session
	row := d.db.QueryRowContext(ctx, `
		SELECT id, user_id, agent_id, task, status, execution_mode, output, branch_name, pr_url, repo, parent_session_id, chain_id, position, started_at, finished_at, share_token, share_token_expires_at, coverage_data, starred, platform_thread_id, platform_user_id, schedule_id, created_at, updated_at
		FROM sessions
		WHERE id = $1
	`, id)

	err := scanSession(row, &s)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get session by id: %w", err)
	}

	return &s, nil
}

func (d *DB) GetSessionByShareToken(ctx context.Context, shareToken string) (*Session, error) {
	var s Session
	row := d.db.QueryRowContext(ctx, `
		SELECT id, user_id, agent_id, task, status, execution_mode, output, branch_name, pr_url, repo, parent_session_id, chain_id, position, started_at, finished_at, share_token, share_token_expires_at, coverage_data, starred, platform_thread_id, platform_user_id, schedule_id, created_at, updated_at
		FROM sessions
		WHERE share_token = $1 AND share_token_expires_at > $2
	`, shareToken, time.Now())

	err := scanSession(row, &s)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get session by share token: %w", err)
	}

	return &s, nil
}

func (d *DB) GetSharedSessionWithAgent(ctx context.Context, shareToken string) (*SessionWithAgent, error) {
	row := d.db.QueryRowContext(ctx, `
		SELECT sessions.id, sessions.user_id, sessions.agent_id, sessions.task, sessions.status, sessions.execution_mode, sessions.output, sessions.branch_name, sessions.pr_url, sessions.repo, sessions.parent_session_id, sessions.chain_id, sessions.position, sessions.started_at, sessions.finished_at, sessions.share_token, sessions.share_token_expires_at, sessions.coverage_data, sessions.starred, sessions.platform_thread_id, sessions.platform_user_id, sessions.schedule_id, sessions.created_at, sessions.updated_at, agents.name AS agent_name
		FROM sessions
		JOIN agents ON agents.id = sessions.agent_id
		WHERE sessions.share_token = $1
	`, shareToken)

	var s SessionWithAgent
	err := scanSessionWithAgent(row, &s)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get shared session with agent: %w", err)
	}

	return &s, nil
}

func (d *DB) GetWaitingSessionByThreadID(ctx context.Context, userID, threadID string) (*SessionWithAgent, error) {
	row := d.db.QueryRowContext(ctx, `
		SELECT sessions.id, sessions.user_id, sessions.agent_id, sessions.task, sessions.status, sessions.execution_mode, sessions.output, sessions.branch_name, sessions.pr_url, sessions.repo, sessions.parent_session_id, sessions.chain_id, sessions.position, sessions.started_at, sessions.finished_at, sessions.share_token, sessions.share_token_expires_at, sessions.coverage_data, sessions.starred, sessions.platform_thread_id, sessions.platform_user_id, sessions.schedule_id, sessions.created_at, sessions.updated_at, agents.name AS agent_name
		FROM sessions
		JOIN agents ON agents.id = sessions.agent_id
		WHERE sessions.user_id = $1 AND sessions.status = $2 AND sessions.platform_thread_id = $3
		LIMIT 1
	`, userID, SessionStatusWaitingForClarification, threadID)

	var s SessionWithAgent
	err := scanSessionWithAgent(row, &s)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get waiting session by thread id: %w", err)
	}

	return &s, nil
}

func (d *DB) ListSessions(ctx context.Context, userID string, limit, offset int, status *string, agentID *string, starred *bool) ([]SessionWithAgent, int, error) {
	query := `
		SELECT sessions.id, sessions.user_id, sessions.agent_id, sessions.task, sessions.status, sessions.execution_mode, sessions.output, sessions.branch_name, sessions.pr_url, sessions.repo, sessions.parent_session_id, sessions.chain_id, sessions.position, sessions.started_at, sessions.finished_at, sessions.share_token, sessions.share_token_expires_at, sessions.coverage_data, sessions.starred, sessions.platform_thread_id, sessions.platform_user_id, sessions.schedule_id, sessions.created_at, sessions.updated_at, agents.name AS agent_name, COUNT(*) OVER() AS total_count
		FROM sessions
		JOIN agents ON agents.id = sessions.agent_id
		WHERE sessions.user_id = $1
	`
	args := []interface{}{userID}
	argPos := 2

	if status != nil {
		query += fmt.Sprintf(" AND sessions.status = $%d", argPos)
		args = append(args, *status)
		argPos++
	}
	if agentID != nil {
		query += fmt.Sprintf(" AND sessions.agent_id = $%d", argPos)
		args = append(args, *agentID)
		argPos++
	}
	if starred != nil {
		query += fmt.Sprintf(" AND sessions.starred = $%d", argPos)
		args = append(args, *starred)
		argPos++
	}

	query += " ORDER BY sessions.created_at DESC"
	query += fmt.Sprintf(" LIMIT $%d OFFSET $%d", argPos, argPos+1)
	args = append(args, limit, offset)

	rows, err := d.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()

	sessions := []SessionWithAgent{}
	var totalCount int
	for rows.Next() {
		var s SessionWithAgent
		var outputSQL sql.NullString
		var branchNameSQL sql.NullString
		var prURLSQL sql.NullString
		var repoSQL sql.NullString
		var parentSessionIDSQL sql.NullString
		var chainIDSQL sql.NullString
		var startedAtSQL sql.NullTime
		var finishedAtSQL sql.NullTime
		var shareTokenSQL sql.NullString
		var shareTokenExpiresAtSQL sql.NullTime
		var platformThreadIDSQL sql.NullString
		var platformUserIDSQL sql.NullString
		var scheduleIDSQL sql.NullString
		var coverageDataSQL []byte

		err := rows.Scan(
			&s.ID,
			&s.UserID,
			&s.AgentID,
			&s.Task,
			&s.Status,
			&s.ExecutionMode,
			&outputSQL,
			&branchNameSQL,
			&prURLSQL,
			&repoSQL,
			&parentSessionIDSQL,
			&chainIDSQL,
			&s.Position,
			&startedAtSQL,
			&finishedAtSQL,
			&shareTokenSQL,
			&shareTokenExpiresAtSQL,
			&coverageDataSQL,
			&s.Starred,
			&platformThreadIDSQL,
			&platformUserIDSQL,
			&scheduleIDSQL,
			&s.CreatedAt,
			&s.UpdatedAt,
			&s.AgentName,
			&totalCount,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("scan session: %w", err)
		}

		s.CoverageData = coverageDataSQL
		if outputSQL.Valid {
			s.Output = &outputSQL.String
		}
		if branchNameSQL.Valid {
			s.BranchName = &branchNameSQL.String
		}
		if prURLSQL.Valid {
			s.PrURL = &prURLSQL.String
		}
		if repoSQL.Valid {
			s.Repo = &repoSQL.String
		}
		if parentSessionIDSQL.Valid {
			s.ParentSessionID = &parentSessionIDSQL.String
		}
		if chainIDSQL.Valid {
			s.ChainID = &chainIDSQL.String
		}
		if startedAtSQL.Valid {
			s.StartedAt = &startedAtSQL.Time
		}
		if finishedAtSQL.Valid {
			s.FinishedAt = &finishedAtSQL.Time
		}
		if shareTokenSQL.Valid {
			s.ShareToken = &shareTokenSQL.String
		}
		if shareTokenExpiresAtSQL.Valid {
			s.ShareTokenExpiresAt = &shareTokenExpiresAtSQL.Time
		}
		if platformThreadIDSQL.Valid {
			s.PlatformThreadID = &platformThreadIDSQL.String
		}
		if platformUserIDSQL.Valid {
			s.PlatformUserID = &platformUserIDSQL.String
		}
		if scheduleIDSQL.Valid {
			s.ScheduleID = &scheduleIDSQL.String
		}

		sessions = append(sessions, s)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("list sessions rows: %w", err)
	}

	if len(sessions) == 0 {
		totalCount = 0
	}

	return sessions, totalCount, nil
}

func (d *DB) UpdateSessionStatus(ctx context.Context, id string, status SessionStatus) error {
	now := time.Now()
	var query string

	switch status {
	case SessionStatusRunning:
		query = `
			UPDATE sessions
			SET status = $1, started_at = $2, updated_at = $2
			WHERE id = $3
		`
		_, err := d.db.ExecContext(ctx, query, status, now, id)
		if err != nil {
			return fmt.Errorf("update session status: %w", err)
		}
	case SessionStatusDone, SessionStatusError, SessionStatusPartial, SessionStatusCancelled:
		query = `
			UPDATE sessions
			SET status = $1, finished_at = $2, updated_at = $2
			WHERE id = $3
		`
		_, err := d.db.ExecContext(ctx, query, status, now, id)
		if err != nil {
			return fmt.Errorf("update session status: %w", err)
		}
	default:
		query = `
			UPDATE sessions
			SET status = $1, updated_at = $2
			WHERE id = $3
		`
		_, err := d.db.ExecContext(ctx, query, status, now, id)
		if err != nil {
			return fmt.Errorf("update session status: %w", err)
		}
	}

	return nil
}

func (d *DB) UpdateSessionOutput(ctx context.Context, id, output string) error {
	_, err := d.db.ExecContext(ctx, `
		UPDATE sessions
		SET output = $1, updated_at = $2
		WHERE id = $3
	`, output, time.Now(), id)
	if err != nil {
		return fmt.Errorf("update session output: %w", err)
	}
	return nil
}

func (d *DB) UpdateSessionResult(ctx context.Context, id string, status SessionStatus, output, branchName, prURL *string) error {
	now := time.Now()
	var outputSQL sql.NullString
	if output != nil {
		outputSQL = sql.NullString{String: *output, Valid: true}
	}
	var branchNameSQL sql.NullString
	if branchName != nil {
		branchNameSQL = sql.NullString{String: *branchName, Valid: true}
	}
	var prURLSQL sql.NullString
	if prURL != nil {
		prURLSQL = sql.NullString{String: *prURL, Valid: true}
	}

	_, err := d.db.ExecContext(ctx, `
		UPDATE sessions
		SET status = $1, output = $2, branch_name = $3, pr_url = $4, finished_at = $5, updated_at = $5
		WHERE id = $6
	`, status, outputSQL, branchNameSQL, prURLSQL, now, id)
	if err != nil {
		return fmt.Errorf("update session result: %w", err)
	}
	return nil
}

func (d *DB) UpdateSessionResultIfNotTerminal(ctx context.Context, id string, status SessionStatus, output, branchName, prURL *string) (bool, error) {
	now := time.Now()
	var outputSQL sql.NullString
	if output != nil {
		outputSQL = sql.NullString{String: *output, Valid: true}
	}
	var branchNameSQL sql.NullString
	if branchName != nil {
		branchNameSQL = sql.NullString{String: *branchName, Valid: true}
	}
	var prURLSQL sql.NullString
	if prURL != nil {
		prURLSQL = sql.NullString{String: *prURL, Valid: true}
	}

	result, err := d.db.ExecContext(ctx, `
		UPDATE sessions
		SET status = $1, output = $2, branch_name = $3, pr_url = $4, finished_at = $5, updated_at = $5
		WHERE id = $6 AND status NOT IN ($7, $8, $9)
	`, status, outputSQL, branchNameSQL, prURLSQL, now, id, SessionStatusCancelled, SessionStatusError, SessionStatusDone)
	if err != nil {
		return false, fmt.Errorf("update session result if not terminal: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("get rows affected: %w", err)
	}
	return rowsAffected > 0, nil
}

func (d *DB) UpdateSessionStar(ctx context.Context, id, userID string, starred bool) error {
	_, err := d.db.ExecContext(ctx, `
		UPDATE sessions
		SET starred = $1, updated_at = $2
		WHERE id = $3 AND user_id = $4
	`, starred, time.Now(), id, userID)
	if err != nil {
		return fmt.Errorf("update session star: %w", err)
	}
	return nil
}

func (d *DB) UpdateSessionShareToken(ctx context.Context, id, userID, shareToken string, expiresAt time.Time) error {
	_, err := d.db.ExecContext(ctx, `
		UPDATE sessions
		SET share_token = $1, share_token_expires_at = $2, updated_at = $3
		WHERE id = $4 AND user_id = $5
	`, shareToken, expiresAt, time.Now(), id, userID)
	if err != nil {
		return fmt.Errorf("update session share token: %w", err)
	}
	return nil
}

func (d *DB) DeleteSessionShareToken(ctx context.Context, id, userID string) error {
	_, err := d.db.ExecContext(ctx, `
		UPDATE sessions
		SET share_token = NULL, share_token_expires_at = NULL, updated_at = $1
		WHERE id = $2 AND user_id = $3
	`, time.Now(), id, userID)
	if err != nil {
		return fmt.Errorf("delete session share token: %w", err)
	}
	return nil
}

func (d *DB) CancelSession(ctx context.Context, id, userID string) error {
	_, err := d.db.ExecContext(ctx, `
		UPDATE sessions
		SET status = $1, updated_at = $2
		WHERE id = $3 AND user_id = $4
	`, SessionStatusCancelling, time.Now(), id, userID)
	if err != nil {
		return fmt.Errorf("cancel session: %w", err)
	}
	return nil
}

func (d *DB) DeleteSession(ctx context.Context, id, userID string) error {
	_, err := d.db.ExecContext(ctx, `
		DELETE FROM sessions
		WHERE id = $1 AND user_id = $2
	`, id, userID)
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

func (d *DB) BulkDeleteSessions(ctx context.Context, userID string, ids []string) (int, error) {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `
		DELETE FROM sessions
		WHERE id = ANY($1) AND user_id = $2
	`, pq.Array(ids), userID)
	if err != nil {
		return 0, fmt.Errorf("bulk delete sessions: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("bulk delete rows affected: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit transaction: %w", err)
	}

	return int(rows), nil
}

func (d *DB) GetNonTerminalSessions(ctx context.Context, userID string) ([]SessionWithAgent, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT sessions.id, sessions.user_id, sessions.agent_id, sessions.task, sessions.status, sessions.execution_mode, sessions.output, sessions.branch_name, sessions.pr_url, sessions.repo, sessions.parent_session_id, sessions.chain_id, sessions.position, sessions.started_at, sessions.finished_at, sessions.share_token, sessions.share_token_expires_at, sessions.coverage_data, sessions.starred, sessions.platform_thread_id, sessions.platform_user_id, sessions.schedule_id, sessions.created_at, sessions.updated_at, agents.name AS agent_name
		FROM sessions
		JOIN agents ON agents.id = sessions.agent_id
		WHERE sessions.user_id = $1 AND (
			sessions.status IN ($2, $3, $4, $5)
			OR (sessions.finished_at >= NOW() - INTERVAL '5 minutes' AND sessions.status IN ($6, $7, $8))
		)
		ORDER BY sessions.created_at DESC
	`, userID, SessionStatusPending, SessionStatusRunning, SessionStatusWaitingForClarification, SessionStatusCancelling, SessionStatusDone, SessionStatusError, SessionStatusPartial)
	if err != nil {
		return nil, fmt.Errorf("get non-terminal sessions: %w", err)
	}
	defer rows.Close()

	sessions := []SessionWithAgent{}
	for rows.Next() {
		var s SessionWithAgent
		err := scanSessionWithAgent(rows, &s)
		if err != nil {
			return nil, fmt.Errorf("scan non-terminal session: %w", err)
		}
		sessions = append(sessions, s)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("get non-terminal sessions rows: %w", err)
	}

	return sessions, nil
}

func (d *DB) CreateSessionChain(ctx context.Context, userID string, tasks []SessionChainTask) ([]string, error) {
	if len(tasks) == 0 {
		return []string{}, nil
	}

	chainID := GenerateID()
	now := time.Now()
	sessionIDs := make([]string, len(tasks))

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	for i, task := range tasks {
		id := GenerateID()
		sessionIDs[i] = id

		var parentSessionIDSQL sql.NullString
		if i > 0 {
			parentSessionIDSQL = sql.NullString{String: sessionIDs[i-1], Valid: true}
		}

		_, err := tx.ExecContext(ctx, `
			INSERT INTO sessions (id, user_id, agent_id, task, status, execution_mode, chain_id, position, parent_session_id, starred, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $11)
		`, id, userID, task.AgentID, task.Task, SessionStatusPending, ExecutionModeContainer, chainID, i, parentSessionIDSQL, false, now)
		if err != nil {
			return nil, fmt.Errorf("insert session %d: %w", i, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	return sessionIDs, nil
}

func (d *DB) CountActiveSessions(ctx context.Context, userID string) (int, error) {
	var count int
	err := d.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM sessions
		WHERE user_id = $1 AND status IN ($2, $3, $4, $5)
	`, userID, SessionStatusPending, SessionStatusRunning, SessionStatusWaitingForClarification, SessionStatusCancelling).Scan(&count)
	return count, err
}

func (d *DB) CompleteSessionInChain(ctx context.Context, sessionID, userID string, status SessionStatus, output, branchName, prURL *string) (*Session, error) {
	now := time.Now()

	var outputSQL sql.NullString
	if output != nil {
		outputSQL = sql.NullString{String: *output, Valid: true}
	}
	var branchNameSQL sql.NullString
	if branchName != nil {
		branchNameSQL = sql.NullString{String: *branchName, Valid: true}
	}
	var prURLSQL sql.NullString
	if prURL != nil {
		prURLSQL = sql.NullString{String: *prURL, Valid: true}
	}

	_, err := d.db.ExecContext(ctx, `
		UPDATE sessions
		SET status = $1, output = $2, branch_name = $3, pr_url = $4, finished_at = $5, updated_at = $5
		WHERE id = $6 AND user_id = $7
	`, status, outputSQL, branchNameSQL, prURLSQL, now, sessionID, userID)
	if err != nil {
		return nil, fmt.Errorf("update completed session: %w", err)
	}

	var nextSession Session
	row := d.db.QueryRowContext(ctx, `
		SELECT id, user_id, agent_id, task, status, execution_mode, output, branch_name, pr_url, repo, parent_session_id, chain_id, position, started_at, finished_at, share_token, share_token_expires_at, coverage_data, starred, platform_thread_id, platform_user_id, schedule_id, created_at, updated_at
		FROM sessions
		WHERE chain_id = (SELECT chain_id FROM sessions WHERE id = $1)
		AND position = (SELECT position + 1 FROM sessions WHERE id = $1)
		AND status = $2
	`, sessionID, SessionStatusPending)

	err = scanSession(row, &nextSession)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get next session: %w", err)
	}

	return &nextSession, nil
}

func (d *DB) GetChainStatus(ctx context.Context, chainID, userID string) ([]SessionWithAgent, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT sessions.id, sessions.user_id, sessions.agent_id, sessions.task, sessions.status, sessions.execution_mode, sessions.output, sessions.branch_name, sessions.pr_url, sessions.repo, sessions.parent_session_id, sessions.chain_id, sessions.position, sessions.started_at, sessions.finished_at, sessions.share_token, sessions.share_token_expires_at, sessions.coverage_data, sessions.starred, sessions.platform_thread_id, sessions.platform_user_id, sessions.schedule_id, sessions.created_at, sessions.updated_at, agents.name AS agent_name
		FROM sessions
		JOIN agents ON agents.id = sessions.agent_id
		WHERE sessions.chain_id = $1 AND sessions.user_id = $2
		ORDER BY sessions.position ASC
	`, chainID, userID)
	if err != nil {
		return nil, fmt.Errorf("query chain status: %w", err)
	}
	defer rows.Close()

	chainSessions := []SessionWithAgent{}
	for rows.Next() {
		var cs SessionWithAgent
		err := scanSessionWithAgent(rows, &cs)
		if err != nil {
			return nil, fmt.Errorf("scan chain session: %w", err)
		}
		chainSessions = append(chainSessions, cs)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate chain sessions: %w", err)
	}

	return chainSessions, nil
}

func (d *DB) GetSessionsByStatus(ctx context.Context, sessionIDs []string, status SessionStatus) ([]string, error) {
	if len(sessionIDs) == 0 {
		return []string{}, nil
	}

	query := `SELECT id FROM sessions WHERE id = ANY($1) AND status = $2`
	rows, err := d.db.QueryContext(ctx, query, pq.Array(sessionIDs), status)
	if err != nil {
		return nil, fmt.Errorf("get sessions by status: %w", err)
	}
	defer rows.Close()

	result := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan session id: %w", err)
		}
		result = append(result, id)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("get sessions by status rows: %w", err)
	}

	return result, nil
}

func (d *DB) UpdateSessionPlatformThreadID(ctx context.Context, sessionID, threadID string) error {
	_, err := d.db.ExecContext(ctx, `
		UPDATE sessions
		SET platform_thread_id = $1, updated_at = $2
		WHERE id = $3
	`, threadID, time.Now(), sessionID)
	if err != nil {
		return fmt.Errorf("update session platform thread id: %w", err)
	}
	return nil
}
