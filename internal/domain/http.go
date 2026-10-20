package domain

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

func ValidateHTTP(t *Task) error {
	t.URL = strings.TrimSpace(t.URL)
	u, err := url.Parse(t.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || len(t.URL) > 8192 {
		return errors.New("请输入有效的 HTTP 或 HTTPS URL，最长 8192 字节")
	}
	t.Method = strings.ToUpper(strings.TrimSpace(t.Method))
	if t.Method == "" {
		t.Method = "GET"
	}
	switch t.Method {
	case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS":
	default:
		return errors.New("不支持的 HTTP 请求方法")
	}
	headers := make(map[string]string, len(t.Headers))
	size := 0
	for name, value := range t.Headers {
		if name == "" {
			return errors.New("请求头名称不能为空")
		}
		for _, c := range name {
			if c <= 32 || c >= 127 || strings.ContainsRune("()<>@,;:\\\"/[]?={}", c) {
				return errors.New("请求头名称无效")
			}
		}
		for _, c := range value {
			if c == '\r' || c == '\n' || c == 127 || (c < 32 && c != '\t') {
				return errors.New("请求头值不能包含换行或控制字符")
			}
		}
		key := http.CanonicalHeaderKey(name)
		if _, exists := headers[key]; exists {
			return errors.New("请求头名称重复（不区分大小写）")
		}
		if key == "Content-Length" || key == "Transfer-Encoding" {
			return errors.New("Content-Length 和 Transfer-Encoding 由 HTTP 客户端自动管理")
		}
		size += len(name) + len(value)
		headers[key] = value
	}
	if len(headers) > 100 || size > 32768 {
		return errors.New("请求头最多 100 项、总长度不超过 32 KiB")
	}
	t.Headers = headers
	if len(t.Data) > 65536 {
		return errors.New("请求体不能超过 64 KiB")
	}
	_, _, err = RequestBody(*t)
	return err
}
func RequestBody(t Task) ([]byte, bool, error) {
	data := bytes.TrimSpace(t.Data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		return nil, false, nil
	}
	if !json.Valid(data) {
		return nil, false, errors.New("data 必须是有效 JSON 对象、数组或字符串")
	}
	if data[0] == '"' {
		var text string
		if err := json.Unmarshal(data, &text); err != nil {
			return nil, false, err
		}
		return []byte(text), false, nil
	}
	if data[0] != '{' && data[0] != '[' {
		return nil, false, errors.New("data 必须是 JSON 对象、数组或字符串")
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, data); err != nil {
		return nil, false, err
	}
	return compact.Bytes(), true, nil
}
func SensitiveKey(name string) bool {
	n := strings.ToLower(name)
	return strings.Contains(n, "authorization") || n == "auth" || strings.Contains(n, "cookie") || strings.Contains(n, "token") || strings.Contains(n, "password") || strings.Contains(n, "secret") || strings.Contains(n, "credential") || strings.Contains(n, "api-key") || strings.Contains(n, "api_key") || strings.Contains(n, "apikey") || strings.HasSuffix(n, "_key") || strings.HasSuffix(n, "-key") || n == "key"
}
func SafeURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	u.User = nil
	u.Fragment = ""
	q := u.Query()
	for key := range q {
		if SensitiveKey(key) {
			q.Set(key, "[REDACTED]")
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}
func (t Task) Redacted() Task {
	t.CurlCommand = ""
	t.Headers = nil
	t.Data = nil
	t.URL = SafeURL(t.URL)
	return t
}
