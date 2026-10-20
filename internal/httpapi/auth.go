package httpapi

import (
	"errors"
	"github.com/jiuwusan/z-task-scheduler/internal/auth"
	"github.com/jiuwusan/z-task-scheduler/internal/store"
	"net"
	"net/http"
	"strings"
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

func (l *limiter) blocked(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	a := l.items[key]
	return time.Now().Before(a.until) && a.count >= 5
}
func (l *limiter) failed(key string) {
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
	a.count++
	l.items[key] = a
}
func cookie(w http.ResponseWriter, r *http.Request, name, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: maxAge})
}
func (s *server) setupStatus(w http.ResponseWriter, r *http.Request) {
	_, _, err := s.db.Admin(r.Context())
	if err == nil {
		respond(w, 200, map[string]any{"initialized": true})
		return
	}
	if !errors.Is(err, store.ErrNotFound) {
		operationError(w, err)
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
	if _, _, err := s.db.Admin(r.Context()); err == nil {
		fail(w, 409, "INITIALIZED", "管理员已创建")
		return
	} else if !errors.Is(err, store.ErrNotFound) {
		operationError(w, err)
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
	body.Username = strings.TrimSpace(body.Username)
	if body.Username == "" || len(body.Username) > 80 {
		fail(w, 400, "INVALID_USERNAME", "请输入 1 至 80 字节的用户名")
		return
	}
	hash, err := auth.Hash(body.Password)
	if err != nil {
		fail(w, 400, "INVALID_PASSWORD", err.Error())
		return
	}
	if err = s.db.InitializeAdmin(r.Context(), body.Username, hash); err != nil {
		if errors.Is(err, store.ErrInitialized) {
			fail(w, 409, "INITIALIZED", "管理员已创建")
		} else {
			operationError(w, err)
		}
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
	if s.limits.blocked(key) {
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
	username, hash, err := s.db.Admin(r.Context())
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		operationError(w, err)
		return
	}
	valid := auth.Verify(hash, body.Password)
	if err != nil || body.Username != username || !valid {
		s.limits.failed(key)
		fail(w, 401, "INVALID_CREDENTIALS", "用户名或密码错误")
		return
	}
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
