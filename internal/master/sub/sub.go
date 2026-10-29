// Package sub 生成订阅内容：按订阅的入站范围收集节点，并渲染为通用 Base64 或 Mihomo 配置。
package sub

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/Yuuuuu0/x-hub/internal/master/model"
	"github.com/Yuuuuu0/x-hub/internal/master/store"
)

// Format 为订阅输出格式。
type Format string

const (
	FormatBase64 Format = "base64" // v2rayN / v2rayNG / Shadowrocket / NekoBox 等
	FormatMihomo Format = "mihomo" // Mihomo（Clash Meta）/ Clash Verge / Stash 等
)

// mihomoUAKeywords 为识别 Mihomo 系客户端的 User-Agent 关键字（小写）。
var mihomoUAKeywords = []string{"mihomo", "clash", "stash", "meta"}

// DetectFormat 决定输出格式：显式参数优先，否则按 User-Agent 识别，默认 Base64。
func DetectFormat(userAgent, override string) Format {
	switch Format(strings.ToLower(override)) {
	case FormatBase64:
		return FormatBase64
	case FormatMihomo, "clash":
		return FormatMihomo
	}
	ua := strings.ToLower(userAgent)
	for _, k := range mihomoUAKeywords {
		if strings.Contains(ua, k) {
			return FormatMihomo
		}
	}
	return FormatBase64
}

// Node 为订阅中的一个节点。
type Node struct {
	Name   string
	Link   string
	Mihomo model.MihomoProxy
}

// Target 计算入站对外的连接信息与按模板生成的名称。
func Target(n *store.Node, in *store.Inbound, nameTemplate string) model.LinkTarget {
	t := model.LinkTarget{Host: in.LinkHost, Port: in.LinkPort}
	if t.Host == "" {
		t.Host = n.PublicHost
	}
	if t.Port == 0 {
		t.Port = in.Port
	}
	name := strings.NewReplacer("{node}", n.Name, "{remark}", in.Remark, "{port}", strconv.Itoa(in.Port)).Replace(nameTemplate)
	t.Name = strings.Join(strings.Fields(name), " ")
	if t.Name == "" {
		t.Name = n.Name
	}
	return t
}

// Collect 收集订阅可用的节点：入站已启用、在订阅范围内、服务器已设置公网地址。名称重复时追加序号。
func Collect(st *store.Store, s *store.Subscription, nameTemplate string) ([]Node, error) {
	nodes, err := st.ListNodes()
	if err != nil {
		return nil, err
	}
	byID := map[int64]*store.Node{}
	for _, n := range nodes {
		byID[n.ID] = n
	}
	allowed := map[int64]bool{}
	if !s.AllInbounds {
		ids, err := st.SubscriptionInboundIDs()
		if err != nil {
			return nil, err
		}
		for _, id := range ids[s.ID] {
			allowed[id] = true
		}
	}
	inbounds, err := st.ListInbounds()
	if err != nil {
		return nil, err
	}
	var out []Node
	seen := map[string]int{}
	// 按服务器排序输出，与面板展示一致。
	for _, n := range nodes {
		for _, in := range inbounds {
			if in.NodeID != n.ID || !in.Enabled || (!s.AllInbounds && !allowed[in.ID]) {
				continue
			}
			spec, err := model.ParseSpec(in.Protocol, in.Spec)
			if err != nil {
				continue
			}
			t := Target(byID[in.NodeID], in, nameTemplate)
			if t.Host == "" {
				continue
			}
			if seen[t.Name]++; seen[t.Name] > 1 {
				t.Name = fmt.Sprintf("%s %d", t.Name, seen[t.Name])
			}
			out = append(out, Node{Name: t.Name, Link: model.ShareLink(spec, t, s.UUID), Mihomo: model.Mihomo(spec, t, s.UUID)})
		}
	}
	return out, nil
}

// Render 按格式渲染订阅内容，返回内容与 Content-Type。
func Render(f Format, nodes []Node) ([]byte, string, error) {
	if f == FormatMihomo {
		b, err := renderMihomo(nodes)
		return b, "text/yaml; charset=utf-8", err
	}
	links := make([]string, len(nodes))
	for i, n := range nodes {
		links[i] = n.Link
	}
	return []byte(base64.StdEncoding.EncodeToString([]byte(strings.Join(links, "\n")))), "text/plain; charset=utf-8", nil
}

type mihomoGroup struct {
	Name      string   `yaml:"name"`
	Type      string   `yaml:"type"`
	Proxies   []string `yaml:"proxies"`
	URL       string   `yaml:"url,omitempty"`
	Interval  int      `yaml:"interval,omitempty"`
	Tolerance int      `yaml:"tolerance,omitempty"`
}

type mihomoConfig struct {
	MixedPort   int                 `yaml:"mixed-port"`
	AllowLan    bool                `yaml:"allow-lan"`
	Mode        string              `yaml:"mode"`
	LogLevel    string              `yaml:"log-level"`
	Proxies     []model.MihomoProxy `yaml:"proxies"`
	ProxyGroups []mihomoGroup       `yaml:"proxy-groups"`
	Rules       []string            `yaml:"rules"`
}

const (
	groupSelect = "节点选择"
	groupAuto   = "自动选择"
)

// renderMihomo 生成精简的 Mihomo 配置：手动选择 + 延迟自动选择，局域网与国内直连。
func renderMihomo(nodes []Node) ([]byte, error) {
	cfg := mihomoConfig{MixedPort: 7890, Mode: "rule", LogLevel: "info", Proxies: []model.MihomoProxy{}}
	names := make([]string, 0, len(nodes))
	for _, n := range nodes {
		cfg.Proxies = append(cfg.Proxies, n.Mihomo)
		names = append(names, n.Name)
	}
	selectProxies := []string{}
	if len(names) > 0 {
		selectProxies = append(selectProxies, groupAuto)
	}
	selectProxies = append(append(selectProxies, names...), "DIRECT")
	cfg.ProxyGroups = []mihomoGroup{{Name: groupSelect, Type: "select", Proxies: selectProxies}}
	if len(names) > 0 {
		cfg.ProxyGroups = append(cfg.ProxyGroups, mihomoGroup{
			Name: groupAuto, Type: "url-test", Proxies: names,
			URL: "https://www.gstatic.com/generate_204", Interval: 300, Tolerance: 50,
		})
	}
	cfg.Rules = []string{
		"GEOIP,LAN,DIRECT,no-resolve",
		"GEOSITE,CN,DIRECT",
		"GEOIP,CN,DIRECT",
		"MATCH," + groupSelect,
	}
	return yaml.Marshal(cfg)
}

// UserInfo 生成 subscription-userinfo 响应头，客户端据此展示用量、总量与到期时间。
func UserInfo(s *store.Subscription) string {
	parts := []string{"upload=" + strconv.FormatInt(s.UsedUp, 10), "download=" + strconv.FormatInt(s.UsedDown, 10)}
	if s.LimitBytes > 0 {
		parts = append(parts, "total="+strconv.FormatInt(s.LimitBytes, 10))
	}
	if s.ExpireAt > 0 {
		parts = append(parts, "expire="+strconv.FormatInt(s.ExpireAt, 10))
	}
	return strings.Join(parts, "; ")
}
