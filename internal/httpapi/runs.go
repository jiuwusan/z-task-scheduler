package httpapi

import (
	"net/http"
	"strconv"
)

func (s *server) runs(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	page = max(1, min(page, 1000000))
	if limit < 1 {
		limit = 20
	}
	limit = min(limit, 100)
	rows, total, err := s.db.ListRuns(r.Context(), r.URL.Query().Get("taskId"), page, limit)
	if err != nil {
		operationError(w, err)
		return
	}
	respond(w, 200, map[string]any{"items": rows, "total": total, "page": page, "limit": limit})
}
func (s *server) run(w http.ResponseWriter, r *http.Request) {
	v, err := s.db.GetRun(r.Context(), r.PathValue("id"))
	if err != nil {
		operationError(w, err)
		return
	}
	respond(w, 200, v)
}
