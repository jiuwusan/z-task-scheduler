package executor

import (
	"encoding/base64"
	"net/url"
	"sort"
	"strings"
)

const outputLimit = 64 * 1024

// Redact before truncating; retain a match window across OS pipe writes.
type redactingOutput struct {
	data, pending []byte
	keys          []string
	window, total int
}

func newOutput(keys []string) *redactingOutput {
	b := &redactingOutput{keys: keys, window: 1}
	for _, k := range keys {
		b.window = max(b.window, len(k))
	}
	return b
}
func (b *redactingOutput) append(p []byte) {
	b.total += len(p)
	if len(b.data) < outputLimit {
		b.data = append(b.data, p[:min(len(p), outputLimit-len(b.data))]...)
	}
}
func (b *redactingOutput) Write(p []byte) (int, error) {
	n := len(p)
	b.pending = append(b.pending, p...)
	b.drain(false)
	return n, nil
}
func (b *redactingOutput) drain(final bool) {
	limit := len(b.pending)
	if !final {
		limit = max(0, limit-b.window+1)
	}
	i := 0
	for i < limit {
		matched := false
		for _, key := range b.keys {
			if key != "" && len(b.pending)-i >= len(key) && string(b.pending[i:i+len(key)]) == key {
				b.append([]byte("[REDACTED]"))
				i += len(key)
				matched = true
				break
			}
		}
		if !matched {
			b.append(b.pending[i : i+1])
			i++
		}
	}
	b.pending = append([]byte(nil), b.pending[i:]...)
}

type metadataOutput struct {
	to     *redactingOutput
	tail   []byte
	marker string
}

func (m *metadataOutput) Write(p []byte) (int, error) {
	n := len(p)
	m.tail = append(m.tail, p...)
	keep := len(m.marker) + 64
	if len(m.tail) > keep {
		cut := len(m.tail) - keep
		m.to.Write(m.tail[:cut])
		m.tail = append([]byte(nil), m.tail[cut:]...)
	}
	return n, nil
}
func (m *metadataOutput) finish() string {
	tail := string(m.tail)
	code := ""
	if i := strings.LastIndex(tail, m.marker); i >= 0 {
		fields := strings.Fields(tail[i+len(m.marker):])
		if len(fields) > 0 {
			code = fields[0]
		}
		tail = strings.TrimSuffix(tail[:i], "\n")
	}
	m.to.Write([]byte(tail))
	m.to.drain(true)
	return code
}

func sensitive(name string) bool {
	n := strings.ToLower(name)
	return strings.Contains(n, "authorization") || strings.Contains(n, "cookie") || strings.Contains(n, "token") || strings.Contains(n, "password") || strings.Contains(n, "secret") || strings.Contains(n, "api-key") || strings.Contains(n, "api_key") || n == "key"
}
func secrets(args []string) []string {
	opts, _ := curlOptions(args)
	out := []string{}
	cookie := func(value string) {
		out = append(out, value)
		for _, part := range strings.Split(value, ";") {
			if _, v, ok := strings.Cut(part, "="); ok {
				out = append(out, strings.TrimSpace(v))
			}
		}
	}
	for _, o := range opts {
		value := o.value
		switch o.name {
		case "header", "proxy-header":
			if name, v, ok := strings.Cut(value, ":"); ok && sensitive(name) {
				v = strings.TrimSpace(v)
				out = append(out, v)
				if strings.Contains(strings.ToLower(name), "cookie") {
					cookie(v)
				}
				if _, token, ok := strings.Cut(v, " "); ok {
					out = append(out, token)
				}
			}
		case "user", "proxy-user":
			out = append(out, value, base64.StdEncoding.EncodeToString([]byte(value)))
			if _, p, ok := strings.Cut(value, ":"); ok {
				out = append(out, p)
			}
		case "cookie":
			cookie(value)
		case "oauth2-bearer", "pass", "proxy-pass", "tlspassword", "proxy-tlspassword":
			out = append(out, value)
		}
		if u, err := url.Parse(value); err == nil && u.Host != "" {
			if u.User != nil {
				if p, ok := u.User.Password(); ok {
					out = append(out, p, base64.StdEncoding.EncodeToString([]byte(u.User.Username()+":"+p)))
				}
			}
			for k, vs := range u.Query() {
				if sensitive(k) {
					out = append(out, vs...)
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return len(out[i]) > len(out[j]) })
	keys := []string{}
	for _, s := range out {
		if s != "" {
			keys = append(keys, s)
		}
	}
	return keys
}
