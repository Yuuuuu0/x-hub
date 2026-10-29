// Package sub 生成订阅内容：按订阅的入站范围收集自有节点，并生成通用 Base64 订阅；Mihomo 与 sing-box 由分流方案渲染。
package sub

import (
	"encoding/base64"
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
