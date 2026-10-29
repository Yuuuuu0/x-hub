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
