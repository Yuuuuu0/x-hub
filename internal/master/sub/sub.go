// Package sub 生成订阅内容：按订阅的入站范围收集自有节点，并生成通用 Base64 订阅；Mihomo 与 sing-box 由分流方案渲染。
package sub

import (
	"encoding/base64"
	"fmt"
	"regexp"
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
	// FormatShadowrocket 为 Shadowrocket 配置（分组与规则），只能显式指定：Shadowrocket 拉取订阅与配置时
	// User-Agent 相同，节点仍由通用订阅提供。
	FormatShadowrocket Format = "shadowrocket"
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
	case FormatShadowrocket:
		return FormatShadowrocket
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

// NameVars 为节点名称模板中与服务器相关的变量，值为空表示不适用（如未设置流量配额）。
type NameVars struct {
	Flag    string // 国家或地区旗帜
	Region  string // 地区名称
	Traffic string // 本周期剩余流量，如 1.14TB
	Days    string // 距 VPS 到期的天数，如 69D
}

// Target 计算入站对外的连接信息与按模板生成的名称。模板变量：{node} 服务器名称、{remark} 入站备注、
// {port} 端口、{flag} 旗帜、{region} 地区、{traffic} 剩余流量、{days} 剩余天数。
func Target(n *store.Node, in *store.Inbound, nameTemplate string, v NameVars) model.LinkTarget {
	t := model.LinkTarget{Host: in.LinkHost, Port: in.LinkPort}
	if t.Host == "" {
		t.Host = n.PublicHost
	}
	if t.Port == 0 {
		t.Port = in.Port
	}
	t.Name = RenderName(nameTemplate, map[string]string{
		"node": n.Name, "remark": in.Remark, "port": strconv.Itoa(in.Port),
		"flag": v.Flag, "region": v.Region, "traffic": v.Traffic, "days": v.Days,
	})
	if t.Name == "" {
		t.Name = n.Name
	}
	return t
}

var nameVar = regexp.MustCompile(`\{([a-z]+)\}`)

// RenderName 按模板生成节点名称。模板以 | 分段，某段中的变量全部为空时整段省略；
// 各段去掉多余空白与首尾的连接符后以 | 连接。未知变量原样保留。
func RenderName(tmpl string, vars map[string]string) string {
	var out []string
	for _, seg := range strings.Split(tmpl, "|") {
		hasVar, filled := false, false
		seg = nameVar.ReplaceAllStringFunc(seg, func(m string) string {
			v, ok := vars[m[1:len(m)-1]]
			if !ok {
				return m
			}
			hasVar = true
			if v != "" {
				filled = true
			}
			return v
		})
		if hasVar && !filled {
			continue
		}
		if seg = strings.Trim(strings.Join(strings.Fields(seg), " "), " -_·"); seg != "" {
			out = append(out, seg)
		}
	}
	return strings.Join(out, "|")
}

// FormatRemaining 把剩余流量格式化为紧凑写法：1.14TB / 512GB / 300MB。
func FormatRemaining(b int64) string {
	if b < 0 {
		b = 0
	}
	units := []struct {
		name string
		size float64
	}{{"TB", 1 << 40}, {"GB", 1 << 30}, {"MB", 1 << 20}}
	for _, u := range units {
		if float64(b) >= u.size || u.name == "MB" {
			s := strconv.FormatFloat(float64(b)/u.size, 'f', 2, 64)
			s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
			return s + u.name
		}
	}
	return ""
}

// Collect 收集订阅可用的节点：入站已启用、在订阅范围内（未被排除或被授权）、服务器已设置公网地址。名称重复时追加序号。
// vars 返回各服务器的名称变量。
func Collect(st *store.Store, s *store.Subscription, nameTemplate string, vars func(*store.Node) NameVars) ([]Node, error) {
	nodes, err := st.ListNodes()
	if err != nil {
		return nil, err
	}
	byID := map[int64]*store.Node{}
	for _, n := range nodes {
		byID[n.ID] = n
	}
	// 关联的入站：仅限模式下为授权的入站，全部入站模式下为排除的入站。
	listed := map[int64]bool{}
	ids, err := st.SubscriptionInboundIDs()
	if err != nil {
		return nil, err
	}
	for _, id := range ids[s.ID] {
		listed[id] = true
	}
	inbounds, err := st.ListInbounds()
	if err != nil {
		return nil, err
	}
	var out []Node
	seen := map[string]int{}
	// 按入站顺序输出，与面板展示一致。
	for _, in := range inbounds {
		if !in.Enabled || s.AllInbounds == listed[in.ID] {
			continue
		}
		spec, err := model.ParseSpec(in.Protocol, in.Spec)
		if err != nil {
			continue
		}
		n := byID[in.NodeID]
		t := Target(n, in, nameTemplate, vars(n))
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
	return out, nil
}

// Base64 生成通用订阅内容：分享链接每行一条，整体 Base64 编码。
func Base64(links []string) []byte {
	return []byte(base64.StdEncoding.EncodeToString([]byte(strings.Join(links, "\n"))))
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
