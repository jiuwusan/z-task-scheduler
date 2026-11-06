package service

import (
	"context"
	"time"

	"github.com/jiuwusan/z-task-scheduler/internal/domain"
	"github.com/jiuwusan/z-task-scheduler/internal/schedule"
)

type TaskStore interface {
	GetTask(context.Context, string) (domain.Task, error)
	ListTasks(context.Context) ([]domain.Task, error)
	ListRuns(context.Context, string, int, int) ([]domain.Run, int, error)
}
type TaskEngine interface {
	SaveTask(context.Context, domain.Task, time.Time) (domain.Task, error)
}
type DockerTarget interface {
	ValidateTarget(context.Context, string) error
}
type Tasks struct {
	Store  TaskStore
	Engine TaskEngine
	Docker DockerTarget
	Clock  func() time.Time
}
type ValidationError struct {
	Code string
	Err  error
}

func (e *ValidationError) Error() string { return e.Err.Error() }
func (e *ValidationError) Unwrap() error { return e.Err }
func (s *Tasks) Save(ctx context.Context, t domain.Task) (domain.Task, error) {
	if err := domain.ValidateTask(&t); err != nil {
		return domain.Task{}, &ValidationError{Code: "INVALID_TASK", Err: err}
	}
	now := s.Clock()
	checkNow := now
	if !t.Enabled && t.Schedule.Kind == "once" && t.Schedule.At != nil {
		checkNow = t.Schedule.At.Add(-time.Nanosecond)
	}
	if err := schedule.Validate(t.Schedule, checkNow); err != nil {
		return domain.Task{}, &ValidationError{Code: "INVALID_SCHEDULE", Err: err}
	}
	if t.Type == "http" {
		t.CurlCommand = ""
		t.ContainerID = ""
		t.ContainerName = ""
		t.DockerAction = ""
	} else {
		validate := true
		if t.ID != "" {
			old, err := s.Store.GetTask(ctx, t.ID)
			if err != nil {
				return domain.Task{}, err
			}
			oldName, _ := domain.ValidateContainerName(old.ContainerName)
			validate = old.Type != "docker" || oldName != t.ContainerName || (!old.Enabled && t.Enabled)
		}
		if validate {
			ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			if err := s.Docker.ValidateTarget(ctx, t.ContainerName); err != nil {
				return domain.Task{}, &ValidationError{Code: "INVALID_CONTAINER", Err: err}
			}
		}
		t.CurlCommand = ""
		t.ContainerID = ""
		t.URL = ""
		t.Method = ""
		t.Headers = nil
		t.Data = nil
	}
	saved, err := s.Engine.SaveTask(ctx, t, now)
	if err != nil {
		return domain.Task{}, err
	}
	return saved, nil
}

type Summary struct {
	State     string    `json:"state"`
	StartedAt time.Time `json:"startedAt"`
}
type Overview struct {
	domain.Task
	LastRun *Summary `json:"lastRun,omitempty"`
}

func (s *Tasks) List(ctx context.Context) ([]Overview, error) {
	rows, err := s.Store.ListTasks(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]Overview, 0, len(rows))
	for _, task := range rows {
		task = task.Redacted()
		item := Overview{Task: task}
		recent, _, err := s.Store.ListRuns(ctx, task.ID, 1, 1)
		if err != nil {
			return nil, err
		}
		if len(recent) > 0 {
			item.LastRun = &Summary{State: recent[0].State, StartedAt: recent[0].StartedAt}
		}
		items = append(items, item)
	}
	return items, nil
}
