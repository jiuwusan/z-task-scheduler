package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image/png"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestInstallAssets(t *testing.T) {
	for _, prefix := range []string{"", "/tools/panel"} {
		handler := http.StripPrefix(prefix, Handler())
		for name, contentType := range map[string]string{
			"manifest.webmanifest": "application/manifest+json", "install.js": "javascript", "install.css": "text/css",
			"icon-180.png": "image/png", "icon-192.png": "image/png", "icon-512.png": "image/png",
		} {
			for _, method := range []string{"GET", "HEAD"} {
				r := httptest.NewRecorder()
				handler.ServeHTTP(r, httptest.NewRequest(method, prefix+"/"+name, nil))
				if r.Code != 200 || !strings.Contains(r.Header().Get("Content-Type"), contentType) {
					t.Errorf("%s %s: %d %s", method, name, r.Code, r.Header().Get("Content-Type"))
				}
				if (method == "HEAD") != (r.Body.Len() == 0) {
					t.Errorf("%s %s: unexpected body size", method, name)
				}
			}
		}
		for _, name := range []string{"embed.go", "install_test.go", "README.md", "missing.png"} {
			r := httptest.NewRecorder()
			handler.ServeHTTP(r, httptest.NewRequest("GET", prefix+"/"+name, nil))
			if r.Code != 404 {
				t.Errorf("unexpected public file %s: %d", name, r.Code)
			}
		}
	}
}

func TestInstallManifestAndHTML(t *testing.T) {
	files, err := fs.Sub(assets, ".")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := fs.ReadFile(files, "manifest.webmanifest")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Name     string
		ID       string
		StartURL string `json:"start_url"`
		Scope    string
		Display  string
		Icons    []struct{ Src, Sizes, Type string }
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Name == "" || manifest.ID != "" || manifest.StartURL != "./" || manifest.Scope != "./" || manifest.Display != "standalone" {
		t.Fatalf("invalid mount-scoped manifest: %s", raw)
	}
	for _, base := range []string{"https://example.test/", "https://example.test/tools/panel/"} {
		manifestURL, _ := url.Parse(base + "manifest.webmanifest")
		for _, value := range []string{manifest.StartURL, manifest.Scope} {
			relative, err := url.Parse(value)
			if err != nil || manifestURL.ResolveReference(relative).String() != base {
				t.Fatalf("URL %q leaves mount %s", value, base)
			}
		}
	}
	for _, size := range []int{180, 192, 512} {
		name := fmt.Sprintf("icon-%d.png", size)
		raw, err := fs.ReadFile(files, name)
		if err != nil {
			t.Fatal(err)
		}
		config, err := png.DecodeConfig(bytes.NewReader(raw))
		if err != nil || config.Width != size || config.Height != size {
			t.Fatalf("%s: invalid PNG %+v %v", name, config, err)
		}
		if size == 180 {
			continue
		}
		found := false
		for _, icon := range manifest.Icons {
			if icon.Src == "./"+name && icon.Sizes == fmt.Sprintf("%dx%d", size, size) && icon.Type == "image/png" {
				found = true
			}
		}
		if !found {
			t.Errorf("missing manifest icon %s", name)
		}
	}
	html, err := fs.ReadFile(files, "index.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{`href="./manifest.webmanifest"`, `href="./icon-180.png"`, `src="./install.js"`, `href="./install.css"`, `id="install-button"`, `aria-labelledby="install-title"`} {
		if !bytes.Contains(html, []byte(marker)) {
			t.Errorf("missing HTML marker %s", marker)
		}
	}
}
