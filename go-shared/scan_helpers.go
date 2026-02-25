package goshared

import (
	"database/sql"
	"time"
)

func toNullString(s *string) sql.NullString {
	if s == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *s, Valid: true}
}

func toNullTime(t *time.Time) sql.NullTime {
	if t == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: *t, Valid: true}
}

func toNullFloat64(f *float64) sql.NullFloat64 {
	if f == nil {
		return sql.NullFloat64{}
	}
	return sql.NullFloat64{Float64: *f, Valid: true}
}

func toNullInt64(i *int) sql.NullInt64 {
	if i == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(*i), Valid: true}
}

func scanSession(row interface {
	Scan(dest ...interface{}) error
}, s *Session) error {
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

	err := row.Scan(
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
	)
	if err != nil {
		return err
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

	return nil
}

func scanSessionWithAgent(row interface {
	Scan(dest ...interface{}) error
}, s *SessionWithAgent) error {
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

	err := row.Scan(
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
	)
	if err != nil {
		return err
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

	return nil
}
