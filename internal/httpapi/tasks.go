package httpapi

import (
	"errors"
	"net/http"

	"github.com/jiuwusan/z-task-scheduler/internal/domain"
	"github.com/jiuwusan/z-task-scheduler/internal/service"
)

func (s *server) tasks(w http.ResponseWriter, r *http.Request) {
	items, err := s.taskService.List(r.Context())
	if err != nil {
		operationError(w, err)
		return
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
	saved, err := s.taskService.Save(r.Context(), t)
	if err != nil {
		var validation *service.ValidationError
		if errors.As(err, &validation) {
			fail(w, 400, validation.Code, validation.Error())
		} else {
			operationError(w, err)
		}
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
