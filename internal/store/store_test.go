package store_test

import (
	"context"
	"errors"
	"github.com/jiuwusan/z-task-scheduler/internal/domain"
	"github.com/jiuwusan/z-task-scheduler/internal/store"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPersistenceRecoveryAndPrune(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Hour)
	task, err := db.CreateTask(ctx, domain.Task{Name: "定时请求", Type: "curl", CurlCommand: "curl http://localhost", Enabled: true, Schedule: domain.Schedule{Kind: "once", At: &past}, NextRunAt: &past})
	if err != nil {
		t.Fatal(err)
	}
	run, err := db.CreateRun(ctx, domain.Run{TaskID: task.ID, TaskName: task.Name, State: "running", StartedAt: past})
	if err != nil {
		t.Fatal(err)
	}
	old, err := db.CreateRun(ctx, domain.Run{TaskID: task.ID, State: "success", StartedAt: time.Now().Add(-31 * 24 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.InitializeAdmin(ctx, "admin", "hash"); err != nil {
		t.Fatal(err)
	}
	if err = db.InitializeAdmin(ctx, "other", "second"); err == nil {
		t.Fatal("second initialization succeeded")
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Recover(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetTask(ctx, task.ID)
	if err != nil || got.Enabled || got.NextRunAt != nil || got.CurlCommand != "curl http://localhost" {
		t.Fatalf("task=%+v %v", got, err)
	}
	result, err := db.GetRun(ctx, run.ID)
	if err != nil || result.State != "interrupted" || result.FinishedAt == nil {
		t.Fatalf("run=%+v %v", result, err)
	}
	username, hash, err := db.Admin(ctx)
	if err != nil || username != "admin" || hash != "hash" {
		t.Fatal(username, hash, err)
	}
	if err = db.DeleteTask(ctx, task.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.GetRun(ctx, run.ID); err != nil {
		t.Fatal("deleted history", err)
	}
	if err = db.Prune(ctx, time.Now().Add(-30*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.GetRun(ctx, old.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("old run not pruned", err)
	}
	rows, total, err := db.ListRuns(ctx, "", 1, 20)
	if err != nil || total != 1 || len(rows) != 1 {
		t.Fatal(rows, total, err)
	}
}
func TestUnwritablePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "regular-file")
	if err := os.WriteFile(path, []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	if db, err := store.Open(filepath.Join(path, "child.db")); err == nil {
		db.Close()
		t.Fatal("directory accepted as database")
	}
}
