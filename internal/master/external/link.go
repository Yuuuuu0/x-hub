package external

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strings"
)

// ShareLink 返回节点在 Base64 订阅中的分享链接，名称替换为 name。来自链接的节点沿用原始链接，
// 否则由节点定义生成；无法用分享链接表达的类型返回空。
func (p Proxy) ShareLink(name string) string {
	if p.Link != "" {
		return renameLink(p.Link, name)
	}
	return toLink(p.Mihomo, name)
}

// renameLink 替换分享链接中的节点名称：vmess 改写 JSON 中的 ps，其余改写 URL 片段。
func renameLink(link, name string) string {
	if strings.HasPrefix(strings.ToLower(link), "vmess://") {
		raw, ok := decodeBase64(link[len("vmess://"):])
		var v map[string]any
		if !ok || json.Unmarshal(raw, &v) != nil {
			return link
		}
		v["ps"] = name
		b, _ := json.Marshal(v)
		return "vmess://" + base64.StdEncoding.EncodeToString(b)
	}
	base, _, _ := strings.Cut(link, "#")
	return base + "#" + url.PathEscape(name)
}

// toLink 由 Mihomo 节点定义生成分享链接，支持 ss / vmess / vless / trojan / hysteria2 / tuic / socks5。
func toLink(m map[string]any, name string) string {
	str := func(k string) string {
		if v, ok := m[k]; ok && v != nil {
			return fmt.Sprint(v)
		}
		return ""
	}
	server, port := str("server"), str("port")
	if server == "" || port == "" {
		return ""
	}
	addr := net.JoinHostPort(server, port)
	frag := "#" + url.PathEscape(name)
	q := url.Values{}
	switch str("type") {
	case "ss":
		if m["plugin"] != nil {
			return ""
		}
		user := base64.RawURLEncoding.EncodeToString([]byte(str("cipher") + ":" + str("password")))
		return "ss://" + user + "@" + addr + frag
	case "vmess":
		v := map[string]string{
			"v": "2", "ps": name, "add": server, "port": port, "id": str("uuid"), "aid": orDefault(str("alterId"), "0"),
			"scy": orDefault(str("cipher"), "auto"), "net": orDefault(str("network"), "tcp"), "type": "none",
		}
		host, path := transportParams(m)
		v["host"], v["path"] = host, path
		if b, _ := m["tls"].(bool); b {
			v["tls"], v["sni"], v["fp"] = "tls", str("servername"), str("client-fingerprint")
		}
		if v["net"] == "http" {
			v["net"], v["type"] = "tcp", "http"
		}
		b, _ := json.Marshal(v)
		return "vmess://" + base64.StdEncoding.EncodeToString(b)
	case "vless", "trojan":
		user := str("uuid")
		sniKey := "servername"
		if str("type") == "trojan" {
			user, sniKey = str("password"), "sni"
			q.Set("security", "tls")
		} else {
			q.Set("encryption", "none")
			if b, _ := m["tls"].(bool); b {
				q.Set("security", "tls")
			}
			setQ(q, "flow", str("flow"))
		}
		setQ(q, "sni", str(sniKey))
		setQ(q, "fp", str("client-fingerprint"))
		if r, ok := m["reality-opts"].(map[string]any); ok {
			q.Set("security", "reality")
			q.Set("pbk", fmt.Sprint(r["public-key"]))
			setQ(q, "sid", fmt.Sprint(orNil(r["short-id"])))
		}
		network := orDefault(str("network"), "tcp")
		switch network {
		case "tcp", "ws", "grpc", "xhttp":
			q.Set("type", network)
		case "h2":
			q.Set("type", "http")
		default:
			return ""
		}
		host, path := transportParams(m)
		setQ(q, "host", host)
		setQ(q, "path", path)
		if g, ok := m["grpc-opts"].(map[string]any); ok {
			setQ(q, "serviceName", fmt.Sprint(orNil(g["grpc-service-name"])))
		}
		if x, ok := m["xhttp-opts"].(map[string]any); ok {
			setQ(q, "mode", fmt.Sprint(orNil(x["mode"])))
		}
		if b, _ := m["skip-cert-verify"].(bool); b {
			q.Set("allowInsecure", "1")
		}
		return str("type") + "://" + url.PathEscape(user) + "@" + addr + "?" + q.Encode() + frag
	case "hysteria2":
		setQ(q, "sni", str("sni"))
		setQ(q, "obfs", str("obfs"))
		setQ(q, "obfs-password", str("obfs-password"))
		setQ(q, "mport", str("ports"))
		if b, _ := m["skip-cert-verify"].(bool); b {
			q.Set("insecure", "1")
		}
		return "hysteria2://" + url.PathEscape(str("password")) + "@" + addr + "/?" + q.Encode() + frag
	case "tuic":
		setQ(q, "sni", str("sni"))
		setQ(q, "congestion_control", str("congestion-controller"))
		setQ(q, "udp_relay_mode", str("udp-relay-mode"))
		if a, ok := m["alpn"].([]any); ok {
			parts := make([]string, len(a))
			for i, x := range a {
				parts[i] = fmt.Sprint(x)
			}
			q.Set("alpn", strings.Join(parts, ","))
		}
		u := url.UserPassword(str("uuid"), str("password"))
		return "tuic://" + u.String() + "@" + addr + "?" + q.Encode() + frag
	case "socks5":
		if user := str("username"); user != "" {
			return "socks://" + base64.RawURLEncoding.EncodeToString([]byte(user+":"+str("password"))) + "@" + addr + frag
		}
		return "socks://" + addr + frag
	}
	return ""
}

// transportParams 返回 ws / h2 / xhttp / http 传输的 Host 与路径。
func transportParams(m map[string]any) (host, path string) {
	first := func(v any) string {
		switch x := v.(type) {
		case []any:
			if len(x) > 0 {
				return fmt.Sprint(x[0])
			}
		case []string:
			if len(x) > 0 {
				return x[0]
			}
		case nil:
		default:
			return fmt.Sprint(x)
		}
		return ""
	}
	for _, k := range []string{"ws-opts", "h2-opts", "xhttp-opts", "http-opts"} {
		o, ok := m[k].(map[string]any)
		if !ok {
			continue
		}
		path = first(o["path"])
		host = first(o["host"])
		if h, ok := o["headers"].(map[string]any); ok && host == "" {
			host = first(h["Host"])
		}
	}
	return host, path
}

func setQ(q url.Values, k, v string) {
	if v != "" {
		q.Set(k, v)
	}
}

func orNil(v any) any {
	if v == nil {
		return ""
	}
	return v
}
