package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// Model the documented prefix location, trailing-slash proxy_pass and Cookie rewrite.
func mountProxy(t *testing.T, c *client, prefix string) {
	t.Helper()
	target, err := url.Parse(c.url)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	director := proxy.Director
	proxy.Director = func(r *http.Request) {
		director(r)
		r.URL.Path = strings.TrimPrefix(r.URL.Path, prefix)
		r.Host = target.Host // Deliberately differ from the public Host and Origin.
		r.Header.Set("X-Forwarded-Proto", "http")
	}
	proxy.ModifyResponse = func(response *http.Response) error {
		cookies := response.Cookies()
		response.Header.Del("Set-Cookie")
		for _, cookie := range cookies {
			if strings.HasPrefix(cookie.Path, "/") {
				cookie.Path = prefix + cookie.Path
			}
			response.Header.Add("Set-Cookie", cookie.String())
		}
		return nil
	}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if prefix != "" && r.URL.Path == prefix {
			target := prefix + "/"
			if r.URL.RawQuery != "" {
				target += "?" + r.URL.RawQuery
			}
			w.Header().Set("Location", target)
			w.WriteHeader(http.StatusPermanentRedirect)
			return
		}
		if !strings.HasPrefix(r.URL.Path, prefix+"/") {
			http.NotFound(w, r)
			return
		}
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(s.Close)
	c.url = s.URL + prefix
}

func TestPublicResourcesUnderStrippingProxy(t *testing.T) {
	for _, prefix := range []string{"", "/scheduler", "/tools/scheduler"} {
		t.Run("mount"+prefix, func(t *testing.T) {
			c := setupClient(t)
			mountProxy(t, c, prefix)
			base, _ := url.Parse(c.url + "/")
			home, err := c.http.Get(base.String())
			if err != nil {
				t.Fatal(err)
			}
			html, _ := io.ReadAll(home.Body)
			home.Body.Close()
			if home.StatusCode != 200 {
				t.Fatal("home", home.StatusCode)
			}
			refs := regexp.MustCompile(`(?:src|href)="([^"#]+)"`).FindAllSubmatch(html, -1)
			if len(refs) != 8 {
				t.Fatal("unexpected resource/link count", len(refs))
			}
			for _, ref := range refs {
				relative, err := url.Parse(string(ref[1]))
				if err != nil {
					t.Fatal(err)
				}
				target := base.ResolveReference(relative)
				if !strings.HasPrefix(target.String(), base.String()) {
					t.Fatalf("reference %q leaves public directory %s", ref[1], base)
				}
				for _, method := range []string{"GET", "HEAD"} {
					r, _ := http.NewRequest(method, target.String(), nil)
					response, err := c.http.Do(r)
					if err != nil {
						t.Fatal(err)
					}
					body, _ := io.ReadAll(response.Body)
					response.Body.Close()
					if response.StatusCode != 200 || (method == "HEAD") != (len(body) == 0) {
						t.Fatalf("%s %s: status %d, body length %d", method, target, response.StatusCode, len(body))
					}
				}
			}
			manifestURL := base.ResolveReference(&url.URL{Path: "manifest.webmanifest"})
			response, err := c.http.Get(manifestURL.String())
			if err != nil {
				t.Fatal(err)
			}
			var manifest struct {
				StartURL string `json:"start_url"`
				Scope    string
				Icons    []struct{ Src string }
			}
			err = json.NewDecoder(response.Body).Decode(&manifest)
			response.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			for _, value := range []string{manifest.StartURL, manifest.Scope} {
				relative, _ := url.Parse(value)
				if manifestURL.ResolveReference(relative).String() != base.String() {
					t.Fatal("manifest leaves public directory", value)
				}
			}
			for _, icon := range manifest.Icons {
				relative, _ := url.Parse(icon.Src)
				target := manifestURL.ResolveReference(relative)
				if !strings.HasPrefix(target.String(), base.String()) {
					t.Fatal("icon leaves public directory")
				}
				response, err := c.http.Get(target.String())
				if err != nil {
					t.Fatal(err)
				}
				response.Body.Close()
				if response.StatusCode != 200 {
					t.Fatal("icon", response.StatusCode)
				}
			}
			if code, _ := c.call("GET", "/health", nil, false); code != 200 {
				t.Fatal("public health", code)
			}
		})
	}
}

func assertProxyCookie(t *testing.T, cookies []*http.Cookie, name, prefix string, cleared bool) {
	t.Helper()
	if len(cookies) != 1 {
		t.Fatal("expected one cookie", len(cookies))
	}
	cookie := cookies[0]
	if cookie.Name != name || cookie.Path != prefix+"/" || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || (cookie.MaxAge < 0) != cleared {
		t.Fatalf("unexpected cookie attributes: name=%s path=%s age=%d", cookie.Name, cookie.Path, cookie.MaxAge)
	}
}

