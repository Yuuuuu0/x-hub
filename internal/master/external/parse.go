// Package external 解析与拉取外部节点：分享链接、Mihomo 节点定义与外部订阅，
// 统一表示为 Mihomo 节点定义（附原始分享链接），并可反向生成分享链接。
package external

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Proxy 为一个外部节点。Link 为原始分享链接（来自链接时），Mihomo 为节点定义。
type Proxy struct {
	Name   string         `json:"name"`
	Link   string         `json:"link,omitempty"`
	Mihomo map[string]any `json:"mihomo"`
}

var schemeRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*://`)

// Parse 解析粘贴的内容或订阅响应：每行一条分享链接（可整体 Base64 编码），
// 或 Mihomo YAML（含 proxies 的配置、节点列表或单个节点）。返回节点与被跳过的条目说明。
func Parse(text string) ([]Proxy, []string, error) {
	t := strings.TrimSpace(strings.TrimPrefix(text, "\uFEFF"))
	if t == "" {
		return nil, nil, errors.New("内容为空")
	}
	if !strings.Contains(t, "://") && !strings.Contains(t, "\n") {
		if b, ok := decodeBase64(t); ok && strings.Contains(string(b), "://") {
			t = strings.TrimSpace(string(b))
		}
	}
	if schemeRe.MatchString(t) {
		return parseLinks(t)
	}
	return parseYAML(t)
}

func parseLinks(t string) ([]Proxy, []string, error) {
	var out []Proxy
	var skipped []string
	for _, line := range strings.Split(t, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		m, err := ParseLink(line)
		if err != nil {
			skipped = append(skipped, fmt.Sprintf("%s：%v", abbrev(line), err))
			continue
		}
		out = append(out, Proxy{Name: m["name"].(string), Link: line, Mihomo: m})
	}
	return out, skipped, nil
}

func parseYAML(t string) ([]Proxy, []string, error) {
	var doc any
	if err := yaml.Unmarshal([]byte(t), &doc); err != nil {
		return nil, nil, fmt.Errorf("既不是分享链接也不是合法的 YAML：%w", err)
	}
	var list []any
	switch v := doc.(type) {
	case map[string]any:
		if p, ok := v["proxies"]; ok {
			list, _ = p.([]any)
		} else if _, ok := v["type"]; ok {
			list = []any{v}
		}
	case []any:
		list = v
	}
	if list == nil {
		return nil, nil, errors.New("YAML 中没有找到节点（proxies）")
	}
	var out []Proxy
	var skipped []string
	for i, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			skipped = append(skipped, fmt.Sprintf("第 %d 项不是节点定义", i+1))
			continue
		}
		name, _ := m["name"].(string)
		typ, _ := m["type"].(string)
		server, _ := m["server"].(string)
		if name == "" || typ == "" || server == "" {
			skipped = append(skipped, fmt.Sprintf("第 %d 项缺少 name、type 或 server", i+1))
			continue
		}
		out = append(out, Proxy{Name: name, Mihomo: m})
	}
	return out, skipped, nil
}

// ParseLink 把一条分享链接转换为 Mihomo 节点定义。
func ParseLink(link string) (map[string]any, error) {
	scheme, _, _ := strings.Cut(link, "://")
	switch strings.ToLower(scheme) {
	case "vmess":
		return parseVMess(link)
	case "ss":
		return parseSS(link)
	}
	u, err := url.Parse(link)
	if err != nil {
		return nil, errors.New("链接格式错误")
	}
	host, port, err := hostPort(u)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	m := map[string]any{"server": host, "port": port}
	switch strings.ToLower(u.Scheme) {
	case "vless":
		m["type"], m["uuid"], m["udp"] = "vless", u.User.Username(), true
		if f := q.Get("flow"); f != "" {
			m["flow"] = f
		}
		applyTLS(m, q, "servername")
		if err := applyTransport(m, q.Get("type"), q.Get("headerType"), q.Get("host"), q.Get("path"), q.Get("serviceName"), q.Get("mode")); err != nil {
			return nil, err
		}
	case "trojan":
		m["type"], m["password"], m["udp"] = "trojan", u.User.Username(), true
		applyTLS(m, q, "sni")
		delete(m, "tls") // trojan 始终使用 TLS
		if err := applyTransport(m, q.Get("type"), q.Get("headerType"), q.Get("host"), q.Get("path"), q.Get("serviceName"), q.Get("mode")); err != nil {
			return nil, err
		}
	case "hysteria2", "hy2":
		m["type"], m["password"] = "hysteria2", userPassword(u)
		setIf(m, "sni", q.Get("sni"))
		setIf(m, "obfs", q.Get("obfs"))
		setIf(m, "obfs-password", q.Get("obfs-password"))
		setIf(m, "ports", q.Get("mport"))
		if truthy(q.Get("insecure")) {
			m["skip-cert-verify"] = true
		}
		if a := q.Get("alpn"); a != "" {
			m["alpn"] = strings.Split(a, ",")
		}
	case "tuic":
		pw, _ := u.User.Password()
		m["type"], m["uuid"], m["password"] = "tuic", u.User.Username(), pw
		setIf(m, "sni", q.Get("sni"))
		setIf(m, "congestion-controller", q.Get("congestion_control"))
		setIf(m, "udp-relay-mode", q.Get("udp_relay_mode"))
		if a := q.Get("alpn"); a != "" {
			m["alpn"] = strings.Split(a, ",")
		}
		if truthy(q.Get("allow_insecure")) || truthy(q.Get("insecure")) {
			m["skip-cert-verify"] = true
		}
	case "socks", "socks5":
		m["type"], m["udp"] = "socks5", true
		user, pass := credentials(u)
		setIf(m, "username", user)
		setIf(m, "password", pass)
	case "http", "https":
		if u.Path != "" && u.Path != "/" {
			return nil, errors.New("看起来是订阅地址，请以「订阅」方式添加")
		}
		m["type"] = "http"
		user, pass := credentials(u)
		setIf(m, "username", user)
		setIf(m, "password", pass)
		if u.Scheme == "https" {
			m["tls"] = true
		}
	default:
		return nil, fmt.Errorf("不支持的协议 %s", u.Scheme)
	}
	m["name"] = linkName(u.Fragment, m)
	return m, nil
}

