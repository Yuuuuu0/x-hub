package model

import (
	"encoding/base64"
	"encoding/json"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// LinkTarget 为生成客户端链接所需的入站外部信息。
type LinkTarget struct {
	Host string // 客户端连接地址：入站覆盖地址或服务器公网地址
	Port int
	Name string // 链接备注
}

// clientInfo 汇总客户端视角的安全层参数，供各输出格式复用。
type clientInfo struct {
	sni, fp, alpn string
	host, path    string // WS / XHTTP 的 Host 与路径
}

func (s *Spec) client() clientInfo {
	var c clientInfo
	switch s.Security {
	case SecurityReality:
		c.sni, c.fp = s.Reality.ServerNames[0], s.Reality.Fingerprint
	case SecurityTLS:
		c.sni, c.fp, c.alpn = s.TLS.ServerName, s.TLS.Fingerprint, strings.Join(s.TLS.ALPN, ",")
	}
	switch s.Network {
	case NetworkWS:
		c.host, c.path = s.WS.Host, s.WS.Path
	case NetworkXHTTP:
		c.host, c.path = s.XHTTP.Host, s.XHTTP.Path
	}
	if c.host == "" {
		c.host = c.sni // CDN 场景下 Host 通常与证书域名一致
	}
	return c
}

// ssPassword 返回 SS2022 多用户的客户端密码：服务端密钥:用户密钥。
func ssPassword(s *Spec, cred Credential) string {
	return s.SS.ServerKey + ":" + cred.Password
}

// ShareLink 生成分享链接（v2rayN / Shadowrocket 等通用格式）。
func ShareLink(protocol string, s *Spec, t LinkTarget, cred Credential) string {
	c := s.client()
	addr := net.JoinHostPort(t.Host, strconv.Itoa(t.Port))
	switch protocol {
	case ProtoVMess:
		v := map[string]string{
			"v": "2", "ps": t.Name, "add": t.Host, "port": strconv.Itoa(t.Port), "id": cred.UUID, "aid": "0",
			"scy": "auto", "net": s.Network, "type": "none", "host": c.host, "path": c.path,
			"tls": SecurityTLS, "sni": c.sni, "alpn": c.alpn, "fp": c.fp,
		}
		b, _ := json.Marshal(v)
		return "vmess://" + base64.StdEncoding.EncodeToString(b)
	case ProtoSS:
		user := base64.RawURLEncoding.EncodeToString([]byte(s.SS.Method + ":" + ssPassword(s, cred)))
		return "ss://" + user + "@" + addr + "#" + url.PathEscape(t.Name)
	}

	q := url.Values{}
	q.Set("type", s.Network)
	q.Set("security", s.Security)
	q.Set("sni", c.sni)
	q.Set("fp", c.fp)
	if c.alpn != "" {
		q.Set("alpn", c.alpn)
	}
	switch s.Network {
	case NetworkTCP:
		q.Set("headerType", "none")
	case NetworkWS:
		q.Set("path", c.path)
		q.Set("host", c.host)
	case NetworkXHTTP:
		q.Set("path", c.path)
		q.Set("host", c.host)
		q.Set("mode", s.XHTTP.Mode)
	}
	if s.Security == SecurityReality {
		r := s.Reality
		q.Set("pbk", r.PublicKey)
		q.Set("sid", r.ShortIDs[0])
		q.Set("spx", r.SpiderX)
	}
	user := cred.Password
	if protocol == ProtoVLESS {
		user = cred.UUID
		q.Set("encryption", "none")
		if s.VLESS.Flow != "" {
			q.Set("flow", s.VLESS.Flow)
		}
	}
	u := url.URL{Scheme: protocol, User: url.User(user), Host: addr, RawQuery: q.Encode(), Fragment: t.Name}
	return u.String()
}

// MihomoProxy 为 Mihomo（Clash Meta）配置中的一个代理节点，字段顺序即输出顺序。
type MihomoProxy struct {
	Name              string       `yaml:"name"`
	Type              string       `yaml:"type"`
	Server            string       `yaml:"server"`
	Port              int          `yaml:"port"`
	UUID              string       `yaml:"uuid,omitempty"`
	Password          string       `yaml:"password,omitempty"`
	Cipher            string       `yaml:"cipher,omitempty"`
	AlterID           *int         `yaml:"alterId,omitempty"`
	Network           string       `yaml:"network,omitempty"`
	UDP               bool         `yaml:"udp"`
	TLS               bool         `yaml:"tls,omitempty"`
	Flow              string       `yaml:"flow,omitempty"`
	ServerName        string       `yaml:"servername,omitempty"`
	SNI               string       `yaml:"sni,omitempty"`
	ALPN              []string     `yaml:"alpn,omitempty"`
	ClientFingerprint string       `yaml:"client-fingerprint,omitempty"`
	RealityOpts       *RealityOpts `yaml:"reality-opts,omitempty"`
	WSOpts            *WSOpts      `yaml:"ws-opts,omitempty"`
	XHTTPOpts         *XHTTPOpts   `yaml:"xhttp-opts,omitempty"`
}

// RealityOpts 为 Mihomo 的 Reality 参数。
type RealityOpts struct {
	PublicKey string `yaml:"public-key"`
	ShortID   string `yaml:"short-id"`
}

// WSOpts 为 Mihomo 的 WebSocket 参数。
type WSOpts struct {
	Path    string            `yaml:"path"`
	Headers map[string]string `yaml:"headers,omitempty"`
}

// XHTTPOpts 为 Mihomo 的 XHTTP 参数。
type XHTTPOpts struct {
	Path string `yaml:"path"`
	Host string `yaml:"host,omitempty"`
	Mode string `yaml:"mode,omitempty"`
}

// Mihomo 生成 Mihomo 代理节点。
func Mihomo(protocol string, s *Spec, t LinkTarget, cred Credential) MihomoProxy {
	c := s.client()
	p := MihomoProxy{Name: t.Name, Type: protocol, Server: t.Host, Port: t.Port, UDP: true}
	switch protocol {
	case ProtoVLESS:
		p.UUID, p.Flow = cred.UUID, s.VLESS.Flow
	case ProtoVMess:
		zero := 0
		p.UUID, p.AlterID, p.Cipher = cred.UUID, &zero, "auto"
	case ProtoTrojan:
		p.Password = cred.Password
	case ProtoSS:
		p.Type, p.Cipher, p.Password = "ss", s.SS.Method, ssPassword(s, cred)
		return p
	}
	if protocol == ProtoTrojan {
		p.SNI = c.sni // Mihomo 的 trojan 使用 sni 字段
	} else {
		p.TLS, p.ServerName = true, c.sni
	}
	p.ClientFingerprint = c.fp
	if s.Security == SecurityTLS {
		p.ALPN = s.TLS.ALPN
	}
	if s.Security == SecurityReality {
		p.RealityOpts = &RealityOpts{PublicKey: s.Reality.PublicKey, ShortID: s.Reality.ShortIDs[0]}
	}
	p.Network = s.Network
	switch s.Network {
	case NetworkWS:
		p.WSOpts = &WSOpts{Path: c.path, Headers: map[string]string{"Host": c.host}}
	case NetworkXHTTP:
		p.XHTTPOpts = &XHTTPOpts{Path: c.path, Host: c.host, Mode: s.XHTTP.Mode}
	}
	return p
}

// SingBox 生成 sing-box 出站。sing-box 不支持 XHTTP，此时返回 false。
func SingBox(protocol string, s *Spec, t LinkTarget, cred Credential) (map[string]any, bool) {
	if s.Network == NetworkXHTTP {
		return nil, false
	}
	c := s.client()
	o := map[string]any{"type": protocol, "tag": t.Name, "server": t.Host, "server_port": t.Port}
	switch protocol {
	case ProtoVLESS:
		o["uuid"] = cred.UUID
		if s.VLESS.Flow != "" {
			o["flow"] = s.VLESS.Flow
		}
	case ProtoVMess:
		// sing-box 的 auto 会发送 Xray 不识别的加密类型（实测报 unknown security type），需明确指定。
		o["uuid"], o["security"], o["alter_id"] = cred.UUID, "aes-128-gcm", 0
	case ProtoTrojan:
		o["password"] = cred.Password
	case ProtoSS:
		o["method"], o["password"] = s.SS.Method, ssPassword(s, cred)
		return o, true
	}
	tls := map[string]any{
		"enabled":     true,
		"server_name": c.sni,
		"utls":        map[string]any{"enabled": true, "fingerprint": c.fp},
	}
	if s.Security == SecurityTLS && len(s.TLS.ALPN) > 0 {
		tls["alpn"] = s.TLS.ALPN
	}
	if s.Security == SecurityReality {
		tls["reality"] = map[string]any{"enabled": true, "public_key": s.Reality.PublicKey, "short_id": s.Reality.ShortIDs[0]}
	}
	o["tls"] = tls
	if s.Network == NetworkWS {
		o["transport"] = map[string]any{"type": "ws", "path": c.path, "headers": map[string]string{"Host": c.host}}
	}
	return o, true
}
