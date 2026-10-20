package executor

import (
	"bytes"
	"context"
	"fmt"
	"github.com/jiuwusan/z-task-scheduler/internal/domain"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestCurlParsePreservesArguments(t *testing.T) {
	cases := []struct {
		command string
		want    []string
	}{
		{`curl 'http://localhost/?a=1&b=2' --data '{"enabled":true}'`, []string{"http://localhost/?a=1&b=2", "--data", `{"enabled":true}`}},
		{"curl \\\n 'http://localhost' -H \"X-Name: a\\\"b\" --data ''", []string{"http://localhost", "-H", `X-Name: a"b`, "--data", ""}},
		{`curl 'http://localhost' --data '$(echo test); x|y'`, []string{"http://localhost", "--data", "$(echo test); x|y"}},
	}
	for _, c := range cases {
		got, err := ParseCurl(c.command)
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q => %q %v", c.command, got, err)
		}
	}
	for _, bad := range []string{`curl x && echo y`, `curl x; echo y`, `curl 'x`, `curl x -wfoo`, `curl x --write-out=x`, `curl -K config`, `curl --next x`, `wget x`, `curl x | cat`} {
		if _, err := ParseCurl(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}
func TestCurlRoundTripAndHTTPFailure(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/fail" {
			w.WriteHeader(500)
			fmt.Fprint(w, "bad response")
			return
		}
		body, _ := io.ReadAll(r.Body)
		fmt.Fprintf(w, "%s|%s|%s", r.Method, r.Header.Get("X-Test"), body)
	}))
	defer s.Close()
	c := NewCurl("")
	r := c.Execute(context.Background(), domain.Task{CurlCommand: `curl '` + s.URL + `' -X POST -H 'X-Test: yes' --data '{"a":1}'`})
	if r.State != "success" || r.HTTPStatus != 200 || !strings.Contains(r.Output, `POST|yes|{"a":1}`) {
		t.Fatalf("%+v", r)
	}
	r = c.Execute(context.Background(), domain.Task{CurlCommand: "curl " + s.URL + "/fail"})
	if r.State != "failed" || r.HTTPStatus != 500 || !strings.Contains(r.Output, "bad response") {
		t.Fatalf("%+v", r)
	}
}
func TestCurlTimeoutAndOutputRedaction(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow" {
			<-r.Context().Done()
			return
		}
		fmt.Fprint(w, strings.Repeat("x", 65530)+"secret-value"+strings.Repeat("z", 70000))
	}))
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	r := NewCurl("").Execute(ctx, domain.Task{CurlCommand: "curl " + s.URL + "/slow"})
	if r.State != "timeout" {
		t.Fatalf("%+v", r)
	}
	r = NewCurl("").Execute(context.Background(), domain.Task{CurlCommand: "curl '" + s.URL + "' -H 'Authorization: Bearer secret-value'"})
	if len(r.Output) > 65536 || !r.Truncated || strings.Contains(r.Output, "secret") {
		t.Fatalf("len=%d truncated=%v tail=%q", len(r.Output), r.Truncated, r.Output[max(0, len(r.Output)-30):])
	}
	if r.HTTPStatus != 200 {
		t.Fatal("metadata lost after truncation", r)
	}
}
func TestCurlConnectionFailure(t *testing.T) {
	r := NewCurl("").Execute(context.Background(), domain.Task{CurlCommand: "curl --connect-timeout 1 http://127.0.0.1:1"})
	if r.State != "failed" || r.ExitCode == 0 {
		t.Fatalf("%+v", r)
	}
}

func TestCurlAttachedCredentialsRedacted(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, r.Header.Get("X-Api-Key")+"|"+r.Header.Get("Authorization"))
	}))
	defer s.Close()
	r := NewCurl("").Execute(context.Background(), domain.Task{CurlCommand: "curl '" + s.URL + "' -H'X-Api-Key: headersecret' -uuser:basicsecret"})
	if r.State != "success" || strings.Contains(r.Output, "headersecret") || strings.Contains(r.Output, "dXNlcjpiYXNpY3NlY3JldA==") {
		t.Fatalf("credential leak: %+v", r)
	}
}

func TestCurlCommonFailFlag(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "ok") }))
	defer s.Close()
	for _, flag := range []string{"-f", "-fsS", "--fail"} {
		r := NewCurl("").Execute(context.Background(), domain.Task{CurlCommand: "curl " + flag + " '" + s.URL + "'"})
		if r.State != "success" || r.HTTPStatus != 200 {
			t.Errorf("%s: %+v", flag, r)
		}
	}
}
func TestCurlOptionValuesAreLiteral(t *testing.T) {
	for _, body := range []string{"-wfoo", "--next", "-Kconf"} {
		args, err := ParseCurl("curl http://localhost --data '" + body + "'")
		if err != nil || args[len(args)-1] != body {
			t.Errorf("body %q rejected: %v", body, err)
		}
	}
	if _, err := ParseCurl("curl http://localhost -: http://localhost"); err == nil {
		t.Fatal("accepted --next short alias")
	}
}

func TestCurlRejectsStderrOverride(t *testing.T) {
	for _, cmd := range []string{"curl http://localhost --stderr -", "curl http://localhost --stderr=/tmp/out"} {
		if _, err := ParseCurl(cmd); err == nil {
			t.Fatal("accepted metadata redirect", cmd)
		}
	}
}
func TestCurlRedactsBeforeTruncationAcrossStreams(t *testing.T) {
	secret := strings.Repeat("private123", 12)
	body := secret + strings.Repeat("x", 65536+len("Bearer "+secret)-60-len(secret)) + secret
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
	defer s.Close()
	r := NewCurl("").Execute(context.Background(), domain.Task{CurlCommand: "curl -v '" + s.URL + "' -H 'Authorization: Bearer " + secret + "'"})
	if strings.Contains(r.Output, "private123") {
		t.Fatalf("secret fragment in output tail %q", r.Output[max(0, len(r.Output)-140):])
	}
}
func TestCurlOutputLimitAfterUTF8Repair(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(bytes.Repeat([]byte{0xff, 'a'}, 40000)) }))
	defer s.Close()
	r := NewCurl("").Execute(context.Background(), domain.Task{CurlCommand: "curl " + s.URL})
	if len(r.Output) > 65536 || !utf8.ValidString(r.Output) || !r.Truncated {
		t.Fatalf("len=%d truncated=%v", len(r.Output), r.Truncated)
	}
}