// applyTLS 处理 vless / trojan 链接的 TLS 与 Reality 参数，sniKey 为 Mihomo 中 SNI 的键名。
func applyTLS(m map[string]any, q url.Values, sniKey string) {
	sec := q.Get("security")
	if sec == "tls" || sec == "reality" || sec == "xtls" {
		m["tls"] = true
	}
	setIf(m, sniKey, q.Get("sni"))
	setIf(m, "client-fingerprint", q.Get("fp"))
	if a := q.Get("alpn"); a != "" {
		m["alpn"] = strings.Split(a, ",")
	}
	if truthy(q.Get("allowInsecure")) || truthy(q.Get("insecure")) {
		m["skip-cert-verify"] = true
	}
	if sec == "reality" {
		m["reality-opts"] = map[string]any{"public-key": q.Get("pbk"), "short-id": q.Get("sid")}
		if m["client-fingerprint"] == nil {
			m["client-fingerprint"] = "chrome"
		}
	}
}

// applyTransport 把传输层参数写入 Mihomo 节点定义。
func applyTransport(m map[string]any, network, headerType, host, path, service, mode string) error {
	switch network {
	case "", "tcp", "raw":
		if headerType == "http" {
			opts := map[string]any{"path": []string{orDefault(path, "/")}}
			if host != "" {
				opts["headers"] = map[string]any{"Host": strings.Split(host, ",")}
			}
			m["network"], m["http-opts"] = "http", opts
		}
	case "ws", "httpupgrade":
		opts := map[string]any{"path": orDefault(path, "/")}
		if host != "" {
			opts["headers"] = map[string]any{"Host": host}
		}
		if network == "httpupgrade" {
			opts["v2ray-http-upgrade"] = true
		}
		m["network"], m["ws-opts"] = "ws", opts
	case "grpc":
		m["network"], m["grpc-opts"] = "grpc", map[string]any{"grpc-service-name": service}
	case "h2", "http":
		opts := map[string]any{"path": orDefault(path, "/")}
		if host != "" {
			opts["host"] = strings.Split(host, ",")
		}
		m["network"], m["h2-opts"] = "h2", opts
	case "xhttp", "splithttp":
		opts := map[string]any{"path": orDefault(path, "/")}
		setIf(opts, "host", host)
		setIf(opts, "mode", mode)
		m["network"], m["xhttp-opts"] = "xhttp", opts
	default:
		return fmt.Errorf("不支持的传输方式 %s", network)
	}
	return nil
}

func parseVMess(link string) (map[string]any, error) {
	raw, ok := decodeBase64(link[len("vmess://"):])
	if !ok {
		return nil, errors.New("vmess 链接不是 Base64 编码的 JSON")
	}
	var v map[string]any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, errors.New("vmess 链接内容不是 JSON")
	}
	str := func(k string) string {
		switch x := v[k].(type) {
		case string:
			return strings.TrimSpace(x)
		case float64:
			return strconv.FormatFloat(x, 'f', -1, 64)
		}
		return ""
	}
	port, err := strconv.Atoi(str("port"))
	if err != nil || port <= 0 || port > 65535 || str("add") == "" {
		return nil, errors.New("vmess 链接缺少地址或端口")
	}
	aid, _ := strconv.Atoi(str("aid"))
	m := map[string]any{
		"type": "vmess", "server": str("add"), "port": port, "uuid": str("id"),
		"alterId": aid, "cipher": orDefault(str("scy"), "auto"), "udp": true,
	}
	if str("tls") == "tls" {
		m["tls"] = true
		setIf(m, "servername", orDefault(str("sni"), str("host")))
		setIf(m, "client-fingerprint", str("fp"))
		if a := str("alpn"); a != "" {
			m["alpn"] = strings.Split(a, ",")
		}
	}
	if err := applyTransport(m, str("net"), str("type"), str("host"), str("path"), str("path"), ""); err != nil {
		return nil, err
	}
	m["name"] = linkName(str("ps"), m)
	return m, nil
}

