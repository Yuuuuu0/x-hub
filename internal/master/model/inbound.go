// Package model 定义入站的领域模型：表单结构、校验，以及由同一份定义产出的服务端配置、
// 分享链接、Mihomo 与 sing-box 节点。
package model

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// 协议。
const (
	ProtoVLESS  = "vless"
	ProtoVMess  = "vmess"
	ProtoTrojan = "trojan"
	ProtoSS     = "shadowsocks"
)

// 传输层。
const (
	NetworkTCP   = "tcp"
	NetworkWS    = "ws"
	NetworkXHTTP = "xhttp"
)

// 安全层。
const (
	SecurityReality = "reality"
	SecurityTLS     = "tls"
	SecurityNone    = "none"
)

// FlowVision 为 XTLS Vision 流控。
const FlowVision = "xtls-rprx-vision"

// 证书来源。
const (
	CertFile = "file" // 节点上已有的证书文件
	CertACME = "acme" // 被控端通过 ACME（Cloudflare DNS）自动申请
)

// SS2022Methods 为支持多用户的 Shadowsocks 2022 加密方式及其密钥长度（字节）。
// Xray 的多用户 SS2022 仅支持 blake3-aes-*-gcm。
var SS2022Methods = map[string]int{
	"2022-blake3-aes-128-gcm": 16,
	"2022-blake3-aes-256-gcm": 32,
}

// XHTTPModes 为 XHTTP 可选模式。
var XHTTPModes = []string{"auto", "packet-up", "stream-up", "stream-one"}

// Fingerprints 为客户端可选的 uTLS 指纹。
var Fingerprints = []string{"chrome", "firefox", "safari", "ios", "android", "edge", "360", "qq", "random", "randomized"}

// combos 为支持的协议组合：protocol / network / security。
var combos = map[[3]string]bool{
	{ProtoVLESS, NetworkTCP, SecurityReality}:   true,
	{ProtoVLESS, NetworkXHTTP, SecurityReality}: true,
	{ProtoVLESS, NetworkWS, SecurityTLS}:        true,
	{ProtoVMess, NetworkWS, SecurityTLS}:        true,
	{ProtoTrojan, NetworkTCP, SecurityTLS}:      true,
	{ProtoSS, NetworkTCP, SecurityNone}:         true,
}

// Spec 为入站的协议参数，按 protocol / network / security 组合取用对应字段。
type Spec struct {
	Network  string   `json:"network"`
	Security string   `json:"security"`
	VLESS    *VLESS   `json:"vless,omitempty"`
	SS       *SS      `json:"ss,omitempty"`
	Reality  *Reality `json:"reality,omitempty"`
	TLS      *TLS     `json:"tls,omitempty"`
	WS       *WS      `json:"ws,omitempty"`
	XHTTP    *XHTTP   `json:"xhttp,omitempty"`
	Sniffing bool     `json:"sniffing"`
}

// VLESS 为 VLESS 协议参数。
type VLESS struct {
	Flow string `json:"flow"`
}

