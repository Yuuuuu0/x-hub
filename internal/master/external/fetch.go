package external

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// DefaultUserAgent 为拉取外部订阅时默认的 User-Agent，多数机场据此返回完整的 Mihomo 配置。
const DefaultUserAgent = "clash.meta"

const maxBody = 10 << 20

var client = &http.Client{Timeout: 30 * time.Second}

// FetchResult 为一次订阅拉取的结果。UserInfo 为 subscription-userinfo 响应头原文。
type FetchResult struct {
	Proxies  []Proxy
	Skipped  []string
	UserInfo string
}

// Fetch 拉取并解析外部订阅。
func Fetch(ctx context.Context, rawURL, userAgent string) (*FetchResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("订阅地址无效：%w", err)
	}
	if userAgent == "" {
		userAgent = DefaultUserAgent
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("拉取失败：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("拉取失败：HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, fmt.Errorf("读取失败：%w", err)
	}
	if len(body) > maxBody {
		return nil, fmt.Errorf("订阅内容超过 %d MB", maxBody>>20)
	}
	proxies, skipped, err := Parse(string(body))
	if err != nil {
		return nil, err
	}
	if len(proxies) == 0 {
		return nil, fmt.Errorf("订阅中没有可用的节点")
	}
	return &FetchResult{Proxies: proxies, Skipped: skipped, UserInfo: resp.Header.Get("Subscription-Userinfo")}, nil
}
