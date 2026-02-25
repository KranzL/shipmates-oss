package goshared_test

import (
	"context"
	"encoding/json"
	"testing"

	goshared "github.com/KranzL/shipmates-oss/go-shared"
	"github.com/KranzL/shipmates-oss/go-shared/testutil"
)

func TestCreateAgent(t *testing.T) {
	db := testutil.SetupTestDB(t)
	defer testutil.CleanupTables(t, db, "agents", "users")
	ctx := context.Background()

	user := testutil.CreateTestUser(t, db)
	agent := &goshared.Agent{
		Name:               "TestBot",
		Role:               "Engineer",
		Personality:        "Helpful",
		VoiceID:            "voice-123",
		ScopeConfig:        json.RawMessage(`{"directories":[],"languages":["go"],"repos":[]}`),
		CanDelegateTo:      []string{},
		MaxCostPerTask:     5.0,
		MaxDelegationDepth: 2,
		AutonomyLevel:      "standard",
	}

	created, err := db.CreateAgent(ctx, user.ID, agent)
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	if created.ID == "" {
		t.Fatal("expected non-empty agent ID")
	}
	if created.Name != "TestBot" {
		t.Fatalf("expected name 'TestBot', got '%s'", created.Name)
	}
	if created.UserID != user.ID {
		t.Fatalf("expected userID '%s', got '%s'", user.ID, created.UserID)
	}
}

func TestListAgents(t *testing.T) {
	db := testutil.SetupTestDB(t)
	defer testutil.CleanupTables(t, db, "agents", "users")
	ctx := context.Background()

	user := testutil.CreateTestUser(t, db)
	testutil.CreateTestAgent(t, db, user.ID)
	testutil.CreateTestAgent(t, db, user.ID)

	agents, err := db.ListAgents(ctx, user.ID)
	if err != nil {
		t.Fatalf("list agents: %v", err)
	}
	if len(agents) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(agents))
	}
}

func TestGetAgentByName(t *testing.T) {
	db := testutil.SetupTestDB(t)
	defer testutil.CleanupTables(t, db, "agents", "users")
	ctx := context.Background()

	user := testutil.CreateTestUser(t, db)
	created := testutil.CreateTestAgent(t, db, user.ID)

	found, err := db.GetAgentByName(ctx, user.ID, created.Name)
	if err != nil {
		t.Fatalf("get agent by name: %v", err)
	}
	if found == nil {
		t.Fatal("expected agent, got nil")
	}
	if found.ID != created.ID {
		t.Fatalf("expected ID '%s', got '%s'", created.ID, found.ID)
	}
}

func TestUpdateAgent(t *testing.T) {
	db := testutil.SetupTestDB(t)
	defer testutil.CleanupTables(t, db, "agents", "users")
	ctx := context.Background()

	user := testutil.CreateTestUser(t, db)
	created := testutil.CreateTestAgent(t, db, user.ID)

	created.Name = "UpdatedBot"
	created.Role = "Lead Engineer"

	updated, err := db.UpdateAgent(ctx, created.ID, user.ID, created)
	if err != nil {
		t.Fatalf("update agent: %v", err)
	}
	if updated.Name != "UpdatedBot" {
		t.Fatalf("expected name 'UpdatedBot', got '%s'", updated.Name)
	}
	if updated.Role != "Lead Engineer" {
		t.Fatalf("expected role 'Lead Engineer', got '%s'", updated.Role)
	}
}

func TestDeleteAgent(t *testing.T) {
	db := testutil.SetupTestDB(t)
	defer testutil.CleanupTables(t, db, "agents", "users")
	ctx := context.Background()

	user := testutil.CreateTestUser(t, db)
	created := testutil.CreateTestAgent(t, db, user.ID)

	err := db.DeleteAgent(ctx, created.ID, user.ID)
	if err != nil {
		t.Fatalf("delete agent: %v", err)
	}

	found, err := db.GetAgent(ctx, created.ID, user.ID)
	if err != nil {
		t.Fatalf("get agent after delete: %v", err)
	}
	if found != nil {
		t.Fatal("expected nil after delete, got agent")
	}
}

func TestCountAgents(t *testing.T) {
	db := testutil.SetupTestDB(t)
	defer testutil.CleanupTables(t, db, "agents", "users")
	ctx := context.Background()

	user := testutil.CreateTestUser(t, db)

	count, err := db.CountAgents(ctx, user.ID)
	if err != nil {
		t.Fatalf("count agents: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 agents, got %d", count)
	}

	testutil.CreateTestAgent(t, db, user.ID)
	testutil.CreateTestAgent(t, db, user.ID)

	count, err = db.CountAgents(ctx, user.ID)
	if err != nil {
		t.Fatalf("count agents: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 agents, got %d", count)
	}
}
