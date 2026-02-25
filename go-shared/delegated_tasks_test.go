package goshared_test

import (
	"context"
	"encoding/json"
	"testing"

	goshared "github.com/KranzL/shipmates-oss/go-shared"
	"github.com/KranzL/shipmates-oss/go-shared/testutil"
)

func TestCreateDelegatedTask(t *testing.T) {
	db := testutil.SetupTestDB(t)
	defer testutil.CleanupTables(t, db, "delegated_tasks", "sessions", "agents", "users")
	ctx := context.Background()

	user := testutil.CreateTestUser(t, db)
	fromAgent := testutil.CreateTestAgent(t, db, user.ID)
	toAgent := testutil.CreateTestAgent(t, db, user.ID)

	task := &goshared.DelegatedTask{
		UserID:      user.ID,
		FromAgentID: fromAgent.ID,
		ToAgentID:   toAgent.ID,
		Description: "Write unit tests",
		Priority:    75,
		Status:      "pending",
		BlockedBy:   []string{},
		Context:     json.RawMessage(`{"summary":"test context"}`),
	}

	created, err := db.CreateDelegatedTask(ctx, task)
	if err != nil {
		t.Fatalf("create delegated task: %v", err)
	}
	if created.ID == "" {
		t.Fatal("expected non-empty task ID")
	}
	if created.Priority != 75 {
		t.Fatalf("expected priority 75, got %d", created.Priority)
	}
}

func TestListDelegatedTasksWithStatusFilter(t *testing.T) {
	db := testutil.SetupTestDB(t)
	defer testutil.CleanupTables(t, db, "delegated_tasks", "sessions", "agents", "users")
	ctx := context.Background()

	user := testutil.CreateTestUser(t, db)
	fromAgent := testutil.CreateTestAgent(t, db, user.ID)
	toAgent := testutil.CreateTestAgent(t, db, user.ID)

	for _, status := range []string{"pending", "pending", "done"} {
		task := &goshared.DelegatedTask{
			UserID:      user.ID,
			FromAgentID: fromAgent.ID,
			ToAgentID:   toAgent.ID,
			Description: "Task " + status,
			Priority:    50,
			Status:      status,
			BlockedBy:   []string{},
			Context:     json.RawMessage(`{}`),
		}
		db.CreateDelegatedTask(ctx, task)
	}

	pendingStatus := goshared.DelegatedTaskStatusPending
	tasks, total, err := db.ListDelegatedTasks(ctx, user.ID, &pendingStatus, 10, 0)
	if err != nil {
		t.Fatalf("list delegated tasks: %v", err)
	}
	if total != 2 {
		t.Fatalf("expected 2 pending tasks, got %d", total)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks returned, got %d", len(tasks))
	}
}

func TestUpdateDelegatedTaskStatus(t *testing.T) {
	db := testutil.SetupTestDB(t)
	defer testutil.CleanupTables(t, db, "delegated_tasks", "sessions", "agents", "users")
	ctx := context.Background()

	user := testutil.CreateTestUser(t, db)
	fromAgent := testutil.CreateTestAgent(t, db, user.ID)
	toAgent := testutil.CreateTestAgent(t, db, user.ID)

	task := &goshared.DelegatedTask{
		UserID:      user.ID,
		FromAgentID: fromAgent.ID,
		ToAgentID:   toAgent.ID,
		Description: "Task to claim",
		Priority:    50,
		Status:      "pending",
		BlockedBy:   []string{},
		Context:     json.RawMessage(`{}`),
	}
	created, _ := db.CreateDelegatedTask(ctx, task)

	err := db.UpdateDelegatedTaskStatus(ctx, created.ID, user.ID, goshared.DelegatedTaskStatusClaimed, nil)
	if err != nil {
		t.Fatalf("update task status: %v", err)
	}

	found, err := db.GetDelegatedTask(ctx, created.ID, user.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if found.Status != "claimed" {
		t.Fatalf("expected status 'claimed', got '%s'", found.Status)
	}
	if found.ClaimedAt == nil {
		t.Fatal("expected claimed_at to be set")
	}
}

func TestUpdateDelegatedTaskStatusDone(t *testing.T) {
	db := testutil.SetupTestDB(t)
	defer testutil.CleanupTables(t, db, "delegated_tasks", "sessions", "agents", "users")
	ctx := context.Background()

	user := testutil.CreateTestUser(t, db)
	fromAgent := testutil.CreateTestAgent(t, db, user.ID)
	toAgent := testutil.CreateTestAgent(t, db, user.ID)

	task := &goshared.DelegatedTask{
		UserID:      user.ID,
		FromAgentID: fromAgent.ID,
		ToAgentID:   toAgent.ID,
		Description: "Task to complete",
		Priority:    50,
		Status:      "claimed",
		BlockedBy:   []string{},
		Context:     json.RawMessage(`{}`),
	}
	created, _ := db.CreateDelegatedTask(ctx, task)

	err := db.UpdateDelegatedTaskStatus(ctx, created.ID, user.ID, goshared.DelegatedTaskStatusDone, nil)
	if err != nil {
		t.Fatalf("update task status to done: %v", err)
	}

	found, err := db.GetDelegatedTask(ctx, created.ID, user.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if found.Status != "done" {
		t.Fatalf("expected status 'done', got '%s'", found.Status)
	}
	if found.CompletedAt == nil {
		t.Fatal("expected completed_at to be set")
	}
}

func TestGetDelegationStats(t *testing.T) {
	db := testutil.SetupTestDB(t)
	defer testutil.CleanupTables(t, db, "delegated_tasks", "sessions", "agents", "users")
	ctx := context.Background()

	user := testutil.CreateTestUser(t, db)
	fromAgent := testutil.CreateTestAgent(t, db, user.ID)
	toAgent := testutil.CreateTestAgent(t, db, user.ID)

	for _, status := range []string{"pending", "claimed", "done", "done"} {
		task := &goshared.DelegatedTask{
			UserID:      user.ID,
			FromAgentID: fromAgent.ID,
			ToAgentID:   toAgent.ID,
			Description: "Stats task",
			Priority:    50,
			Status:      status,
			BlockedBy:   []string{},
			Context:     json.RawMessage(`{}`),
		}
		db.CreateDelegatedTask(ctx, task)
	}

	stats, err := db.GetDelegationStats(ctx, user.ID)
	if err != nil {
		t.Fatalf("get delegation stats: %v", err)
	}
	if stats.TotalTasks != 4 {
		t.Fatalf("expected 4 total tasks, got %d", stats.TotalTasks)
	}
	if stats.ByStatus[goshared.DelegatedTaskStatusDone] != 2 {
		t.Fatalf("expected 2 done tasks, got %d", stats.ByStatus[goshared.DelegatedTaskStatusDone])
	}
}
