package executor

import (
	"errors"
	"strings"
)

type curlOption struct{ name, value string }

var shortValue = map[byte]string{'A': "user-agent", 'b': "cookie", 'c': "cookie-jar", 'C': "continue-at", 'd': "data", 'D': "dump-header", 'e': "referer", 'E': "cert", 'F': "form", 'H': "header", 'm': "max-time", 'o': "output", 'P': "ftp-port", 'Q': "quote", 'r': "range", 't': "telnet-option", 'T': "upload-file", 'u': "user", 'U': "proxy-user", 'x': "proxy", 'X': "request", 'y': "speed-time", 'Y': "speed-limit", 'z': "time-cond"}

const longValues = " url request request-target header proxy-header data data-ascii data-raw data-binary data-urlencode url-query json form form-string user proxy-user oauth2-bearer cookie cookie-jar output output-dir upload-file connect-timeout max-time retry retry-delay retry-max-time resolve connect-to cacert capath cert cert-type key key-type pass proxy noproxy proxy-cacert proxy-capath proxy-cert proxy-cert-type proxy-key proxy-key-type proxy-pass unix-socket abstract-unix-socket trace trace-ascii trace-config user-agent referer range continue-at limit-rate max-filesize local-port interface dns-interface dns-ipv4-addr dns-ipv6-addr dns-servers doh-url dump-header time-cond speed-time speed-limit ftp-port quote telnet-option ftp-method ftp-account ftp-alternative-to-user hostpubmd5 hostpubsha256 knownhosts netrc-file login-options mail-from mail-rcpt service-name proxy-service-name socks5-gssapi-service ciphers tls13-ciphers tlsauthtype tlspassword tlsuser tls-max proxy-ciphers proxy-tls13-ciphers proxy-tlspassword proxy-tlsuser aws-sigv4 alt-svc hsts etag-save etag-compare proto proto-default proto-redir rate variable expand-header expand-data expand-url expand-variable "

// Interpret arity: literal request bodies must not be interpreted as flags.
func curlOptions(args []string) ([]curlOption, error) {
	out := []curlOption{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			return nil, errors.New("不支持 -- 选项终止符")
		}
		if strings.HasPrefix(a, "--") {
			name, value, has := strings.Cut(a[2:], "=")
			if name == "next" || name == "config" || name == "write-out" || name == "stderr" {
				return nil, errors.New("不支持 --next、-:、--config、-K、--write-out、-w 和 --stderr")
			}
			if !has && strings.Contains(longValues, " "+name+" ") {
				i++
				if i >= len(args) {
					return nil, errors.New("curl 选项缺少参数")
				}
				value = args[i]
			}
			out = append(out, curlOption{name, value})
			continue
		}
		if strings.HasPrefix(a, "-") && a != "-" {
			for n := 1; n < len(a); n++ {
				flag := a[n]
				if flag == 'w' || flag == 'K' || flag == ':' {
					return nil, errors.New("不支持 -w、-K 和 -: 选项")
				}
				if name, ok := shortValue[flag]; ok {
					value := a[n+1:]
					if value == "" {
						i++
						if i >= len(args) {
							return nil, errors.New("curl 选项缺少参数")
						}
						value = args[i]
					}
					out = append(out, curlOption{name, value})
					break
				}
				name := string(flag)
				if flag == 'f' {
					name = "fail"
				}
				out = append(out, curlOption{name, ""})
			}
			continue
		}
		out = append(out, curlOption{"url", a})
	}
	return out, nil
}

// curl 7.88 rejects --fail with --fail-with-body; remove only the fail flag.
func normalizeFailure(args []string) []string {
	out := []string{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--fail" || a == "--no-fail" {
			continue
		}
		if strings.HasPrefix(a, "--") {
			out = append(out, a)
			name, _, has := strings.Cut(a[2:], "=")
			if !has && strings.Contains(longValues, " "+name+" ") && i+1 < len(args) {
				i++
				out = append(out, args[i])
			}
			continue
		}
		if strings.HasPrefix(a, "-") && a != "-" {
			clean := "-"
			for n := 1; n < len(a); n++ {
				flag := a[n]
				if _, ok := shortValue[flag]; ok {
					clean += a[n:]
					if n == len(a)-1 && i+1 < len(args) {
						out = append(out, clean)
						i++
						out = append(out, args[i])
						clean = "-"
					}
					break
				}
				if flag != 'f' {
					clean += string(flag)
				}
			}
			if clean != "-" {
				out = append(out, clean)
			}
			continue
		}
		out = append(out, a)
	}
	return out
}
