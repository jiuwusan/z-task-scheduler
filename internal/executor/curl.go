package executor

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"github.com/jiuwusan/z-task-scheduler/internal/domain"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

type Curl struct{ binary string }

func NewCurl(binary string) *Curl {
	if binary == "" {
		binary = "curl"
		if runtime.GOOS == "windows" {
			binary = "curl.exe"
		}
	}
	return &Curl{binary: binary}
}
func (c *Curl) Execute(ctx context.Context, t domain.Task) domain.RunResult {
	result := domain.RunResult{State: "failed"}
	args, err := ParseCurl(t.CurlCommand)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	keys := secrets(args)
	extra := 0
	for _, s := range keys {
		extra = max(extra, len(s))
	}
	extra = min(extra, 65536)
	out := &boundedOutput{limit: outputLimit + extra}
	stderr := &boundedOutput{limit: outputLimit + extra}
	marker := "__zTS_" + rand.Text() + ":"
	managed := []string{"-q", "--silent", "--show-error"}
	managed = append(managed, args...)
	managed = append(managed, "--fail-with-body", "--fail-early", "--write-out", "%{stderr}\n"+marker+"%{http_code}\n")
	cmd := exec.CommandContext(ctx, c.binary, managed...)
	cmd.Stdout = out
	cmd.Stderr = stderr
	err = cmd.Run()
	codeText := ""
	if p := strings.LastIndex(string(stderr.tail), marker); p >= 0 {
		codeText = strings.Fields(string(stderr.tail)[p+len(marker):])[0]
	}
	result.HTTPStatus, _ = strconv.Atoi(codeText)
	text := string(out.data)
	errText := string(stderr.data)
	if p := strings.Index(errText, marker); p >= 0 {
		errText = errText[:p]
	}
	if strings.TrimSpace(errText) != "" {
		text += "\n" + errText
	}
	text = redact(text, keys)
	result.Truncated = out.total+stderr.total > outputLimit || len(text) > outputLimit
	if len(text) > outputLimit {
		text = text[:outputLimit]
	}
	result.Output = strings.ToValidUTF8(text, "�")
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			result.ExitCode = exit.ExitCode()
		} else {
			result.ExitCode = -1
		}
		result.Error = "curl 执行失败"
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			result.State = "timeout"
			result.Error = "执行超过超时限制"
		} else if ctx.Err() != nil {
			result.State = "interrupted"
			result.Error = "执行已取消"
		}
		return result
	}
	if result.HTTPStatus >= 400 {
		result.Error = fmt.Sprintf("HTTP %d", result.HTTPStatus)
		return result
	}
	if codeText == "" {
		result.Error = "未能读取 curl 执行元数据"
		return result
	}
	result.State = "success"
	return result
}
