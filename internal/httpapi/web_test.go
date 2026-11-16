package httpapi

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Static installation assets must stay available before administrator login.
// Nil business dependencies ensure this check cannot run tasks or access Docker.
func TestInstallAssetsBeforeLogin(t *testing.T) {
	for _, prefix := range []string{"", "/tools/scheduler"} {
		handler := http.StripPrefix(prefix, New(nil, nil, nil))
		for _, name := range []string{"manifest.webmanifest", "install.js", "install.css", "icon-180.png", "icon-192.png", "icon-512.png"} {
			for _, method := range []string{"GET", "HEAD"} {
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, httptest.NewRequest(method, prefix+"/"+name, nil))
				if response.Code != http.StatusOK {
					t.Errorf("%s %s requires login or is missing: %d", method, name, response.Code)
				}
				if !strings.Contains(response.Header().Get("Content-Security-Policy"), "script-src 'self'") || response.Header().Get("Cache-Control") != "no-store" {
					t.Errorf("%s lost static security policy", name)
				}
			}
		}
	}
}

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
