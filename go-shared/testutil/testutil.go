package testutil

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	goshared "github.com/KranzL/shipmates-oss/go-shared"
)

func SetupTestDB(t *testing.T) *goshared.DB {
	t.Helper()

	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping integration test")
	}

	db, err := goshared.NewDB(dbURL)
	if err != nil {
		t.Fatalf("connect to test db: %v", err)
	}

	t.Cleanup(func() {
		db.Close()
	})

	return db
}

func CleanupTables(t *testing.T, db *goshared.DB, tables ...string) {
	t.Helper()
	ctx := context.Background()
	for _, table := range tables {
		_, err := db.ExecContext(ctx, fmt.Sprintf("TRUNCATE %s CASCADE", table))
		if err != nil {
			t.Fatalf("truncate %s: %v", table, err)
		}
	}
}

func CreateTestUser(t *testing.T, db *goshared.DB) *goshared.User {
	t.Helper()
	ctx := context.Background()

	hash, err := goshared.HashPassword("TestPassword123!")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	email := fmt.Sprintf("test-%s@shipmates.dev", goshared.GenerateID())
	user, err := db.CreateUser(ctx, email, hash)
	if err != nil {
		t.Fatalf("create test user: %v", err)
	}

	return user
}

func CreateTestAgent(t *testing.T, db *goshared.DB, userID string) *goshared.Agent {
	t.Helper()
	ctx := context.Background()

	agent := &goshared.Agent{
		Name:               fmt.Sprintf("Agent-%s", goshared.GenerateID()[:8]),
		Role:               "Senior Engineer",
		Personality:        "Helpful and concise",
		VoiceID:            "test-voice-id",
		ScopeConfig:        json.RawMessage(`{"directories":[],"languages":[],"repos":[]}`),
		CanDelegateTo:      []string{},
		MaxCostPerTask:     10.0,
		MaxDelegationDepth: 3,
		AutonomyLevel:      "standard",
	}

	created, err := db.CreateAgent(ctx, userID, agent)
	if err != nil {
		t.Fatalf("create test agent: %v", err)
	}

	return created
}

func CreateTestSession(t *testing.T, db *goshared.DB, userID, agentID string) *goshared.Session {
	t.Helper()
	ctx := context.Background()

	session := &goshared.Session{
		UserID:        userID,
		AgentID:       agentID,
		Task:          "Test task " + goshared.GenerateID()[:8],
		Status:        goshared.SessionStatusPending,
		ExecutionMode: goshared.ExecutionModeContainer,
	}

	created, err := db.CreateSession(ctx, session)
	if err != nil {
		t.Fatalf("create test session: %v", err)
	}

	return created
}