func TestAuthenticationUnderStrippingProxy(t *testing.T) {
	for _, prefix := range []string{"", "/scheduler", "/tools/scheduler"} {
		t.Run("mount"+prefix, func(t *testing.T) {
			c := setupClient(t)
			mountProxy(t, c, prefix)
			c.headers = map[string]string{"Origin": "https://public.example:36443", "Sec-Fetch-Site": "cross-site"}
			_, result, cookies := c.callWithCookies("GET", "/api/setup/status", nil, false)
			assertProxyCookie(t, cookies, "scheduler_setup", prefix, false)
			c.csrf = result["data"].(map[string]any)["csrf"].(string)
			credentials := map[string]string{"username": "admin", "password": "test-password"}
			if code, _ := c.call("POST", "/api/setup", credentials, false); code != 403 {
				t.Fatal("setup requires CSRF", code)
			}
			code, _, cookies := c.callWithCookies("POST", "/api/setup", credentials, true)
			if code != 201 {
				t.Fatal("setup through proxy", code)
			}
			assertProxyCookie(t, cookies, "scheduler_setup", prefix, true)
			code, result, cookies = c.callWithCookies("POST", "/api/auth/login", credentials, false)
			if code != 200 {
				t.Fatal("login through proxy", code)
			}
			assertProxyCookie(t, cookies, "scheduler_session", prefix, false)
			c.csrf = result["data"].(map[string]any)["csrf"].(string)
			body := map[string]any{"name": "disabled proxy test", "type": "http", "url": "http://127.0.0.1:1/", "enabled": false, "schedule": map[string]any{"kind": "interval", "intervalSeconds": 60}}
			if code, _ := c.call("POST", "/api/tasks", body, false); code != 403 {
				t.Fatal("management requires CSRF", code)
			}
			code, result = c.call("POST", "/api/tasks", body, true)
			if code != 201 {
				t.Fatal("save through proxy", code)
			}
			id := result["data"].(map[string]any)["id"].(string)
			if code, _ := c.call("GET", "/api/tasks/"+id, nil, false); code != 200 {
				t.Fatal("read through proxy", code)
			}
			if code, _ := c.call("DELETE", "/api/tasks/"+id, nil, true); code != 200 {
				t.Fatal("delete through proxy", code)
			}
			code, _, cookies = c.callWithCookies("POST", "/api/auth/logout", nil, true)
			if code != 200 {
				t.Fatal("logout through proxy", code)
			}
			assertProxyCookie(t, cookies, "scheduler_session", prefix, true)
			if code, _ := c.call("GET", "/api/tasks", nil, false); code != 401 {
				t.Fatal("logout must revoke session", code)
			}
		})
	}
}

func TestSourceHeadersDoNotRestrictAuthentication(t *testing.T) {
	for _, origin := range []string{"https://public.example:36443", "null", "invalid-origin", ""} {
		t.Run(origin, func(t *testing.T) {
			c := setupClient(t)
			c.headers = map[string]string{"Origin": origin, "Sec-Fetch-Site": "cross-site"}
			_, result := c.call("GET", "/api/setup/status", nil, false)
			c.csrf = result["data"].(map[string]any)["csrf"].(string)
			credentials := map[string]string{"username": "admin", "password": "test-password"}
			if code, _ := c.call("POST", "/api/setup", credentials, true); code != 201 {
				t.Fatal("setup rejected source headers", code)
			}
			if code, _ := c.call("POST", "/api/auth/login", map[string]string{"username": "admin", "password": "wrong-password"}, false); code != 401 {
				t.Fatal("login must validate credentials", code)
			}
			code, result := c.call("POST", "/api/auth/login", credentials, false)
			if code != 200 {
				t.Fatal("login rejected source headers", code)
			}
			token := result["data"].(map[string]any)["csrf"].(string)
			c.csrf = "wrong-token"
			if code, _ := c.call("POST", "/api/auth/logout", nil, true); code != 403 {
				t.Fatal("logout must validate CSRF", code)
			}
			c.csrf = token
			if code, _ := c.call("POST", "/api/auth/logout", nil, true); code != 200 {
				t.Fatal("logout rejected source headers", code)
			}
		})
	}
}

func TestCookieSecureBehindHTTPSProxy(t *testing.T) {
	for _, scenario := range []struct {
		name, target, forwarded string
		secure                  bool
	}{
		{"http", "http://backend/", "", false},
		{"tls", "https://backend/", "", true},
		{"proxy-https", "http://backend/", "https", true},
		{"proxy-http", "http://backend/", "http", false},
		{"tls-proxy-http", "https://backend/", "http", true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			for _, name := range []string{"scheduler_setup", "scheduler_session"} {
				request := httptest.NewRequest("GET", scenario.target, nil)
				request.Header.Set("X-Forwarded-Proto", scenario.forwarded)
				response := httptest.NewRecorder()
				cookie(response, request, name, "test-value", 600)
				cookies := response.Result().Cookies()
				if len(cookies) != 1 || cookies[0].Secure != scenario.secure || !cookies[0].HttpOnly || cookies[0].Path != "/" {
					t.Fatal("unexpected cookie transport attributes", name)
				}
			}
		})
	}
}