func parseSS(link string) (map[string]any, error) {
	body, frag, _ := strings.Cut(link[len("ss://"):], "#")
	name, _ := url.PathUnescape(frag)
	// 旧格式：ss://base64(method:password@host:port)
	if main, _, _ := strings.Cut(body, "?"); !strings.Contains(main, "@") {
		dec, ok := decodeBase64(strings.TrimSuffix(main, "/"))
		if !ok {
			return nil, errors.New("ss 链接格式错误")
		}
		body = string(dec)
	}
	u, err := url.Parse("ss://" + body)
	if err != nil {
		return nil, errors.New("ss 链接格式错误")
	}
	host, port, err := hostPort(u)
	if err != nil {
		return nil, err
	}
	method, password := u.User.Username(), ""
	if p, ok := u.User.Password(); ok {
		password = p
	} else if dec, ok := decodeBase64(method); ok && strings.Contains(string(dec), ":") {
		method, password, _ = strings.Cut(string(dec), ":")
	}
	if method == "" || password == "" {
		return nil, errors.New("ss 链接缺少加密方式或密码")
	}
	m := map[string]any{"type": "ss", "server": host, "port": port, "cipher": method, "password": password, "udp": true}
	if plugin := u.Query().Get("plugin"); plugin != "" {
		if err := applySSPlugin(m, plugin); err != nil {
			return nil, err
		}
	}
	m["name"] = linkName(name, m)
	return m, nil
}

// applySSPlugin 转换 SIP003 插件参数，支持 obfs 与 v2ray-plugin。
func applySSPlugin(m map[string]any, plugin string) error {
	parts := strings.Split(plugin, ";")
	opts := map[string]string{}
	for _, p := range parts[1:] {
		k, v, _ := strings.Cut(p, "=")
		opts[k] = v
	}
	switch parts[0] {
	case "obfs-local", "simple-obfs", "obfs":
		m["plugin"] = "obfs"
		m["plugin-opts"] = map[string]any{"mode": opts["obfs"], "host": opts["obfs-host"]}
	case "v2ray-plugin":
		po := map[string]any{"mode": orDefault(opts["mode"], "websocket")}
		if _, ok := opts["tls"]; ok {
			po["tls"] = true
		}
		if h, ok := opts["host"]; ok {
			po["host"] = h
		}
		if p, ok := opts["path"]; ok {
			po["path"] = p
		}
		m["plugin"], m["plugin-opts"] = "v2ray-plugin", po
	default:
		return fmt.Errorf("不支持的 ss 插件 %s", parts[0])
	}
	return nil
}

func hostPort(u *url.URL) (string, int, error) {
	host := u.Hostname()
	port, err := strconv.Atoi(u.Port())
	if host == "" || err != nil || port <= 0 || port > 65535 {
		return "", 0, errors.New("缺少地址或端口")
	}
	return host, port, nil
}

// credentials 读取链接中的用户名与密码，兼容 v2rayN 的 Base64(user:pass) 写法。
func credentials(u *url.URL) (string, string) {
	if u.User == nil {
		return "", ""
	}
	user := u.User.Username()
	if pass, ok := u.User.Password(); ok {
		return user, pass
	}
	if dec, ok := decodeBase64(user); ok && strings.Contains(string(dec), ":") {
		user, pass, _ := strings.Cut(string(dec), ":")
		return user, pass
	}
	return user, ""
}

// userPassword 返回 hysteria2 链接中的认证信息（user:pass 形式时保留完整原文）。
func userPassword(u *url.URL) string {
	if u.User == nil {
		return ""
	}
	if p, ok := u.User.Password(); ok {
		return u.User.Username() + ":" + p
	}
	return u.User.Username()
}

func linkName(name string, m map[string]any) string {
	if name = strings.TrimSpace(name); name != "" {
		return name
	}
	return fmt.Sprintf("%s %s", m["type"], net.JoinHostPort(fmt.Sprint(m["server"]), fmt.Sprint(m["port"])))
}

func decodeBase64(s string) ([]byte, bool) {
	s = strings.TrimSpace(s)
	s = strings.NewReplacer("\n", "", "\r", "", " ", "").Replace(s)
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(s); err == nil {
			return b, true
		}
	}
	return nil, false
}

func setIf(m map[string]any, k, v string) {
	if v != "" {
		m[k] = v
	}
}

func truthy(s string) bool { return s == "1" || strings.EqualFold(s, "true") }

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

func abbrev(s string) string {
	if r := []rune(s); len(r) > 48 {
		return string(r[:48]) + "…"
	}
	return s
}
