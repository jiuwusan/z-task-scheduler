package executor

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"github.com/jiuwusan/z-task-scheduler/internal/domain"
	"net/url"
	"sort"
	"strings"
)

const outputLimit = 64 * 1024

// Redact before truncating; preserve matches across network read boundaries.
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
func requestSecrets(t domain.Task) []string {
	keys := []string{}
	add := func(v string) {
		if v != "" {
			keys = append(keys, v, url.QueryEscape(v), url.PathEscape(v))
			variants := []string{v}
			// Cover JSON credentials and a response that JSON-encodes the request body.
			for depth := 0; depth < 2; depth++ {
				next := []string{}
				for _, value := range variants {
					for _, escapeHTML := range []bool{false, true} {
						var b bytes.Buffer
						encoder := json.NewEncoder(&b)
						encoder.SetEscapeHTML(escapeHTML)
						if encoder.Encode(value) == nil {
							encoded := strings.TrimSuffix(b.String(), "\n")
							encoded = encoded[1 : len(encoded)-1]
							keys = append(keys, encoded)
							next = append(next, encoded)
						}
					}
				}
				variants = next
			}
		}
	}
	for name, value := range t.Headers {
		if domain.SensitiveKey(name) {
			add(value)
			// net/http strips surrounding HTTP whitespace when writing headers.
			value = strings.Trim(value, " \t")
			add(value)
			if i := strings.IndexAny(value, " \t"); i >= 0 {
				add(strings.Trim(value[i+1:], " \t"))
			}
			if strings.Contains(strings.ToLower(name), "cookie") {
				for _, part := range strings.Split(value, ";") {
					if _, v, ok := strings.Cut(part, "="); ok {
						add(strings.TrimSpace(v))
					}
				}
			}
		}
	}
	if u, err := url.Parse(t.URL); err == nil {
		if u.User != nil {
			if p, ok := u.User.Password(); ok {
				add(p)
				add(base64.StdEncoding.EncodeToString([]byte(u.User.Username() + ":" + p)))
			}
		}
		for k, vs := range u.Query() {
			if domain.SensitiveKey(k) {
				for _, v := range vs {
					add(v)
				}
			}
		}
	}
	var collect func(any, bool)
	collect = func(v any, secret bool) {
		switch x := v.(type) {
		case map[string]any:
			for k, item := range x {
				collect(item, secret || domain.SensitiveKey(k))
			}
		case []any:
			for _, item := range x {
				collect(item, secret)
			}
		case string:
			if secret {
				add(x)
			}
		case json.Number:
			if secret {
				add(x.String())
			}
		}
	}
	var data any
	decoder := json.NewDecoder(bytes.NewReader(t.Data))
	decoder.UseNumber()
	if decoder.Decode(&data) == nil {
		collect(data, false)
		if text, ok := data.(string); ok {
			var nested any
			d := json.NewDecoder(strings.NewReader(text))
			d.UseNumber()
			if d.Decode(&nested) == nil {
				collect(nested, false)
			}
			if q, err := url.ParseQuery(text); err == nil {
				for k, vs := range q {
					if domain.SensitiveKey(k) {
						for _, v := range vs {
							add(v)
						}
					}
				}
			}
		}
	}
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
	unique := make([]string, 0, len(keys))
	seen := map[string]bool{}
	for _, k := range keys {
		if k != "" && !seen[k] {
			unique = append(unique, k)
			seen[k] = true
		}
	}
	return unique
}
