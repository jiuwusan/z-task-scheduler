package executor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/jiuwusan/z-task-scheduler/internal/domain"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"
)

type HTTP struct{ client *http.Client }

func NewHTTP() *HTTP {
	return &HTTP{client: &http.Client{Transport: http.DefaultTransport.(*http.Transport).Clone(), CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (h *HTTP) Execute(ctx context.Context, t domain.Task) domain.RunResult {
	result := domain.RunResult{State: "failed"}
	if err := domain.ValidateTask(&t); err != nil {
		result.Error = err.Error()
		return result
	}
	body, isJSON, err := domain.RequestBody(t)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	req, err := http.NewRequestWithContext(ctx, t.Method, t.URL, bytes.NewReader(body))
	if err != nil {
		result.Error = "无法创建 HTTP 请求，请检查 URL"
		return result
	}
	for name, value := range t.Headers {
		if name == "Host" {
			req.Host = value
		} else {
			req.Header.Set(name, value)
		}
	}
	if isJSON && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := h.client.Do(req)
	if err != nil {
		result.Error = "HTTP 请求失败，请检查地址、网络和 TLS 证书"
		setHTTPContextError(&result, ctx)
		return result
	}
	defer resp.Body.Close()
	result.HTTPStatus = resp.StatusCode
	output := newOutput(requestSecrets(t))
	_, err = io.Copy(output, resp.Body)
	output.drain(true)
	text := strings.ToValidUTF8(string(output.data), "�")
	result.Truncated = output.total > outputLimit || len(text) > outputLimit
	if len(text) > outputLimit {
		text = text[:outputLimit]
		for !utf8.ValidString(text) {
			text = text[:len(text)-1]
		}
	}
	result.Output = text
	if err != nil {
		result.Error = "读取 HTTP 响应失败"
		setHTTPContextError(&result, ctx)
		return result
	}
	if resp.StatusCode >= 400 {
		result.Error = fmt.Sprintf("HTTP %d", resp.StatusCode)
		return result
	}
	result.State = "success"
	return result
}
func setHTTPContextError(r *domain.RunResult, ctx context.Context) {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		r.State = "timeout"
		r.Error = "执行超过超时限制"
	} else if ctx.Err() != nil {
		r.State = "interrupted"
		r.Error = "执行已取消"
	}
}
