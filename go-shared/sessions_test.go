package goshared_test

import (
	"context"
	"testing"
	"time"

	goshared "github.com/KranzL/shipmates-oss/go-shared"
	"github.com/KranzL/shipmates-oss/go-shared/testutil"
)

func TestCreateSession(t *testing.T) {
	db := testutil.SetupTestDB(t)
	defer testutil.CleanupTables(t, db, "sessions", "agents", "users")
	ctx := context.Background()

	user := testutil.CreateTestUser(t, db)
	agent := testutil.CreateTestAgent(t, db, user.ID)

	session := &goshared.Session{
		UserID:        user.ID,
		AgentID:       agent.ID,
		Task:          "Build a feature",
		Status:        goshared.SessionStatusPending,
		ExecutionMode: goshared.ExecutionModeContainer,
	}

	created, err := db.CreateSession(ctx, session)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if created.ID == "" {
		t.Fatal("expected non-empty session ID")
	}
	if created.Task != "Build a feature" {
		t.Fatalf("expected task 'Build a feature', got '%s'", created.Task)
	}
	if created.Status != goshared.SessionStatusPending {
		t.Fatalf("expected status 'pending', got '%s'", created.Status)
	}
}

func TestListSessionsPagination(t *testing.T) {
	db := testutil.SetupTestDB(t)
	defer testutil.CleanupTables(t, db, "sessions", "agents", "users")
	ctx := context.Background()

	user := testutil.CreateTestUser(t, db)
	agent := testutil.CreateTestAgent(t, db, user.ID)

	for i := 0; i < 5; i++ {
		testutil.CreateTestSession(t, db, user.ID, agent.ID)
	}

	sessions, total, err := db.ListSessions(ctx, user.ID, 3, 0, nil, nil, nil)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != 3 {
		t.Fatalf("expected 3 sessions, got %d", len(sessions))
	}
	if total != 5 {
		t.Fatalf("expected total 5, got %d", total)
	}

	sessions, _, err = db.ListSessions(ctx, user.ID, 3, 3, nil, nil, nil)
	if err != nil {
		t.Fatalf("list sessions page 2: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions on page 2, got %d", len(sessions))
	}
}

func TestUpdateSessionStatusRunning(t *testing.T) {
	db := testutil.SetupTestDB(t)
	defer testutil.CleanupTables(t, db, "sessions", "agents", "users")
	ctx := context.Background()

	user := testutil.CreateTestUser(t, db)
	agent := testutil.CreateTestAgent(t, db, user.ID)
	session := testutil.CreateTestSession(t, db, user.ID, agent.ID)

	err := db.UpdateSessionStatus(ctx, session.ID, goshared.SessionStatusRunning)
	if err != nil {
		t.Fatalf("update status to running: %v", err)
	}

	updated, err := db.GetSession(ctx, session.ID, user.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if updated.Status != goshared.SessionStatusRunning {
		t.Fatalf("expected status 'running', got '%s'", updated.Status)
	}
	if updated.StartedAt == nil {
		t.Fatal("expected started_at to be set")
	}
}

func TestUpdateSessionStatusDone(t *testing.T) {
	db := testutil.SetupTestDB(t)
	defer testutil.CleanupTables(t, db, "sessions", "agents", "users")
	ctx := context.Background()

	user := testutil.CreateTestUser(t, db)
	agent := testutil.CreateTestAgent(t, db, user.ID)
	session := testutil.CreateTestSession(t, db, user.ID, agent.ID)

	db.UpdateSessionStatus(ctx, session.ID, goshared.SessionStatusRunning)
	err := db.UpdateSessionStatus(ctx, session.ID, goshared.SessionStatusDone)
	if err != nil {
		t.Fatalf("update status to done: %v", err)
	}

	updated, err := db.GetSession(ctx, session.ID, user.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if updated.Status != goshared.SessionStatusDone {
		t.Fatalf("expected status 'done', got '%s'", updated.Status)
	}
	if updated.FinishedAt == nil {
		t.Fatal("expected finished_at to be set")
	}
}

func TestUpdateSessionStar(t *testing.T) {
	db := testutil.SetupTestDB(t)
	defer testutil.CleanupTables(t, db, "sessions", "agents", "users")
	ctx := context.Background()

	user := testutil.CreateTestUser(t, db)
	agent := testutil.CreateTestAgent(t, db, user.ID)
	session := testutil.CreateTestSession(t, db, user.ID, agent.ID)

	err := db.UpdateSessionStar(ctx, session.ID, user.ID, true)
	if err != nil {
		t.Fatalf("star session: %v", err)
	}

	updated, err := db.GetSession(ctx, session.ID, user.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if !updated.Starred {
		t.Fatal("expected starred to be true")
	}
}

func TestUpdateSessionShareToken(t *testing.T) {
	db := testutil.SetupTestDB(t)
	defer testutil.CleanupTables(t, db, "sessions", "agents", "users")
	ctx := context.Background()

	user := testutil.CreateTestUser(t, db)
	agent := testutil.CreateTestAgent(t, db, user.ID)
	session := testutil.CreateTestSession(t, db, user.ID, agent.ID)

	token := "test-share-token-" + goshared.GenerateID()[:8]
	expiresAt := time.Now().Add(7 * 24 * time.Hour)

	err := db.UpdateSessionShareToken(ctx, session.ID, user.ID, token, expiresAt)
	if err != nil {
		t.Fatalf("set share token: %v", err)
	}

	found, err := db.GetSessionByShareToken(ctx, token)
	if err != nil {
		t.Fatalf("get by share token: %v", err)
	}
	if found == nil {
		t.Fatal("expected session, got nil")
	}
	if found.ID != session.ID {
		t.Fatalf("expected session ID '%s', got '%s'", session.ID, found.ID)
	}
}

func TestCreateSessionChain(t *testing.T) {
	db := testutil.SetupTestDB(t)
	defer testutil.CleanupTables(t, db, "sessions", "agents", "users")
	ctx := context.Background()

	user := testutil.CreateTestUser(t, db)
	agent1 := testutil.CreateTestAgent(t, db, user.ID)
	agent2 := testutil.CreateTestAgent(t, db, user.ID)

	tasks := []goshared.SessionChainTask{
		{AgentID: agent1.ID, Task: "Step 1"},
		{AgentID: agent2.ID, Task: "Step 2"},
		{AgentID: agent1.ID, Task: "Step 3"},
	}

	sessionIDs, err := db.CreateSessionChain(ctx, user.ID, tasks)
	if err != nil {
		t.Fatalf("create session chain: %v", err)
	}
	if len(sessionIDs) != 3 {
		t.Fatalf("expected 3 session IDs, got %d", len(sessionIDs))
	}

	first, err := db.GetSession(ctx, sessionIDs[0], user.ID)
	if err != nil {
		t.Fatalf("get first session: %v", err)
	}
	if first.ParentSessionID != nil {
		t.Fatal("first session should have no parent")
	}
	if first.Position != 0 {
		t.Fatalf("expected position 0, got %d", first.Position)
	}

	second, err := db.GetSession(ctx, sessionIDs[1], user.ID)
	if err != nil {
		t.Fatalf("get second session: %v", err)
	}
	if second.ParentSessionID == nil || *second.ParentSessionID != sessionIDs[0] {
		t.Fatal("second session should reference first as parent")
	}
	if second.Position != 1 {
		t.Fatalf("expected position 1, got %d", second.Position)
	}
}
