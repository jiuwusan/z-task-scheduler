package httpapi

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestEmbeddedWebAndUnknownAPI(t *testing.T) {
	c := setupClient(t)
	for _, path := range []string{"/", "/app.js", "/style.css"} {
		resp, err := c.http.Get(c.url + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || len(body) == 0 {
			t.Fatalf("%s %d", path, resp.StatusCode)
		}
		if path == "/" && !strings.Contains(string(body), "任务调度") {
			t.Fatal("missing dashboard")
		}
	}
	code, _ := c.call("GET", "/api/nonexistent", nil, false)
	if code != 404 {
		t.Fatal(code)
	}
}
