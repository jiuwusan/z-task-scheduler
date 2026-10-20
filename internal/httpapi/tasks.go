package httpapi

import (
	"context"
	"github.com/jiuwusan/z-task-scheduler/internal/domain"
	"github.com/jiuwusan/z-task-scheduler/internal/executor"
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
	for i := range rows {
		rows[i].CurlCommand = ""
	}
	respond(w, 200, rows)
}
func (s *server) task(w http.ResponseWriter, r *http.Request) {
	t, err := s.db.GetTask(r.Context(), r.PathValue("id"))
	if err != nil {
		operationError(w, err)
		return
	}
	respond(w, 200, t)
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
	if t.Type == "curl" {
		if _, err := executor.ParseCurl(t.CurlCommand); err != nil {
			fail(w, 400, "INVALID_CURL", err.Error())
			return
		}
		t.ContainerID = ""
		t.ContainerName = ""
		t.DockerAction = ""
	} else {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		if err := s.docker.ValidateTarget(ctx, t.ContainerID); err != nil {
			fail(w, 400, "INVALID_CONTAINER", err.Error())
			return
		}
		t.CurlCommand = ""
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
