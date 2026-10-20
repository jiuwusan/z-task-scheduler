package executor

import (
	"encoding/base64"
	"net/url"
	"sort"
	"strings"
)

const outputLimit = 64 * 1024

type boundedOutput struct {
	data  []byte
	tail  []byte
	total int
	limit int
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	n := len(p)
	b.total += n
	if len(b.data) < b.limit {
		b.data = append(b.data, p[:min(n, b.limit-len(b.data))]...)
	}
	b.tail = append(b.tail, p...)
	if len(b.tail) > 4096 {
		b.tail = append([]byte(nil), b.tail[len(b.tail)-4096:]...)
	}
	return n, nil
}
func sensitive(name string) bool {
	n := strings.ToLower(name)
	return strings.Contains(n, "authorization") || strings.Contains(n, "cookie") || strings.Contains(n, "token") || strings.Contains(n, "password") || strings.Contains(n, "secret") || strings.Contains(n, "api-key") || strings.Contains(n, "api_key") || n == "key"
}
func secrets(args []string) []string {
	var out []string
	for i, a := range args {
		value := ""
		if i+1 < len(args) {
			value = args[i+1]
		}
		if a == "-H" || a == "--header" {
			if name, v, ok := strings.Cut(value, ":"); ok && sensitive(name) {
				v = strings.TrimSpace(v)
				out = append(out, v)
				if _, token, ok := strings.Cut(v, " "); ok {
					out = append(out, token)
				}
			}
		}
		if a == "-u" || a == "--user" || a == "--proxy-user" || a == "-U" {
			out = append(out, value, base64.StdEncoding.EncodeToString([]byte(value)))
			if _, password, ok := strings.Cut(value, ":"); ok {
				out = append(out, password)
			}
		}
		if a == "-b" || a == "--cookie" {
			out = append(out, value)
		}
		if u, err := url.Parse(a); err == nil && u.Host != "" {
			if u.User != nil {
				if p, ok := u.User.Password(); ok {
					out = append(out, p)
				}
			}
			for k, vs := range u.Query() {
				if sensitive(k) {
					out = append(out, vs...)
				}
			}
		}
		if strings.HasPrefix(a, "--header=") {
			value = strings.TrimPrefix(a, "--header=")
			if n, v, ok := strings.Cut(value, ":"); ok && sensitive(n) {
				v = strings.TrimSpace(v)
				out = append(out, v)
				if _, token, ok := strings.Cut(v, " "); ok {
					out = append(out, token)
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return len(out[i]) > len(out[j]) })
	return out
}
func redact(s string, keys []string) string {
	for _, key := range keys {
		if key == "" {
			continue
		}
		s = strings.ReplaceAll(s, key, "[REDACTED]")
		for n := min(len(key)-1, len(s)); n > 0; n-- {
			if strings.HasSuffix(s, key[:n]) {
				s = s[:len(s)-n] + "[REDACTED]"
				break
			}
		}
	}
	return s
}
