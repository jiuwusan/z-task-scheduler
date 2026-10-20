package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestHTTPDefaultsAndSafeSnapshot(t *testing.T) {
	task := Task{Name: "HTTP", Type: "http", URL: "https://user:pass@example.com/api?token=secret&ok=1", Headers: map[string]string{"Authorization": "Bearer private"}, Data: json.RawMessage(`{"password":"private"}`)}
	if err := ValidateTask(&task); err != nil || task.Method != "GET" {
		t.Fatal(task, err)
	}
	b, err := json.Marshal(Run{Snapshot: task})
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"user:pass", "secret", "private"} {
		if strings.Contains(string(b), secret) {
			t.Fatalf("snapshot leak: %s", b)
		}
	}
	if task.Headers["Authorization"] != "Bearer private" {
		t.Fatal("redaction mutated saved task")
	}
}
