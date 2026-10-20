package executor

import (
	"errors"
	"strings"
	"unicode"
)

func ParseCurl(command string) ([]string, error) {
	var args []string
	var b strings.Builder
	var quote rune
	active := false
	r := []rune(strings.ReplaceAll(command, "\r\n", "\n"))
	flush := func() {
		if active {
			args = append(args, b.String())
			b.Reset()
			active = false
		}
	}
	for i := 0; i < len(r); i++ {
		c := r[i]
		if quote == '\'' {
			if c == '\'' {
				quote = 0
			} else {
				b.WriteRune(c)
			}
			continue
		}
		if c == '\\' {
			if i+1 >= len(r) {
				return nil, errors.New("命令末尾有未完成的转义")
			}
			n := r[i+1]
			if n == '\n' {
				i++
				continue
			}
			if quote == '"' && !strings.ContainsRune("$`\"\\", n) {
				b.WriteRune(c)
				continue
			}
			b.WriteRune(n)
			active = true
			i++
			continue
		}
		if quote == '"' {
			if c == '"' {
				quote = 0
			} else {
				b.WriteRune(c)
			}
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
			active = true
			continue
		}
		if unicode.IsSpace(c) {
			flush()
			continue
		}
		if strings.ContainsRune("|&;<>`", c) {
			return nil, errors.New("不支持 Shell 管道、连接或重定向，请每个任务填写一条 curl 命令")
		}
		b.WriteRune(c)
		active = true
	}
	if quote != 0 {
		return nil, errors.New("命令引号未闭合")
	}
	flush()
	if len(args) < 2 || args[0] != "curl" {
		return nil, errors.New("命令必须以 curl 开头并包含请求地址")
	}
	for _, a := range args[1:] {
		if a == "--" || a == "--next" || strings.HasPrefix(a, "--config") || strings.HasPrefix(a, "--write-out") {
			return nil, errors.New("不支持 --、--next、--config 和自定义 --write-out")
		}
		if strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") {
			for _, c := range a[1:] {
				if c == 'w' || c == 'K' {
					return nil, errors.New("不支持 -w 和 -K")
				}
				if strings.ContainsRune("AbcCdeEFHmoPuUQrtTxXyz", c) {
					break
				}
			}
		}
	}
	return args[1:], nil
}