// SS 为 Shadowsocks 2022 参数。ServerKey 为服务端密钥，用户密钥由订阅派生。
type SS struct {
	Method    string `json:"method"`
	ServerKey string `json:"serverKey"`
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

// TLS 为标准 TLS 安全层参数。ServerName 同时用作链接 SNI 与 ACME 申请的域名。
type TLS struct {
	ServerName  string   `json:"serverName"`
	CertMode    string   `json:"certMode"`
	CertFile    string   `json:"certFile,omitempty"`
	KeyFile     string   `json:"keyFile,omitempty"`
	ALPN        []string `json:"alpn"`
	Fingerprint string   `json:"fingerprint"`
}

// WS 为 WebSocket 传输参数。
type WS struct {
	Path string `json:"path"`
	Host string `json:"host"`
}

// XHTTP 为 XHTTP 传输参数。
type XHTTP struct {
	Path string `json:"path"`
	Host string `json:"host"`
	Mode string `json:"mode"`
}

var (
	shortIDRe   = regexp.MustCompile(`^([0-9a-f]{2}){0,8}$`)
	clientVerRe = regexp.MustCompile(`^\d{1,3}\.\d{1,3}\.\d{1,3}$`)
	hostnameRe  = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?$`)
	pathRe      = regexp.MustCompile(`^/[A-Za-z0-9._~!$&'()*+,;=:@%/-]*$`)
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

// Validate 校验协议组合与各字段，并就地规范化；与当前组合无关的字段会被清除。
func (s *Spec) Validate(protocol string) error {
	if !combos[[3]string{protocol, s.Network, s.Security}] {
		return fmt.Errorf("不支持的协议组合：%s + %s + %s", protocol, s.Network, s.Security)
	}
	if protocol == ProtoVLESS {
		if s.VLESS == nil {
			s.VLESS = &VLESS{}
		}
		switch {
		case s.VLESS.Flow == "":
		case s.VLESS.Flow != FlowVision:
			return fmt.Errorf("不支持的 flow：%s", s.VLESS.Flow)
		case s.Network != NetworkTCP:
			return errors.New("XTLS Vision 只能用于 TCP 传输")
		}
	} else {
		s.VLESS = nil
	}
	if protocol == ProtoSS {
		if s.SS == nil {
			return errors.New("缺少 Shadowsocks 参数")
		}
		if err := s.SS.validate(); err != nil {
			return err
		}
	} else {
		s.SS = nil
	}

	switch s.Security {
	case SecurityReality:
		s.TLS = nil
		if s.Reality == nil {
			return errors.New("缺少 Reality 参数")
		}
		if err := s.Reality.validate(); err != nil {
			return err
		}
	case SecurityTLS:
		s.Reality = nil
		if s.TLS == nil {
			return errors.New("缺少 TLS 参数")
		}
		if err := s.TLS.validate(); err != nil {
			return err
		}
	default:
		s.Reality, s.TLS = nil, nil
	}

	s.WS, s.XHTTP = keepIf(s.Network == NetworkWS, s.WS), keepIf(s.Network == NetworkXHTTP, s.XHTTP)
	switch s.Network {
	case NetworkWS:
		if s.WS == nil {
			s.WS = &WS{}
		}
		return validatePathHost(&s.WS.Path, &s.WS.Host)
	case NetworkXHTTP:
		if s.XHTTP == nil {
			s.XHTTP = &XHTTP{}
		}
		if s.XHTTP.Mode == "" {
			s.XHTTP.Mode = "auto"
		}
		if !slices.Contains(XHTTPModes, s.XHTTP.Mode) {
			return fmt.Errorf("不支持的 XHTTP 模式：%s", s.XHTTP.Mode)
		}
		return validatePathHost(&s.XHTTP.Path, &s.XHTTP.Host)
	}
	return nil
}

func keepIf[T any](cond bool, v *T) *T {
	if cond {
		return v
	}
	return nil
}

func validatePathHost(path, host *string) error {
	*path, *host = strings.TrimSpace(*path), strings.TrimSpace(*host)
	if *path == "" {
		*path = "/"
	}
	if !pathRe.MatchString(*path) {
		return fmt.Errorf("路径需以 / 开头且只含 URL 合法字符：%s", *path)
	}
	if *host != "" && !hostnameRe.MatchString(*host) {
		return fmt.Errorf("Host 无效：%s", *host)
	}
	return nil
}

func (s *SS) validate() error {
	n, ok := SS2022Methods[s.Method]
	if !ok {
		return fmt.Errorf("不支持的加密方式：%s（多用户仅支持 2022-blake3-aes-128-gcm / 256-gcm）", s.Method)
	}
	s.ServerKey = strings.TrimSpace(s.ServerKey)
	key, err := base64.StdEncoding.DecodeString(s.ServerKey)
	if err != nil || len(key) != n {
		return fmt.Errorf("服务端密钥需为 %d 字节的 base64，请重新生成", n)
	}
	return nil
}

func (t *TLS) validate() error {
	t.ServerName = strings.TrimSpace(t.ServerName)
	if !hostnameRe.MatchString(t.ServerName) || !strings.Contains(t.ServerName, ".") {
		return errors.New("TLS 需要填写证书对应的域名")
	}
	switch t.CertMode {
	case CertFile:
		t.CertFile, t.KeyFile = strings.TrimSpace(t.CertFile), strings.TrimSpace(t.KeyFile)
		if !strings.HasPrefix(t.CertFile, "/") || !strings.HasPrefix(t.KeyFile, "/") {
			return errors.New("证书与私钥需填写节点上的绝对路径")
		}
	case CertACME:
		t.CertFile, t.KeyFile = "", ""
	default:
		return fmt.Errorf("未知的证书来源：%s", t.CertMode)
	}
	t.ALPN = cleanList(t.ALPN)
	for _, a := range t.ALPN {
		if a != "h2" && a != "http/1.1" {
			return fmt.Errorf("不支持的 ALPN：%s", a)
		}
	}
	if t.Fingerprint == "" {
		t.Fingerprint = "chrome"
	}
	if !slices.Contains(Fingerprints, t.Fingerprint) {
		return fmt.Errorf("不支持的指纹：%s", t.Fingerprint)
	}
	return nil
}

func (r *Reality) validate() error {
	r.Target = strings.TrimSpace(r.Target)
	host, port, err := net.SplitHostPort(r.Target)
	if err != nil {
		return errors.New("目标网站格式应为 域名:端口，如 dl.google.com:443")
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

// ACMEDomain 返回入站需要由被控端自动申请证书的域名，不需要时返回空。
func (s *Spec) ACMEDomain() string {
	if s.TLS != nil && s.TLS.CertMode == CertACME {
		return s.TLS.ServerName
	}
	return ""
}

// ClientEmail 返回订阅在某入站上的统计标识，流量统计按此粒度上报。
func ClientEmail(subID, inboundID int64) string {
	return fmt.Sprintf("s%d.i%d", subID, inboundID)
}

// ParseClientEmail 解析 ClientEmail 生成的统计标识。
func ParseClientEmail(email string) (subID, inboundID int64, ok bool) {
	n, err := fmt.Sscanf(email, "s%d.i%d", &subID, &inboundID)
	return subID, inboundID, err == nil && n == 2 && email == ClientEmail(subID, inboundID)
}

// InboundTag 返回入站在 Xray 配置中的 tag。
func InboundTag(inboundID int64) string {
	return fmt.Sprintf("in-%d", inboundID)
}
