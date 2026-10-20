package httpapi

import (
	"context"
	"net/http"
	"time"
)

func (s *server) containers(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	rows, err := s.docker.List(ctx)
	if err != nil {
		fail(w, 503, "DOCKER_UNAVAILABLE", err.Error())
		return
	}
	respond(w, 200, rows)
}
func (s *server) status(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	err := s.docker.Status(ctx)
	message := "已连接"
	if err != nil {
		message = err.Error()
	}
	respond(w, 200, map[string]any{"dockerAvailable": err == nil, "dockerMessage": message, "timezone": "Asia/Shanghai", "maxConcurrent": 4, "retentionDays": 30})
}
