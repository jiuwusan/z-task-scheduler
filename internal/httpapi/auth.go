package httpapi

import (
	"errors"
	"github.com/jiuwusan/z-task-scheduler/internal/service"
	"net"
	"net/http"

	"sync"
	"time"
)

type attempt struct {
	count int
	until time.Time
}
type limiter struct {
	mu    sync.Mutex
	items map[string]attempt
}

func (l *limiter) reserve(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	for k, a := range l.items {
		if !now.Before(a.until) {
			delete(l.items, k)
		}
	}
	a := l.items[key]
	if !now.Before(a.until) {
		a = attempt{until: now.Add(15 * time.Minute)}
	}
	if a.count >= 5 {
		return false
	}
	a.count++
	l.items[key] = a
	return true
}
func (l *limiter) release(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	a := l.items[key]
	if a.count > 0 {
		a.count--
		l.items[key] = a
	}
}
func cookie(w http.ResponseWriter, r *http.Request, name, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: maxAge})
}
func (s *server) setupStatus(w http.ResponseWriter, r *http.Request) {
	initialized, err := s.accountService.Initialized(r.Context())
	if err != nil {
		operationError(w, err)
		return
	}
	if initialized {
		respond(w, 200, map[string]any{"initialized": true})
		return
	}
	if c, err := r.Cookie("scheduler_setup"); err == nil {
		if v, ok := s.setup.Get(c.Value, time.Now()); ok {
			respond(w, 200, map[string]any{"initialized": false, "csrf": v.CSRF})
			return
		}
	}
	v, err := s.setup.Create("setup", time.Now().Add(-24*time.Hour+10*time.Minute))
	if err != nil {
		operationError(w, err)
		return
	}
	cookie(w, r, "scheduler_setup", v.Token, 600)
	respond(w, 200, map[string]any{"initialized": false, "csrf": v.CSRF})
}
func (s *server) initialize(w http.ResponseWriter, r *http.Request) {
	initialized, err := s.accountService.Initialized(r.Context())
	if err != nil {
		operationError(w, err)
		return
	}
	if initialized {
		fail(w, 409, "INITIALIZED", "管理员已创建")
		return
	}
	c, err := r.Cookie("scheduler_setup")
	if err != nil {
		fail(w, 403, "CSRF", "请刷新初始化页面")
		return
	}
	v, ok := s.setup.Get(c.Value, time.Now())
	if !ok || !sameOrigin(r) || r.Header.Get("X-CSRF-Token") != v.CSRF {
		fail(w, 403, "CSRF", "初始化校验失败")
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decode(w, r, &body) {
		return
	}
	if err = s.accountService.Initialize(r.Context(), body.Username, body.Password); err != nil {
		accountError(w, err)
		return
	}
	s.setup.Delete(c.Value)
	cookie(w, r, "scheduler_setup", "", -1)
	respond(w, 201, map[string]bool{"initialized": true})
}
func (s *server) login(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		fail(w, 403, "ORIGIN", "请求来源无效")
		return
	}
	key, _, _ := net.SplitHostPort(r.RemoteAddr)
	if !s.limits.reserve(key) {
		fail(w, 429, "RATE_LIMIT", "登录失败次数过多，请在 15 分钟后重试")
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decode(w, r, &body) {
		return
	}
	username, err := s.accountService.Authenticate(r.Context(), body.Username, body.Password)
	if err != nil {
		accountError(w, err)
		return
	}
	s.limits.release(key)
	v, err := s.sessions.Create(username, time.Now())
	if err != nil {
		operationError(w, err)
		return
	}
	cookie(w, r, "scheduler_session", v.Token, 86400)
	respond(w, 200, v)
}
func (s *server) session(w http.ResponseWriter, r *http.Request) {
	c, _ := r.Cookie("scheduler_session")
	v, _ := s.sessions.Get(c.Value, time.Now())
	respond(w, 200, v)
}
func (s *server) logout(w http.ResponseWriter, r *http.Request) {
	c, _ := r.Cookie("scheduler_session")
	s.sessions.Delete(c.Value)
	cookie(w, r, "scheduler_session", "", -1)
	respond(w, 200, map[string]bool{"loggedOut": true})
}

func accountError(w http.ResponseWriter, err error) {
	var app *service.AccountError
	if errors.As(err, &app) {
		fail(w, app.Status, app.Code, app.Message)
		return
	}
	operationError(w, err)
}
