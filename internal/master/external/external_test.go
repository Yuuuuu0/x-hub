package external

import (
	"encoding/base64"
	"strings"
	"testing"
)

// TestParseLinks 覆盖各协议分享链接到 Mihomo 节点定义的转换。
func TestParseLinks(t *testing.T) {
	cases := []struct {
		link string
		want map[string]any
	}{
		{"vless://11111111-1111-1111-1111-111111111111@1.2.3.4:443?security=reality&sni=dl.google.com&fp=chrome&pbk=PUB&sid=ab&flow=xtls-rprx-vision&type=tcp#%E7%BE%8E%E5%9B%BD",
			map[string]any{"name": "美国", "type": "vless", "servername": "dl.google.com", "flow": "xtls-rprx-vision", "tls": true}},
		{"trojan://pa%40ss@t.example.com:443?sni=t.example.com&type=ws&path=%2Fws&host=cdn.example.com#T",
			map[string]any{"type": "trojan", "password": "pa@ss", "sni": "t.example.com", "network": "ws"}},
		{"hysteria2://auth@h.example.com:8443/?sni=h.example.com&insecure=1&obfs=salamander&obfs-password=x#H",
			map[string]any{"type": "hysteria2", "password": "auth", "skip-cert-verify": true, "obfs": "salamander"}},
		{"tuic://uuid:pw@u.example.com:443?congestion_control=bbr&alpn=h3#U",
			map[string]any{"type": "tuic", "uuid": "uuid", "password": "pw", "congestion-controller": "bbr"}},
		{"socks5://user:pass@5.6.7.8:1080#家宽", map[string]any{"name": "家宽", "type": "socks5", "username": "user", "password": "pass"}},
		{"socks://" + base64.StdEncoding.EncodeToString([]byte("u:p")) + "@5.6.7.8:1080#S", map[string]any{"username": "u", "password": "p"}},
		{"http://u:p@9.9.9.9:8080", map[string]any{"type": "http", "name": "http 9.9.9.9:8080"}},
		{"ss://" + base64.RawURLEncoding.EncodeToString([]byte("2022-blake3-aes-128-gcm:a:b")) + "@s.example.com:8388#SS",
			map[string]any{"type": "ss", "cipher": "2022-blake3-aes-128-gcm", "password": "a:b"}},
		{"ss://" + base64.StdEncoding.EncodeToString([]byte("aes-256-gcm:pw@1.1.1.1:8388")) + "#Old",
			map[string]any{"type": "ss", "cipher": "aes-256-gcm", "server": "1.1.1.1", "port": 8388}},
		{"ss://YWVzLTI1Ni1nY206cHc@o.example.com:443/?plugin=obfs-local%3Bobfs%3Dhttp%3Bobfs-host%3Dbing.com#O",
			map[string]any{"plugin": "obfs"}},
		{"vmess://" + base64.StdEncoding.EncodeToString([]byte(`{"v":"2","ps":"V","add":"v.example.com","port":"443","id":"id1","aid":0,"net":"ws","path":"/p","host":"h.com","tls":"tls"}`)),
			map[string]any{"name": "V", "type": "vmess", "port": 443, "network": "ws", "servername": "h.com", "alterId": 0}},
	}
	for _, c := range cases {
		m, err := ParseLink(c.link)
		if err != nil {
			t.Fatalf("%s: %v", c.link, err)
		}
		for k, v := range c.want {
			if m[k] != v {
				t.Errorf("%s: %s = %v, want %v", c.link, k, m[k], v)
			}
		}
	}
	if _, err := ParseLink("https://sub.example.com/api/v1/client?token=x"); err == nil {
		t.Error("subscription URL must be rejected as a node")
	}
}

// TestParseFormats 覆盖整体 Base64、Mihomo 配置与跳过无效条目。
func TestParseFormats(t *testing.T) {
	blob := base64.StdEncoding.EncodeToString([]byte("socks5://1.1.1.1:1080#A\nfoo://bar\nhttp://2.2.2.2:3128#B\n"))
	ps, skipped, err := Parse(blob)
	if err != nil || len(ps) != 2 || len(skipped) != 1 || ps[1].Name != "B" {
		t.Fatalf("base64: %v %v %v", ps, skipped, err)
	}
	ps, skipped, err = Parse("port: 7890\nproxies:\n  - {name: A, type: ss, server: a.com, port: 1, cipher: aes-128-gcm, password: x}\n  - {name: B}\n")
	if err != nil || len(ps) != 1 || ps[0].Name != "A" || len(skipped) != 1 {
		t.Fatalf("yaml: %v %v %v", ps, skipped, err)
	}
	if ps, _, err = Parse("name: C\ntype: socks5\nserver: c.com\nport: 1080\n"); err != nil || len(ps) != 1 {
		t.Fatalf("single: %v %v", ps, err)
	}
	if _, _, err = Parse("port: 7890\n"); err == nil {
		t.Fatal("config without proxies must fail")
	}
}

// TestShareLink 覆盖改名与由节点定义生成链接（再解析应得到等价节点）。
func TestShareLink(t *testing.T) {
	m, _ := ParseLink("trojan://pw@t.com:443?sni=t.com#Old")
	p := Proxy{Name: "Old", Link: "trojan://pw@t.com:443?sni=t.com#Old", Mihomo: m}
	if got := p.ShareLink("新 名"); got != "trojan://pw@t.com:443?sni=t.com#%E6%96%B0%20%E5%90%8D" {
		t.Fatalf("rename: %s", got)
	}
	ps, _, _ := Parse(`proxies:
  - {name: R, type: vless, server: r.com, port: 443, uuid: u1, tls: true, servername: s.com, flow: xtls-rprx-vision, client-fingerprint: chrome, reality-opts: {public-key: PK, short-id: ab}}
  - {name: W, type: vmess, server: w.com, port: 443, uuid: u2, alterId: 0, cipher: auto, tls: true, servername: w.com, network: ws, ws-opts: {path: /x, headers: {Host: w.com}}}
  - {name: S, type: ss, server: s.com, port: 8388, cipher: aes-128-gcm, password: pw}
  - {name: H, type: hysteria2, server: h.com, port: 443, password: pw, sni: h.com}
  - {name: G, type: wireguard, server: g.com, port: 51820}
`)
	for _, p := range ps {
		link := p.ShareLink(p.Name)
		if p.Name == "G" {
			if link != "" {
				t.Fatalf("wireguard must not produce a link: %s", link)
			}
			continue
		}
		back, err := ParseLink(link)
		if err != nil {
			t.Fatalf("%s: %s: %v", p.Name, link, err)
		}
		for _, k := range []string{"name", "type", "server", "uuid", "password", "servername", "flow", "network"} {
			if p.Mihomo[k] != nil && back[k] != p.Mihomo[k] {
				t.Errorf("%s: %s = %v, want %v (%s)", p.Name, k, back[k], p.Mihomo[k], link)
			}
		}
		if p.Name == "R" && !strings.Contains(link, "pbk=PK") {
			t.Errorf("reality params lost: %s", link)
		}
	}
}
