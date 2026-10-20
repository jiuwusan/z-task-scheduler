package httpapi

import (
	"context"
	"github.com/jiuwusan/z-task-scheduler/internal/domain"
	"github.com/jiuwusan/z-task-scheduler/internal/schedule"
	"net/http"
	"time"
)

func (s *server) tasks(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.ListTasks(r.Context())
	if err != nil {
		operationError(w, err)
		return
	}
	type summary struct {
		State     string    `json:"state"`
		StartedAt time.Time `json:"startedAt"`
	}
	type overview struct {
		domain.Task
		LastRun *summary `json:"lastRun,omitempty"`
	}
	items := make([]overview, 0, len(rows))
	for _, task := range rows {
		task = task.Redacted()
		item := overview{Task: task}
		recent, _, err := s.db.ListRuns(r.Context(), task.ID, 1, 1)
		if err != nil {
			operationError(w, err)
			return
		}
		if len(recent) > 0 {
			item.LastRun = &summary{State: recent[0].State, StartedAt: recent[0].StartedAt}
		}
		items = append(items, item)
	}
	respond(w, 200, items)
}
func (s *server) task(w http.ResponseWriter, r *http.Request) {
	t, err := s.db.GetTask(r.Context(), r.PathValue("id"))
	if err != nil {
		operationError(w, err)
		return
	}
	// Preserve the original JSON number lexemes for browser editing. Parsing
	// Task.Data into JavaScript numbers would silently round large integers.
	respond(w, 200, struct {
		domain.Task
		DataText string `json:"dataText,omitempty"`
	}{Task: t, DataText: string(t.Data)})
}
func (s *server) saveTask(w http.ResponseWriter, r *http.Request) {
	var t domain.Task
	if !decode(w, r, &t) {
		return
	}
	t.ID = r.PathValue("id")
	if err := domain.ValidateTask(&t); err != nil {
		fail(w, 400, "INVALID_TASK", err.Error())
		return
	}
	now := time.Now()
	checkNow := now
	if !t.Enabled && t.Schedule.Kind == "once" && t.Schedule.At != nil {
		checkNow = t.Schedule.At.Add(-time.Nanosecond)
	}
	if err := schedule.Validate(t.Schedule, checkNow); err != nil {
		fail(w, 400, "INVALID_SCHEDULE", err.Error())
		return
	}
	if t.Type == "http" {
		t.CurlCommand = ""
		t.ContainerID = ""
		t.ContainerName = ""
		t.DockerAction = ""
	} else {
		validate := true
		if t.ID != "" {
			old, err := s.db.GetTask(r.Context(), t.ID)
			if err != nil {
				operationError(w, err)
				return
			}
			oldName, _ := domain.ValidateContainerName(old.ContainerName)
			validate = old.Type != "docker" || oldName != t.ContainerName || (!old.Enabled && t.Enabled)
		}
		if validate {
			ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
			defer cancel()
			if err := s.docker.ValidateTarget(ctx, t.ContainerName); err != nil {
				fail(w, 400, "INVALID_CONTAINER", err.Error())
				return
			}
		}
		t.CurlCommand = ""
		t.ContainerID = ""
		t.URL = ""
		t.Method = ""
		t.Headers = nil
		t.Data = nil
	}
	saved, err := s.engine.SaveTask(r.Context(), t, now)
	if err != nil {
		operationError(w, err)
		return
	}
	status := 200
	if r.Method == "POST" {
		status = 201
	}
	respond(w, status, saved)
}
func (s *server) deleteTask(w http.ResponseWriter, r *http.Request) {
	if err := s.engine.DeleteTask(r.Context(), r.PathValue("id")); err != nil {
		operationError(w, err)
		return
	}
	respond(w, 200, map[string]bool{"deleted": true})
}
func (s *server) runTask(w http.ResponseWriter, r *http.Request) {
	run, err := s.engine.RunNow(r.Context(), r.PathValue("id"))
	if err != nil {
		operationError(w, err)
		return
	}
	respond(w, 202, run)
}
