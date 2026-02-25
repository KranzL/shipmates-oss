package goshared_test

import (
	"context"
	"testing"

	goshared "github.com/KranzL/shipmates-oss/go-shared"
	"github.com/KranzL/shipmates-oss/go-shared/testutil"
)

func TestCreateClarification(t *testing.T) {
	db := testutil.SetupTestDB(t)
	defer testutil.CleanupTables(t, db, "clarifications", "sessions", "agents", "users")
	ctx := context.Background()

	user := testutil.CreateTestUser(t, db)
	agent := testutil.CreateTestAgent(t, db, user.ID)
	session := testutil.CreateTestSession(t, db, user.ID, agent.ID)

	clar, err := db.CreateClarification(ctx, session.ID, "What database should I use?")
	if err != nil {
		t.Fatalf("create clarification: %v", err)
	}
	if clar.ID == "" {
		t.Fatal("expected non-empty clarification ID")
	}
	if clar.Question != "What database should I use?" {
		t.Fatalf("expected question text, got '%s'", clar.Question)
	}
	if clar.Status != goshared.ClarificationStatusPending {
		t.Fatalf("expected status 'pending', got '%s'", clar.Status)
	}
}

func TestAnswerClarification(t *testing.T) {
	db := testutil.SetupTestDB(t)
	defer testutil.CleanupTables(t, db, "clarifications", "sessions", "agents", "users")
	ctx := context.Background()

	user := testutil.CreateTestUser(t, db)
	agent := testutil.CreateTestAgent(t, db, user.ID)
	session := testutil.CreateTestSession(t, db, user.ID, agent.ID)

	db.UpdateSessionStatus(ctx, session.ID, goshared.SessionStatusWaitingForClarification)

	clar, err := db.CreateClarification(ctx, session.ID, "Which framework?")
	if err != nil {
		t.Fatalf("create clarification: %v", err)
	}

	err = db.AnswerClarification(ctx, clar.ID, user.ID, "Use React")
	if err != nil {
		t.Fatalf("answer clarification: %v", err)
	}

	answered, err := db.GetClarification(ctx, clar.ID)
	if err != nil {
		t.Fatalf("get clarification: %v", err)
	}
	if answered.Status != goshared.ClarificationStatusAnswered {
		t.Fatalf("expected status 'answered', got '%s'", answered.Status)
	}
	if answered.Answer == nil || *answered.Answer != "Use React" {
		t.Fatal("expected answer 'Use React'")
	}
	if answered.AnsweredAt == nil {
		t.Fatal("expected answered_at to be set")
	}
}

func TestListClarificationsBySession(t *testing.T) {
	db := testutil.SetupTestDB(t)
	defer testutil.CleanupTables(t, db, "clarifications", "sessions", "agents", "users")
	ctx := context.Background()

	user := testutil.CreateTestUser(t, db)
	agent := testutil.CreateTestAgent(t, db, user.ID)
	session := testutil.CreateTestSession(t, db, user.ID, agent.ID)

	db.CreateClarification(ctx, session.ID, "Question 1")
	db.CreateClarification(ctx, session.ID, "Question 2")

	clars, err := db.ListClarifications(ctx, session.ID)
	if err != nil {
		t.Fatalf("list clarifications: %v", err)
	}
	if len(clars) != 2 {
		t.Fatalf("expected 2 clarifications, got %d", len(clars))
	}
}

func TestMaxClarificationsPerSession(t *testing.T) {
	db := testutil.SetupTestDB(t)
	defer testutil.CleanupTables(t, db, "clarifications", "sessions", "agents", "users")
	ctx := context.Background()

	user := testutil.CreateTestUser(t, db)
	agent := testutil.CreateTestAgent(t, db, user.ID)
	session := testutil.CreateTestSession(t, db, user.ID, agent.ID)

	for i := 0; i < goshared.MaxClarificationsPerSession; i++ {
		_, err := db.CreateClarification(ctx, session.ID, "Question")
		if err != nil {
			t.Fatalf("create clarification %d: %v", i, err)
		}
	}

	_, err := db.CreateClarification(ctx, session.ID, "One too many")
	if err == nil {
		t.Fatal("expected error for exceeding max clarifications, got nil")
	}
}
