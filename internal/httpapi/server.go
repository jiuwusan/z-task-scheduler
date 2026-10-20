package httpapi

import (
	"encoding/json"
	"errors"
	"github.com/jiuwusan/z-task-scheduler/internal/auth"
	"github.com/jiuwusan/z-task-scheduler/internal/executor"
	"github.com/jiuwusan/z-task-scheduler/internal/scheduler"
	"github.com/jiuwusan/z-task-scheduler/internal/store"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type server struct {
	db              *store.Store
	engine          *scheduler.Engine
	docker          *executor.Docker
	sessions, setup *auth.Sessions
	limits          limiter
}

func New(db *store.Store, engine *scheduler.Engine, docker *executor.Docker) http.Handler {
	s := &server{db: db, engine: engine, docker: docker, sessions: auth.NewSessions(), setup: auth.NewSessions(), limits: limiter{items: map[string]attempt{}}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) { respond(w, 200, map[string]any{"status": "ok"}) })
	mux.HandleFunc("GET /api/setup/status", s.setupStatus)
	mux.HandleFunc("POST /api/setup", s.initialize)
	mux.HandleFunc("POST /api/auth/login", s.login)
	mux.HandleFunc("GET /api/auth/session", s.protect(s.session))
	mux.HandleFunc("POST /api/auth/logout", s.protect(s.logout))
	mux.HandleFunc("GET /api/tasks", s.protect(s.tasks))
	mux.HandleFunc("POST /api/tasks", s.protect(s.saveTask))
	mux.HandleFunc("GET /api/tasks/{id}", s.protect(s.task))
	mux.HandleFunc("PUT /api/tasks/{id}", s.protect(s.saveTask))
	mux.HandleFunc("DELETE /api/tasks/{id}", s.protect(s.deleteTask))
	mux.HandleFunc("POST /api/tasks/{id}/run", s.protect(s.runTask))
	mux.HandleFunc("GET /api/runs", s.protect(s.runs))
	mux.HandleFunc("GET /api/runs/{id}", s.protect(s.run))
	mux.HandleFunc("GET /api/docker/containers", s.protect(s.containers))
	mux.HandleFunc("GET /api/status", s.protect(s.status))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { fail(w, 404, "NOT_FOUND", "接口不存在") })
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		mux.ServeHTTP(w, r)
	})
}
func respond(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{"data": data})
}
func fail(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code, "message": message}})
}
func decode(w http.ResponseWriter, r *http.Request, out any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 128<<10)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		fail(w, 400, "BAD_REQUEST", "请求格式无效或内容过长")
		return false
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		fail(w, 400, "BAD_REQUEST", "只接受一个 JSON 对象")
		return false
	}
	return true
}
func sameOrigin(r *http.Request) bool {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.User == nil && strings.EqualFold(u.Host, r.Host)
}
func (s *server) protect(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie("scheduler_session")
		if err != nil {
			fail(w, 401, "UNAUTHORIZED", "请先登录")
			return
		}
		session, ok := s.sessions.Get(c.Value, time.Now())
		if !ok {
			fail(w, 401, "UNAUTHORIZED", "登录已过期，请重新登录")
			return
		}
		if r.Method != "GET" && (!sameOrigin(r) || r.Header.Get("X-CSRF-Token") != session.CSRF) {
			fail(w, 403, "CSRF", "请求校验失败，请刷新页面")
			return
		}
		next(w, r)
	}
}
func operationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		fail(w, 404, "NOT_FOUND", "任务或记录不存在")
	case errors.Is(err, scheduler.ErrBusy):
		fail(w, 409, "BUSY", err.Error())
	case errors.Is(err, scheduler.ErrClosed):
		fail(w, 503, "CLOSING", err.Error())
	default:
		slog.Error("管理操作失败", "error", err)
		fail(w, 500, "INTERNAL", "操作失败，请查看服务日志")
	}
}
