package executor

import (
	"context"
	"encoding/json"
	"github.com/jiuwusan/z-task-scheduler/internal/domain"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func dockerFixture(t *testing.T, label bool, slow bool) (*Docker, *atomic.Int32) {
	t.Helper()
	calls := &atomic.Int32{}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/version":
			json.NewEncoder(w).Encode(map[string]string{"ApiVersion": "1.45", "MinAPIVersion": "1.24"})
		case "/v1.45/containers/json":
			json.NewEncoder(w).Encode([]map[string]any{{"Id": "target-id", "Names": []string{"/test"}, "State": "exited"}, {"Id": "self-id", "Names": []string{"/z-task-scheduler"}, "State": "running"}})
		case "/v1.45/containers/z-task-scheduler/json":
			v := "false"
			if label {
				v = "true"
			}
			json.NewEncoder(w).Encode(map[string]any{"Id": "self-id", "Config": map[string]any{"Labels": map[string]string{"io.jiuwusan.z-task-scheduler": v}}, "State": map[string]bool{"Running": true}})
		case "/v1.45/containers/target-id/json":
			json.NewEncoder(w).Encode(map[string]any{"Id": "target-id", "State": map[string]bool{"Running": false}})
		case "/v1.45/containers/self-id/json":
			json.NewEncoder(w).Encode(map[string]any{"Id": "self-id", "State": map[string]bool{"Running": true}})
		case "/v1.45/containers/target-id/start", "/v1.45/containers/target-id/restart":
			calls.Add(1)
			if slow {
				<-r.Context().Done()
				return
			}
			w.WriteHeader(204)
		default:
			http.Error(w, `{"message":"No such container"}`, 404)
		}
	}))
	t.Cleanup(s.Close)
	d := NewDocker("", "z-task-scheduler", "io.jiuwusan.z-task-scheduler")
	d.client = s.Client()
	d.baseURL = s.URL
	return d, calls
}
func TestDockerControlAndSelfProtection(t *testing.T) {
	d, calls := dockerFixture(t, true, false)
	ctx := context.Background()
	list, err := d.List(ctx)
	if err != nil || len(list) != 2 || !list[1].Self {
		t.Fatal(list, err)
	}
	for _, action := range []string{"start", "stop", "restart"} {
		r := d.Execute(ctx, domain.Task{ContainerID: "target-id", DockerAction: action, TimeoutSeconds: 60})
		if r.State != "success" {
			t.Fatalf("%s %+v", action, r)
		}
	}
	if calls.Load() != 2 {
		t.Fatal("unexpected writes", calls.Load())
	}
	r := d.Execute(ctx, domain.Task{ContainerID: "self-id", DockerAction: "stop"})
	if r.State != "failed" || !strings.Contains(r.Error, "自身") {
		t.Fatalf("%+v", r)
	}
	if err = d.ValidateTarget(ctx, "recreated-id"); err == nil {
		t.Fatal("missing ID accepted")
	}
}
func TestDockerUnavailableIdentityAndUnknownResult(t *testing.T) {
	d, calls := dockerFixture(t, false, false)
	r := d.Execute(context.Background(), domain.Task{ContainerID: "target-id", DockerAction: "start"})
	if r.State != "failed" || calls.Load() != 0 {
		t.Fatalf("%+v", r)
	}
	d, calls = dockerFixture(t, true, true)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	r = d.Execute(ctx, domain.Task{ContainerID: "target-id", DockerAction: "start"})
	if r.State != "unknown" || calls.Load() != 1 {
		t.Fatalf("%+v calls=%d", r, calls.Load())
	}
}
