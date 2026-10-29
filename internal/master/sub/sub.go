// Package sub 生成订阅内容：按订阅的入站范围收集自有节点，并渲染为通用 Base64 或 sing-box 配置。
package sub

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/Yuuuuu0/x-hub/internal/master/model"
	"github.com/Yuuuuu0/x-hub/internal/master/store"
)

// Format 为订阅输出格式。
type Format string

const (
	FormatBase64  Format = "base64"  // v2rayN / v2rayNG / Shadowrocket / NekoBox 等
	FormatMihomo  Format = "mihomo"  // Mihomo（Clash Meta）/ Clash Verge / Stash 等
	FormatSingBox Format = "singbox" // sing-box 官方客户端（SFA / SFI / SFM）
)

// 识别客户端的 User-Agent 关键字（小写）。sing-box 优先，避免被泛化的关键字误判。
var (
	singboxUAKeywords = []string{"sing-box", "sfa/", "sfi/", "sfm/", "sft/"}
	mihomoUAKeywords  = []string{"mihomo", "clash", "stash", "meta"}
)

// DetectFormat 决定输出格式：显式参数优先，否则按 User-Agent 识别，默认 Base64。
func DetectFormat(userAgent, override string) Format {
	switch Format(strings.ToLower(override)) {
	case FormatBase64:
		return FormatBase64
	case FormatMihomo, "clash":
		return FormatMihomo
	case FormatSingBox, "sing-box":
		return FormatSingBox
	}
	ua := strings.ToLower(userAgent)
	for _, k := range singboxUAKeywords {
		if strings.Contains(ua, k) {
			return FormatSingBox
		}
	}
	for _, k := range mihomoUAKeywords {
		if strings.Contains(ua, k) {
			return FormatMihomo
		}
	}
	return FormatBase64
}

// Node 为订阅中的一个节点。SingBox 为空表示 sing-box 不支持该协议组合。
type Node struct {
	Name      string
	InboundID int64
	Link      string
	Mihomo    model.MihomoProxy
	SingBox   map[string]any
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
			cred := model.CredentialFor(in.Protocol, spec, s.UUID, s.Secret, in.ID)
			sb, _ := model.SingBox(in.Protocol, spec, t, cred)
			out = append(out, Node{
				Name: t.Name, InboundID: in.ID, Link: model.ShareLink(in.Protocol, spec, t, cred),
				Mihomo: model.Mihomo(in.Protocol, spec, t, cred), SingBox: sb,
			})
		}
	}
	return out, nil
}

// Render 按格式渲染 Base64 或 sing-box 订阅内容，返回内容与 Content-Type。Mihomo 由分流方案渲染。
func Render(f Format, nodes []Node) ([]byte, string, error) {
	if f == FormatSingBox {
		b, err := renderSingBox(nodes)
		return b, "application/json; charset=utf-8", err
	}
	links := make([]string, 0, len(nodes))
	for _, n := range nodes {
		if n.Link != "" {
			links = append(links, n.Link)
		}
	}
	return []byte(base64.StdEncoding.EncodeToString([]byte(strings.Join(links, "\n")))), "text/plain; charset=utf-8", nil
}

const (
	groupSelect = "节点选择"
	groupAuto   = "自动选择"
)

// renderSingBox 生成 sing-box 配置（1.12+ 格式）：TUN 与本地 mixed 入站、手动 / 自动选择、
// 局域网与国内直连。sing-box 不支持的节点（XHTTP）不输出。
func renderSingBox(nodes []Node) ([]byte, error) {
	names := []string{}
	var outbounds []any
	for _, n := range nodes {
		if n.SingBox != nil {
			outbounds = append(outbounds, n.SingBox)
			names = append(names, n.Name)
		}
	}
	selectList := []string{}
	if len(names) > 0 {
		selectList = append(selectList, groupAuto)
	}
	selectList = append(append(selectList, names...), "direct")
	groups := []any{map[string]any{"type": "selector", "tag": groupSelect, "outbounds": selectList}}
	if len(names) > 0 {
		groups = append(groups, map[string]any{
			"type": "urltest", "tag": groupAuto, "outbounds": names,
			"url": "https://www.gstatic.com/generate_204", "interval": "5m", "tolerance": 50,
		})
	}
	outbounds = append(append(groups, outbounds...), map[string]any{"type": "direct", "tag": "direct"})
	// 不设置 download_detour：它在 1.14 被弃用，而替代的 http_client 在 1.12 / 1.13 上无法解析；
	// 留空时规则集经默认出站（route.final，即节点选择）下载，各版本均可用。
	ruleSet := func(tag, repo string) map[string]any {
		return map[string]any{
			"type": "remote", "tag": tag, "format": "binary",
			"url": "https://raw.githubusercontent.com/SagerNet/" + repo + "/rule-set/" + tag + ".srs",
		}
	}
	cfg := map[string]any{
		"log": map[string]any{"level": "warn", "timestamp": true},
		"dns": map[string]any{
			"servers": []any{
				map[string]any{"type": "https", "tag": "remote", "server": "1.1.1.1", "detour": groupSelect},
				map[string]any{"type": "udp", "tag": "local", "server": "223.5.5.5"},
			},
			"rules": []any{map[string]any{"rule_set": "geosite-cn", "server": "local"}},
			"final": "remote",
		},
		"inbounds": []any{
			map[string]any{"type": "tun", "tag": "tun-in", "address": []string{"172.19.0.1/30", "fdfe:dcba:9876::1/126"}, "auto_route": true, "strict_route": true},
			map[string]any{"type": "mixed", "tag": "mixed-in", "listen": "127.0.0.1", "listen_port": 2080},
		},
		"outbounds": outbounds,
		"route": map[string]any{
			"rules": []any{
				map[string]any{"action": "sniff"},
				map[string]any{"protocol": "dns", "action": "hijack-dns"},
				map[string]any{"ip_is_private": true, "outbound": "direct"},
				map[string]any{"rule_set": []string{"geosite-cn", "geoip-cn"}, "outbound": "direct"},
			},
			"rule_set":                []any{ruleSet("geosite-cn", "sing-geosite"), ruleSet("geoip-cn", "sing-geoip")},
			"final":                   groupSelect,
			"auto_detect_interface":   true,
			"default_domain_resolver": "local",
		},
	}
	return json.MarshalIndent(cfg, "", "  ")
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
