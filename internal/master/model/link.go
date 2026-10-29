package model

import (
	"net"
	"net/url"
	"strconv"
)

// LinkTarget 为生成客户端链接所需的入站外部信息。
type LinkTarget struct {
	Host string // 客户端连接地址：入站覆盖地址或服务器公网地址
	Port int
	Name string // 链接备注
}

// ShareLink 生成 vless:// 分享链接（v2rayN / Shadowrocket 等通用格式）。
func ShareLink(s *Spec, t LinkTarget, uuid string) string {
	r := s.Reality
	q := url.Values{}
	q.Set("encryption", "none")
	if s.VLESS.Flow != "" {
		q.Set("flow", s.VLESS.Flow)
	}
	q.Set("security", SecurityReality)
	q.Set("sni", r.ServerNames[0])
	q.Set("fp", r.Fingerprint)
	q.Set("pbk", r.PublicKey)
	q.Set("sid", r.ShortIDs[0])
	q.Set("spx", r.SpiderX)
	q.Set("type", NetworkTCP)
	q.Set("headerType", "none")
	u := url.URL{
		Scheme:   "vless",
		User:     url.User(uuid),
		Host:     net.JoinHostPort(t.Host, strconv.Itoa(t.Port)),
		RawQuery: q.Encode(),
		Fragment: t.Name,
	}
	return u.String()
}

// MihomoProxy 为 Mihomo（Clash Meta）配置中的一个代理节点，字段顺序即输出顺序。
type MihomoProxy struct {
	Name              string       `yaml:"name"`
	Type              string       `yaml:"type"`
	Server            string       `yaml:"server"`
	Port              int          `yaml:"port"`
	UUID              string       `yaml:"uuid"`
	Network           string       `yaml:"network"`
	UDP               bool         `yaml:"udp"`
	TLS               bool         `yaml:"tls"`
	Flow              string       `yaml:"flow,omitempty"`
	ServerName        string       `yaml:"servername"`
	ClientFingerprint string       `yaml:"client-fingerprint"`
	RealityOpts       *RealityOpts `yaml:"reality-opts"`
}

// RealityOpts 为 Mihomo 的 Reality 参数。
type RealityOpts struct {
	PublicKey string `yaml:"public-key"`
	ShortID   string `yaml:"short-id"`
}

// Mihomo 生成 Mihomo 代理节点。
func Mihomo(s *Spec, t LinkTarget, uuid string) MihomoProxy {
	r := s.Reality
	return MihomoProxy{
		Name: t.Name, Type: ProtoVLESS, Server: t.Host, Port: t.Port, UUID: uuid,
		Network: NetworkTCP, UDP: true, TLS: true, Flow: s.VLESS.Flow,
		ServerName: r.ServerNames[0], ClientFingerprint: r.Fingerprint,
		RealityOpts: &RealityOpts{PublicKey: r.PublicKey, ShortID: r.ShortIDs[0]},
	}
}
