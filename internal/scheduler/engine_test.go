package scheduler

import (
	"context"
	"errors"
	"github.com/jiuwusan/z-task-scheduler/internal/domain"
	"github.com/jiuwusan/z-task-scheduler/internal/store"
	"path/filepath"
	"testing"
	"time"
)

type blocking struct {
	started chan domain.Task
	release chan struct{}
}

func (b *blocking) Execute(ctx context.Context, t domain.Task) domain.RunResult {
	b.started <- t
	select {
	case <-b.release:
		return domain.RunResult{State: "success"}
	case <-ctx.Done():
		return domain.RunResult{State: "interrupted"}
	}
}
func fixture(t *testing.T) (*Engine, *store.Store, *blocking) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	b := &blocking{started: make(chan domain.Task, 10), release: make(chan struct{})}
	e := New(db, b, b)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		e.Close(ctx)
		db.Close()
	})
	return e, db, b
}
func task(name string) domain.Task {
	return domain.Task{Name: name, Type: "http", URL: "http://localhost", Enabled: true, Schedule: domain.Schedule{Kind: "interval", IntervalSeconds: 60}, TimeoutSeconds: 60}
}
func TestManualCapacityAndTaskLocks(t *testing.T) {
	e, _, b := fixture(t)
	ctx := context.Background()
	var first domain.Task
	for i := 0; i < 5; i++ {
		v, err := e.SaveTask(ctx, task("test"), time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = v
		}
		_, err = e.RunNow(ctx, v.ID)
		if i < 4 {
			if err != nil {
				t.Fatal(err)
			}
			<-b.started
		} else if !errors.Is(err, ErrBusy) {
			t.Fatal("capacity", err)
		}
	}
	if _, err := e.RunNow(ctx, first.ID); !errors.Is(err, ErrBusy) {
		t.Fatal("overlap allowed", err)
	}
	if err := e.DeleteTask(ctx, first.ID); !errors.Is(err, ErrBusy) {
		t.Fatal("deleted active task", err)
	}
}
func TestDockerTasksShareContainerLock(t *testing.T) {
	e, _, b := fixture(t)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		v := task("docker")
		v.Type = "docker"
		v.ContainerID = "same-id"
		if i == 1 {
			v.ContainerID = "recreated-id"
		}
		v.ContainerName = "same-name"
		v.DockerAction = "restart"
		v, err := e.SaveTask(ctx, v, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		_, err = e.RunNow(ctx, v.ID)
		if i == 0 {
			if err != nil {
				t.Fatal(err)
			}
			<-b.started
		} else if !errors.Is(err, ErrBusy) {
			t.Fatal("container overlap", err)
		}
	}
}

func TestDockerTasksWithDifferentNamesCanRunTogether(t *testing.T) {
	e, _, b := fixture(t)
	for _, name := range []string{"first", "second"} {
		v := task("docker")
		v.Type, v.ContainerName, v.DockerAction = "docker", name, "start"
		v, err := e.SaveTask(context.Background(), v, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.RunNow(context.Background(), v.ID); err != nil {
			t.Fatal("distinct containers blocked", err)
		}
		<-b.started
	}
}
func TestTickAdvanceSkipAndSnapshot(t *testing.T) {
	e, db, b := fixture(t)
	ctx := context.Background()
	now := time.Now()
	v, err := e.SaveTask(ctx, task("before"), now.Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Tick(ctx, now); err != nil {
		t.Fatal(err)
	}
	snap := <-b.started
	if snap.Name != "before" {
		t.Fatal(snap)
	}
	v.Name = "after"
	if _, err = e.SaveTask(ctx, v, now); err != nil {
		t.Fatal(err)
	}
	if err = e.Tick(ctx, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	rows, _, err := db.ListRuns(ctx, v.ID, 1, 20)
	if err != nil || len(rows) != 2 || rows[0].State != "skipped" {
		t.Fatalf("%+v %v", rows, err)
	}
	saved, _ := db.GetTask(ctx, v.ID)
	if saved.NextRunAt == nil || !saved.NextRunAt.After(now.Add(time.Minute)) {
		t.Fatal(saved)
	}
}
func TestOnceCompletesAndPausePreventsTick(t *testing.T) {
	e, db, b := fixture(t)
	ctx := context.Background()
	now := time.Now()
	at := now.Add(time.Minute)
	v := task("once")
	v.Schedule = domain.Schedule{Kind: "once", At: &at}
	v, err := e.SaveTask(ctx, v, now)
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Tick(ctx, at); err != nil {
		t.Fatal(err)
	}
	<-b.started
	got, _ := db.GetTask(ctx, v.ID)
	if got.Enabled || got.NextRunAt != nil {
		t.Fatal("one-shot not consumed", got)
	}
	paused := task("paused")
	paused.Enabled = false
	paused, err = e.SaveTask(ctx, paused, now)
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Tick(ctx, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	rows, _, _ := db.ListRuns(ctx, paused.ID, 1, 20)
	if len(rows) != 0 {
		t.Fatal("paused task ran")
	}
}
