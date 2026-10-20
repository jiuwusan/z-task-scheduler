package executor

import (
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
