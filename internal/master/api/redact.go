package api

import (
	"net/url"
	"regexp"
	"strings"
)

// urlInTextRe 匹配文本中的 http(s) 地址，用于日志脱敏。
var urlInTextRe = regexp.MustCompile(`(?i)https?://[^\s"'<>]+`)

// redactURLs 把文本中的 http(s) 地址替换为只含协议与主机的形式，去掉用户信息、路径与查询参数，
// 避免机场订阅等地址中的令牌写入日志或返回给外部。
func redactURLs(s string) string {
	return urlInTextRe.ReplaceAllStringFunc(s, func(raw string) string {
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" {
			return strings.ToLower(raw[:strings.Index(raw, "://")]) + "://***"
		}
		return u.Scheme + "://" + u.Host
	})
}
