package executor

import (
	"context"
	"encoding/json"
	"fmt"
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
		case "/v1.45/containers/target-id/json", "/v1.45/containers/test/json":
			json.NewEncoder(w).Encode(map[string]any{"Id": "target-id", "Name": "/test", "State": map[string]bool{"Running": false}})
		case "/v1.45/containers/self-id/json":
			json.NewEncoder(w).Encode(map[string]any{"Id": "self-id", "State": map[string]bool{"Running": true}})
		case "/v1.45/containers/target-id/start", "/v1.45/containers/target-id/restart":
			calls.Add(1)
			if slow {
				select {
				case <-r.Context().Done():
					return
				case <-time.After(10200 * time.Millisecond):
				}
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
	d.hostname = "self-id"
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
		r := d.Execute(ctx, domain.Task{ContainerID: "target-id", ContainerName: "test", DockerAction: action, TimeoutSeconds: 60})
		if r.State != "success" {
			t.Fatalf("%s %+v", action, r)
		}
	}
	if calls.Load() != 2 {
		t.Fatal("unexpected writes", calls.Load())
	}
	r := d.Execute(ctx, domain.Task{ContainerID: "self-id", ContainerName: "z-task-scheduler", DockerAction: "stop"})
	if r.State != "failed" || !strings.Contains(r.Error, "自身") {
		t.Fatalf("%+v", r)
	}
	if err = d.ValidateTarget(ctx, "recreated-id"); err == nil {
		t.Fatal("missing ID accepted")
	}
}
func TestDockerUnavailableIdentityAndUnknownResult(t *testing.T) {
	d, calls := dockerFixture(t, false, false)
	r := d.Execute(context.Background(), domain.Task{ContainerID: "target-id", ContainerName: "test", DockerAction: "start"})
	if r.State != "failed" || calls.Load() != 0 {
		t.Fatalf("%+v", r)
	}
	d, calls = dockerFixture(t, true, true)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	r = d.Execute(ctx, domain.Task{ContainerID: "target-id", ContainerName: "test", DockerAction: "start"})
	if r.State != "unknown" || calls.Load() != 1 {
		t.Fatalf("%+v calls=%d", r, calls.Load())
	}
}

func TestDockerRejectsForeignInstance(t *testing.T) {
	d, calls := dockerFixture(t, true, false)
	d.hostname = "foreign-host"
	r := d.Execute(context.Background(), domain.Task{ContainerID: "target-id", ContainerName: "test", DockerAction: "start"})
	if r.State != "failed" || calls.Load() != 0 {
		t.Fatalf("foreign instance accepted: %+v", r)
	}
}

func TestDockerHonorsTaskDeadlineBeyondTenSeconds(t *testing.T) {
	d, _ := dockerFixture(t, true, true)
	configured := NewDocker("", "z-task-scheduler", "io.jiuwusan.z-task-scheduler")
	configured.client.Transport = d.client.Transport
	d.client = configured.client
	ctx, cancel := context.WithTimeout(context.Background(), 13*time.Second)
	defer cancel()
	r := d.Execute(ctx, domain.Task{ContainerID: "target-id", ContainerName: "test", DockerAction: "restart", TimeoutSeconds: 13})
	if r.State != "success" {
		t.Fatalf("request cut short before task timeout: %+v", r)
	}
}

func TestDockerResolvesNameBeforeEveryExecution(t *testing.T) {
	var generation atomic.Int32
	generation.Store(1)
	writes := make(chan string, 4)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/version":
			json.NewEncoder(w).Encode(map[string]string{"ApiVersion": "1.45"})
		case "/v1.45/containers/z-task-scheduler/json":
			json.NewEncoder(w).Encode(map[string]any{"Id": "self-id", "Name": "/z-task-scheduler", "Config": map[string]any{"Labels": map[string]string{"io.jiuwusan.z-task-scheduler": "true"}}})
		case "/v1.45/containers/media/json":
			switch generation.Load() {
			case 0:
				http.Error(w, "missing", 404)
			case 3:
				json.NewEncoder(w).Encode(map[string]any{"Id": "self-id", "Name": "/media"})
			case 4:
				json.NewEncoder(w).Encode(map[string]any{"Id": "unrelated-id", "Name": "/different-name"})
			default:
				json.NewEncoder(w).Encode(map[string]any{"Id": fmt.Sprintf("generation-%d", generation.Load()), "Name": "/media"})
			}
		case "/v1.45/containers/generation-1/start", "/v1.45/containers/generation-2/start":
			writes <- r.URL.Path
			w.WriteHeader(204)
		default:
			http.Error(w, "unexpected target", 404)
		}
	}))
	defer s.Close()
	d := NewDocker("", "z-task-scheduler", "io.jiuwusan.z-task-scheduler")
	d.client, d.baseURL, d.hostname = s.Client(), s.URL, "self-id"
	// The old ID remains in legacy tasks; it must never control execution.
	task := domain.Task{ContainerName: "media", ContainerID: "stale-id", DockerAction: "start"}
	for _, version := range []int32{1, 2} {
		generation.Store(version)
		if err := d.ValidateTarget(context.Background(), "media"); err != nil {
			t.Fatal(err)
		}
		if result := d.Execute(context.Background(), task); result.State != "success" {
			t.Fatalf("generation %d: %+v", version, result)
		}
	}
	if len(writes) != 2 {
		t.Fatal("unexpected number of writes", len(writes))
	}
	if first, second := <-writes, <-writes; !strings.Contains(first, "generation-1") || !strings.Contains(second, "generation-2") {
		t.Fatal("did not follow recreated container", first, second)
	}
	for _, version := range []int32{0, 3, 4} {
		generation.Store(version)
		if result := d.Execute(context.Background(), task); result.State != "failed" {
			t.Fatalf("missing/self target accepted: %+v", result)
		}
	}
	if len(writes) != 0 {
		t.Fatal("wrote to missing/self target", len(writes))
	}
}
