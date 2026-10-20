package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/jiuwusan/z-task-scheduler/internal/domain"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"
)

func requestTask(url string) domain.Task { return domain.Task{Name: "request", Type: "http", URL: url} }
func TestHTTPJSONAndTextRequests(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		fmt.Fprintf(w, "%s|%s|%s|%s", r.Method, r.Header.Get("X-Test"), r.Header.Get("Content-Type"), body)
	}))
	defer s.Close()
	task := requestTask(s.URL)
	task.Method = "post"
	task.Headers = map[string]string{"X-Test": "yes"}
	task.Data = json.RawMessage(`{"id":9007199254740993,"enabled":true}`)
	result := NewHTTP().Execute(context.Background(), task)
	if result.State != "success" || result.HTTPStatus != 200 || result.Output != `POST|yes|application/json|{"id":9007199254740993,"enabled":true}` {
		t.Fatalf("%+v", result)
	}
	task.Method = "PUT"
	task.Data = json.RawMessage(`"plain\ntext"`)
	task.Headers["Content-Type"] = "text/plain"
	result = NewHTTP().Execute(context.Background(), task)
	if result.Output != "PUT|yes|text/plain|plain\ntext" {
		t.Fatalf("%+v", result)
	}
	task = requestTask(s.URL)
	result = NewHTTP().Execute(context.Background(), task)
	if result.Output != "GET|||" {
		t.Fatalf("default GET %+v", result)
	}
}
func TestHTTPErrorRedirectAndNoRetry(t *testing.T) {
	var calls atomic.Int32
	var redirected atomic.Int32
	dest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected.Add(1) }))
	defer dest.Close()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, dest.URL, 302)
			return
		}
		w.WriteHeader(500)
		fmt.Fprint(w, "error body")
	}))
	defer s.Close()
	task := requestTask(s.URL)
	result := NewHTTP().Execute(context.Background(), task)
	if result.State != "failed" || result.HTTPStatus != 500 || result.Output != "error body" || calls.Load() != 1 {
		t.Fatalf("%+v calls=%d", result, calls.Load())
	}
	task.URL += "/redirect"
	result = NewHTTP().Execute(context.Background(), task)
	if result.HTTPStatus != 302 || result.State != "success" || redirected.Load() != 0 {
		t.Fatalf("redirect: %+v", result)
	}
}
func TestHTTPTimeoutAndReadFailure(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/broken" {
			w.Header().Set("Content-Length", "100")
			fmt.Fprint(w, "partial")
			return
		}
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	result := NewHTTP().Execute(ctx, requestTask(s.URL))
	if result.State != "timeout" {
		t.Fatalf("%+v", result)
	}
	result = NewHTTP().Execute(context.Background(), requestTask(s.URL+"/broken"))
	if result.State != "failed" || !strings.Contains(result.Output, "partial") {
		t.Fatalf("%+v", result)
	}
}
func TestHTTPOutputRedactionAndLimit(t *testing.T) {
	secret := strings.Repeat("private123", 12)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/invalid" {
			w.Write(bytes.Repeat([]byte{0xff, 'a'}, 40000))
			return
		}
		fmt.Fprint(w, secret+strings.Repeat("x", 65536)+secret+"cookie-secret|body-secret|query-secret")
	}))
	defer s.Close()
	task := requestTask(s.URL + "?token=query-secret")
	task.Method = "POST"
	task.Headers = map[string]string{"Authorization": "Bearer " + secret, "Cookie": "session=cookie-secret"}
	task.Data = json.RawMessage(`{"password":"body-secret"}`)
	result := NewHTTP().Execute(context.Background(), task)
	if result.State != "success" || !result.Truncated || len(result.Output) > 65536 || strings.Contains(result.Output, "private123") || strings.Contains(result.Output, "cookie-secret") || strings.Contains(result.Output, "body-secret") || strings.Contains(result.Output, "query-secret") {
		t.Fatalf("len=%d result=%s", len(result.Output), result.State)
	}
	task.URL = s.URL + "/invalid"
	result = NewHTTP().Execute(context.Background(), task)
	if len(result.Output) > 65536 || !utf8.ValidString(result.Output) {
		t.Fatalf("invalid output len=%d", len(result.Output))
	}
}
func TestHTTPInvalidInputNeverSends(t *testing.T) {
	var calls atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer s.Close()
	for _, mutate := range []func(*domain.Task){func(t *domain.Task) { t.URL = "file:///etc/passwd" }, func(t *domain.Task) { t.Headers = map[string]string{"X-Test": "x\r\nInjected: y"} }, func(t *domain.Task) { t.Method = "GET\r\nPOST" }, func(t *domain.Task) { t.Data = json.RawMessage(`123`) }} {
		task := requestTask(s.URL)
		mutate(&task)
		result := NewHTTP().Execute(context.Background(), task)
		if result.State != "failed" {
			t.Fatalf("%+v", result)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("invalid request sent")
	}
}

func TestHTTPRedactsEchoedCredentials(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.NewEncoder(w).Encode(map[string]string{"header": r.Header.Get("X-Api-Key"), "cookie": r.Header.Get("Cookie"), "query": r.URL.Query().Get("token"), "echo": string(body)})
	}))
	defer s.Close()
	task := requestTask(s.URL + "?token=query-private")
	task.Method = "POST"
	task.Headers = map[string]string{"X-Api-Key": "header-private", "Cookie": "session=cookie-private"}
	task.Data = json.RawMessage(`{"password":"body\"&private"}`)
	result := NewHTTP().Execute(context.Background(), task)
	if result.State != "success" || strings.Contains(result.Output, "private") {
		t.Fatalf("credential echo leak: %+v", result)
	}
}

func TestHTTPRedactsHeadersAfterWireWhitespaceNormalization(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s|%s", r.Header.Get("Authorization"), r.Header.Get("X-Api-Key"))
	}))
	defer s.Close()
	for _, whitespace := range []string{"  ", "\t"} {
		task := requestTask(s.URL)
		task.Headers = map[string]string{"Authorization": whitespace + "Bearer private-secret" + whitespace, "X-Api-Key": whitespace + "private-key" + whitespace}
		result := NewHTTP().Execute(context.Background(), task)
		if result.State != "success" || strings.Contains(result.Output, "private") {
			t.Fatalf("credential echo leak: %+v", result)
		}
	}
}
