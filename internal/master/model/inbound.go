// Package model 定义入站的领域模型：表单结构、校验，以及由同一份定义产出的服务端配置与分享链接。
package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// 协议与组合常量。v1 当前实现 VLESS + TCP + Reality（可选 Vision）。
const (
	ProtoVLESS = "vless"

	NetworkTCP = "tcp"

	SecurityReality = "reality"

	FlowVision = "xtls-rprx-vision"
)

// Fingerprints 为 Reality 客户端可选的 uTLS 指纹。
var Fingerprints = []string{"chrome", "firefox", "safari", "ios", "android", "edge", "360", "qq", "random", "randomized"}

// Spec 为入站的协议参数，按 protocol / network / security 组合取用对应字段。
type Spec struct {
	Network  string   `json:"network"`
	Security string   `json:"security"`
	VLESS    *VLESS   `json:"vless,omitempty"`
	Reality  *Reality `json:"reality,omitempty"`
	Sniffing bool     `json:"sniffing"`
}

// VLESS 为 VLESS 协议参数。
type VLESS struct {
	Flow string `json:"flow"`
}

// Reality 为 Reality 安全层参数。PublicKey 由 PrivateKey 派生，仅用于生成链接。
type Reality struct {
	Target      string   `json:"target"`
	ServerNames []string `json:"serverNames"`
	PrivateKey  string   `json:"privateKey"`
	PublicKey   string   `json:"publicKey"`
	ShortIDs    []string `json:"shortIds"`
	Fingerprint string   `json:"fingerprint"`
	SpiderX     string   `json:"spiderX"`
	// MinClientVer 为允许连接的最低 Xray 客户端版本。留空时较新的 Xray（如 v26.7.28）
	// 默认要求 v26.3.27，部分第三方客户端可能因此被拒绝。
	MinClientVer string `json:"minClientVer,omitempty"`
}

var (
	shortIDRe   = regexp.MustCompile(`^([0-9a-f]{2}){0,8}$`)
	clientVerRe = regexp.MustCompile(`^\d{1,3}\.\d{1,3}\.\d{1,3}$`)
	hostnameRe  = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?$`)
)

// ParseSpec 解析并校验入站参数，返回规范化后的结构。
func ParseSpec(protocol string, raw json.RawMessage) (*Spec, error) {
	var s Spec
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, errors.New("入站参数格式错误")
	}
	if err := s.Validate(protocol); err != nil {
		return nil, err
	}
	return &s, nil
}

// Validate 校验协议组合与各字段，并就地规范化（去空白、派生公钥等）。
func (s *Spec) Validate(protocol string) error {
	if protocol != ProtoVLESS || s.Network != NetworkTCP || s.Security != SecurityReality {
		return errors.New("当前仅支持 VLESS + TCP + Reality")
	}
	if s.VLESS == nil {
		s.VLESS = &VLESS{}
	}
	if s.VLESS.Flow != "" && s.VLESS.Flow != FlowVision {
		return fmt.Errorf("不支持的 flow：%s", s.VLESS.Flow)
	}
	if s.Reality == nil {
		return errors.New("缺少 Reality 参数")
	}
	return s.Reality.validate()
}

func (r *Reality) validate() error {
	r.Target = strings.TrimSpace(r.Target)
	host, port, err := net.SplitHostPort(r.Target)
	if err != nil {
		return errors.New("目标网站格式应为 域名:端口，如 www.microsoft.com:443")
	}
	if p, err := strconv.Atoi(port); err != nil || p < 1 || p > 65535 {
		return errors.New("目标网站端口无效")
	}
	if net.ParseIP(host) == nil && !hostnameRe.MatchString(host) {
		return errors.New("目标网站域名无效")
	}
	r.ServerNames = cleanList(r.ServerNames)
	if len(r.ServerNames) == 0 {
		return errors.New("至少需要一个 SNI")
	}
	for _, n := range r.ServerNames {
		if !hostnameRe.MatchString(n) {
			return fmt.Errorf("SNI 无效：%s", n)
		}
	}
	pub, err := PublicKeyFromPrivate(strings.TrimSpace(r.PrivateKey))
	if err != nil {
		return errors.New("Reality 私钥无效，请重新生成")
	}
	r.PrivateKey, r.PublicKey = strings.TrimSpace(r.PrivateKey), pub
	r.ShortIDs = cleanList(r.ShortIDs)
	if len(r.ShortIDs) == 0 {
		return errors.New("至少需要一个 ShortID")
	}
	for i, id := range r.ShortIDs {
		r.ShortIDs[i] = strings.ToLower(id)
		if !shortIDRe.MatchString(r.ShortIDs[i]) {
			return fmt.Errorf("ShortID 需为不超过 16 位的偶数长度十六进制：%s", id)
		}
	}
	if r.Fingerprint == "" {
		r.Fingerprint = "chrome"
	}
	if !slices.Contains(Fingerprints, r.Fingerprint) {
		return fmt.Errorf("不支持的指纹：%s", r.Fingerprint)
	}
	if r.SpiderX == "" {
		r.SpiderX = "/"
	}
	if !strings.HasPrefix(r.SpiderX, "/") {
		return errors.New("SpiderX 需以 / 开头")
	}
	r.MinClientVer = strings.TrimPrefix(strings.TrimSpace(r.MinClientVer), "v")
	if r.MinClientVer != "" {
		if !clientVerRe.MatchString(r.MinClientVer) {
			return errors.New("最低客户端版本格式应为 26.3.27")
		}
		for _, part := range strings.Split(r.MinClientVer, ".") {
			if n, _ := strconv.Atoi(part); n > 255 {
				return errors.New("最低客户端版本每段需小于 256")
			}
		}
	}
	return nil
}

func cleanList(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" && !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}

// Client 为写入服务端配置的一个客户端。
type Client struct {
	Email string
	UUID  string
}

// ClientEmail 返回订阅在某入站上的统计标识，流量统计按此粒度上报。
func ClientEmail(subID, inboundID int64) string {
	return fmt.Sprintf("s%d.i%d", subID, inboundID)
}

// InboundTag 返回入站在 Xray 配置中的 tag。
func InboundTag(inboundID int64) string {
	return fmt.Sprintf("in-%d", inboundID)
}

// XrayInbound 生成服务端 inbound 配置。
func XrayInbound(id int64, listen string, port int, s *Spec, clients []Client) map[string]any {
	cs := make([]any, 0, len(clients))
	for _, c := range clients {
		cs = append(cs, map[string]any{"id": c.UUID, "email": c.Email, "flow": s.VLESS.Flow})
	}
	r := s.Reality
	reality := map[string]any{
		"target":      r.Target,
		"serverNames": r.ServerNames,
		"privateKey":  r.PrivateKey,
		"shortIds":    r.ShortIDs,
	}
	if r.MinClientVer != "" {
		reality["minClientVer"] = r.MinClientVer
	}
	in := map[string]any{
		"tag":      InboundTag(id),
		"port":     port,
		"protocol": ProtoVLESS,
		"settings": map[string]any{"clients": cs, "decryption": "none"},
		"streamSettings": map[string]any{
			"network":         NetworkTCP,
			"security":        SecurityReality,
			"realitySettings": reality,
		},
	}
	if listen != "" {
		in["listen"] = listen
	}
	if s.Sniffing {
		in["sniffing"] = map[string]any{"enabled": true, "destOverride": []string{"http", "tls", "quic"}, "routeOnly": true}
	}
	return in
}
