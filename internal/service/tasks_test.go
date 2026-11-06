package service

import (
	"context"
	"errors"
	"github.com/jiuwusan/z-task-scheduler/internal/domain"
	"testing"
	"time"
)

type taskFixture struct {
	old       domain.Task
	saved     []domain.Task
	targetErr error
	targets   []string
}

func (f *taskFixture) GetTask(context.Context, string) (domain.Task, error) { return f.old, nil }
func (f *taskFixture) ListTasks(context.Context) ([]domain.Task, error) {
	return []domain.Task{f.old}, nil
}
func (f *taskFixture) ListRuns(context.Context, string, int, int) ([]domain.Run, int, error) {
	return []domain.Run{}, 0, nil
}
func (f *taskFixture) SaveTask(_ context.Context, t domain.Task, _ time.Time) (domain.Task, error) {
	f.saved = append(f.saved, t)
	return t, nil
}
func (f *taskFixture) ValidateTarget(_ context.Context, name string) error {
	f.targets = append(f.targets, name)
	return f.targetErr
}
func fixtureService(f *taskFixture, now time.Time) *Tasks {
	return &Tasks{Store: f, Engine: f, Docker: f, Clock: func() time.Time { return now }}
}
func TestDisabledPastOnceCanBeSavedButCannotBeEnabled(t *testing.T) {
	now := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	past := now.Add(-time.Hour)
	f := &taskFixture{}
	s := fixtureService(f, now)
	task := domain.Task{Name: "  request  ", Type: "http", URL: "https://example.invalid/", Method: "GET", Schedule: domain.Schedule{Kind: "once", At: &past}}
	got, err := s.Save(context.Background(), task)
	if err != nil || got.Name != "request" || got.Enabled || len(f.saved) != 1 {
		t.Fatalf("disabled task: %+v %v", got, err)
	}
	task.Enabled = true
	_, err = s.Save(context.Background(), task)
	var invalid *ValidationError
	if !errors.As(err, &invalid) || invalid.Code != "INVALID_SCHEDULE" || len(f.saved) != 1 {
		t.Fatalf("enabled invalid once persisted: %v", err)
	}
}
func TestDockerEditSkipsUnchangedTargetAndReenableValidates(t *testing.T) {
	now := time.Now()
	task := domain.Task{ID: "existing", Name: "container", Type: "docker", ContainerName: "permitted", DockerAction: "restart", Enabled: false, Schedule: domain.Schedule{Kind: "interval", IntervalSeconds: 60}}
	f := &taskFixture{old: task, targetErr: errors.New("not permitted")}
	s := fixtureService(f, now)
	got, err := s.Save(context.Background(), task)
	if err != nil || got.ContainerName != "permitted" || len(f.targets) != 0 {
		t.Fatalf("unchanged disabled target: %+v %v", got, err)
	}
	task.Enabled = true
	_, err = s.Save(context.Background(), task)
	var invalid *ValidationError
	if !errors.As(err, &invalid) || invalid.Code != "INVALID_CONTAINER" || len(f.saved) != 1 {
		t.Fatalf("re-enabled invalid target persisted: %v", err)
	}
}
