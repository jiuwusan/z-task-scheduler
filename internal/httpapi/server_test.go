package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/jiuwusan/z-task-scheduler/internal/domain"
	"github.com/jiuwusan/z-task-scheduler/internal/executor"
	"github.com/jiuwusan/z-task-scheduler/internal/scheduler"
	"github.com/jiuwusan/z-task-scheduler/internal/store"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type instant struct{}

func (instant) Execute(context.Context, domain.Task) domain.RunResult {
	return domain.RunResult{State: "success", Output: "done"}
}

type client struct {
	t    *testing.T
	url  string
	http *http.Client
	csrf string
}

func (c *client) call(method, path string, body any, csrf bool) (int, map[string]any) {
	c.t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(method, c.url+path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", c.url)
	if csrf {
		req.Header.Set("X-CSRF-Token", c.csrf)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err = json.NewDecoder(resp.Body).Decode(&out); err != nil {
		c.t.Fatal(err)
	}
	return resp.StatusCode, out
}
func setupClient(t *testing.T) *client {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	engine := scheduler.New(db, instant{}, instant{})
	s := httptest.NewServer(New(db, engine, executor.NewDocker("/missing", "z-task-scheduler", "io.jiuwusan.z-task-scheduler")))
	jar, _ := cookiejar.New(nil)
	c := &client{t: t, url: s.URL, http: &http.Client{Jar: jar}}
	t.Cleanup(func() {
		s.Close()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		engine.Close(ctx)
		db.Close()
	})
	return c
}
func TestSetupLoginCSRFAndTaskPersistence(t *testing.T) {
	c := setupClient(t)
	if code, _ := c.call("GET", "/health", nil, false); code != 200 {
		t.Fatal(code)
	}
	if code, _ := c.call("GET", "/api/tasks", nil, false); code != 401 {
		t.Fatal(code)
	}
	_, v := c.call("GET", "/api/setup/status", nil, false)
	c.csrf = v["data"].(map[string]any)["csrf"].(string)
	creds := map[string]string{"username": "admin", "password": "test-password"}
	if code, _ := c.call("POST", "/api/setup", creds, false); code != 403 {
		t.Fatal("setup CSRF", code)
	}
	if code, _ := c.call("POST", "/api/setup", creds, true); code != 201 {
		t.Fatal("setup", code)
	}
	if code, _ := c.call("POST", "/api/setup", creds, true); code != 409 {
		t.Fatal("repeat setup", code)
	}
	code, v := c.call("POST", "/api/auth/login", creds, false)
	if code != 200 {
		t.Fatal("login", code)
	}
	c.csrf = v["data"].(map[string]any)["csrf"].(string)
	body := map[string]any{"name": "请求任务", "type": "curl", "curlCommand": "curl http://localhost -H 'Authorization: Bearer sensitive'", "enabled": false, "schedule": map[string]any{"kind": "interval", "intervalSeconds": 60}}
	if code, _ = c.call("POST", "/api/tasks", body, false); code != 403 {
		t.Fatal("missing CSRF", code)
	}
	code, v = c.call("POST", "/api/tasks", body, true)
	if code != 201 {
		t.Fatalf("create %d %v", code, v)
	}
	id := v["data"].(map[string]any)["id"].(string)
	_, v = c.call("GET", "/api/tasks", nil, false)
	raw, _ := json.Marshal(v)
	if bytes.Contains(raw, []byte("sensitive")) {
		t.Fatal("raw command leaked into list")
	}
	code, v = c.call("GET", "/api/tasks/"+id, nil, false)
	if code != 200 || v["data"].(map[string]any)["curlCommand"] != body["curlCommand"] {
		t.Fatal(v)
	}
	if code, _ = c.call("POST", "/api/tasks/"+id+"/run", nil, true); code != 202 {
		t.Fatal("run", code)
	}
	for i := 0; i < 100; i++ {
		_, v = c.call("GET", "/api/runs", nil, false)
		data := v["data"].(map[string]any)
		rows := data["items"].([]any)
		if len(rows) > 0 && rows[0].(map[string]any)["state"] == "success" {
			break
		}
		if i == 99 {
			t.Fatal("run not saved", v)
		}
		time.Sleep(10 * time.Millisecond)
	}
	req, _ := http.NewRequest("DELETE", c.url+"/api/tasks/"+id, nil)
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("X-CSRF-Token", c.csrf)
	resp, err := c.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatal("cross-site mutation", resp.StatusCode)
	}
	if code, _ = c.call("DELETE", "/api/tasks/"+id, nil, true); code != 200 {
		t.Fatal("delete", code)
	}
	if code, _ = c.call("POST", "/api/auth/logout", nil, true); code != 200 {
		t.Fatal(code)
	}
	if code, _ = c.call("GET", "/api/tasks", nil, false); code != 401 {
		t.Fatal("logout", code)
	}
}
func TestLoginRateLimit(t *testing.T) {
	c := setupClient(t)
	_, v := c.call("GET", "/api/setup/status", nil, false)
	c.csrf = v["data"].(map[string]any)["csrf"].(string)
	c.call("POST", "/api/setup", map[string]string{"username": "admin", "password": "password123"}, true)
	for i := 0; i < 6; i++ {
		code, _ := c.call("POST", "/api/auth/login", map[string]string{"username": "admin", "password": "wrongpass"}, false)
		if i < 5 && code != 401 {
			t.Fatal(code)
		}
		if i == 5 && code != 429 {
			t.Fatal("no rate limit", code)
		}
	}
}
func TestRejectInvalidTask(t *testing.T) {
	c := setupClient(t)
	_, v := c.call("GET", "/api/setup/status", nil, false)
	c.csrf = v["data"].(map[string]any)["csrf"].(string)
	creds := map[string]string{"username": "admin", "password": "password123"}
	c.call("POST", "/api/setup", creds, true)
	_, v = c.call("POST", "/api/auth/login", creds, false)
	c.csrf = v["data"].(map[string]any)["csrf"].(string)
	code, v := c.call("POST", "/api/tasks", map[string]any{"name": "bad", "type": "curl", "curlCommand": "curl x && echo y", "schedule": map[string]any{"kind": "interval", "intervalSeconds": 60}}, true)
	if code != 400 || !strings.Contains(v["error"].(map[string]any)["message"].(string), "Shell") {
		t.Fatal(code, v)
	}
}
