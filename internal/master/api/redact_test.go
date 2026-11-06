package api

import "testing"

// TestRedactURLs 覆盖日志脱敏：保留协议与主机，去掉用户信息、路径与查询参数。
func TestRedactURLs(t *testing.T) {
	cases := map[string]string{
		`拉取失败：Get "https://sub.example.com/api/v1/client/subscribe?token=abc123": dial tcp: timeout`: `拉取失败：Get "https://sub.example.com": dial tcp: timeout`,
		`http://user:pw@1.2.3.4:8080/s/secret-token`:                                                 `http://1.2.3.4:8080`,
		`HTTPS://Example.com/x?k=v and https://b.example.org/y`:                                      `https://Example.com and https://b.example.org`,
		`拉取失败：HTTP 403`: `拉取失败：HTTP 403`,
	}
	for in, want := range cases {
		if got := redactURLs(in); got != want {
			t.Errorf("redactURLs(%q) = %q, want %q", in, got, want)
		}
	}
}
