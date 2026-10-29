package metrics

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"strings"
	"time"
)

// countrySources 为查询本机出口 IP 所在国家的接口：Cloudflare trace 返回 loc=XX，ipinfo 直接返回代码。
var countrySources = []struct {
	url   string
	parse func(body string) string
}{
	{"https://www.cloudflare.com/cdn-cgi/trace", func(body string) string {
		s := bufio.NewScanner(strings.NewReader(body))
		for s.Scan() {
			if v, ok := strings.CutPrefix(s.Text(), "loc="); ok {
				return v
			}
		}
		return ""
	}},
	{"https://ipinfo.io/country", strings.TrimSpace},
}

// DetectCountry 返回本机出口 IP 所在国家或地区的 ISO 3166-1 两位代码（大写），失败时返回空。
func DetectCountry(ctx context.Context) string {
	client := &http.Client{Timeout: 4 * time.Second}
	for _, src := range countrySources {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, src.url, nil)
		if err != nil {
			continue
		}
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		if code := strings.ToUpper(src.parse(string(body))); validCountry(code) {
			return code
		}
	}
	return ""
}

// validCountry 排除非国家代码（XX 未知、T1 Tor 等）。
func validCountry(code string) bool {
	if len(code) != 2 || code == "XX" || code == "T1" {
		return false
	}
	return code[0] >= 'A' && code[0] <= 'Z' && code[1] >= 'A' && code[1] <= 'Z'
}
