package external

import (
	"fmt"
	"strconv"
	"strings"
)

// SingBox 把 Mihomo 节点定义转换为 sing-box 出站（tag 为 name），不支持的类型或传输方式返回 false。
func SingBox(m map[string]any, name string) (map[string]any, bool) {
	str := func(k string) string {
		if v, ok := m[k]; ok && v != nil {
			return fmt.Sprint(v)
		}
		return ""
	}
	port, err := strconv.Atoi(str("port"))
	if err != nil || str("server") == "" {
		return nil, false
	}
	out := map[string]any{"tag": name, "server": str("server"), "server_port": port}
	tls := func(sniKey string, always bool) {
		on, _ := m["tls"].(bool)
		if !on && !always {
			return
		}
		t := map[string]any{"enabled": true}
		if s := str(sniKey); s != "" {
			t["server_name"] = s
		}
		if b, _ := m["skip-cert-verify"].(bool); b {
			t["insecure"] = true
		}
		if a := stringList(m["alpn"]); len(a) > 0 {
			t["alpn"] = a
		}
		if fp := str("client-fingerprint"); fp != "" {
			t["utls"] = map[string]any{"enabled": true, "fingerprint": fp}
		}
		if r, ok := m["reality-opts"].(map[string]any); ok {
			t["reality"] = map[string]any{"enabled": true, "public_key": fmt.Sprint(r["public-key"]), "short_id": fmt.Sprint(orNil(r["short-id"]))}
			if t["utls"] == nil {
				t["utls"] = map[string]any{"enabled": true, "fingerprint": "chrome"}
			}
		}
		out["tls"] = t
	}

	switch str("type") {
	case "vless":
		out["type"], out["uuid"] = "vless", str("uuid")
		if f := str("flow"); f != "" {
			out["flow"] = f
		}
		tls("servername", false)
	case "vmess":
		cipher := str("cipher")
		if cipher == "" || cipher == "auto" {
			cipher = "aes-128-gcm" // 服务端按请求头识别加密方式，客户端选择固定值即可
		}
		aid, _ := strconv.Atoi(str("alterId"))
		out["type"], out["uuid"], out["security"], out["alter_id"] = "vmess", str("uuid"), cipher, aid
		tls("servername", false)
	case "trojan":
		out["type"], out["password"] = "trojan", str("password")
		tls("sni", true)
	case "ss":
		out["type"], out["method"], out["password"] = "shadowsocks", str("cipher"), str("password")
		if plugin := str("plugin"); plugin != "" {
			opts, _ := m["plugin-opts"].(map[string]any)
			switch plugin {
			case "obfs":
				out["plugin"], out["plugin_opts"] = "obfs-local", fmt.Sprintf("obfs=%v;obfs-host=%v", orNil(opts["mode"]), orNil(opts["host"]))
			case "v2ray-plugin":
				parts := []string{"mode=" + fmt.Sprint(orNil(opts["mode"]))}
				if b, _ := opts["tls"].(bool); b {
					parts = append(parts, "tls")
				}
				for _, k := range []string{"host", "path"} {
					if v, ok := opts[k]; ok {
						parts = append(parts, k+"="+fmt.Sprint(v))
					}
				}
				out["plugin"], out["plugin_opts"] = "v2ray-plugin", strings.Join(parts, ";")
			default:
				return nil, false
			}
		}
		return out, true
	case "hysteria2":
		out["type"], out["password"] = "hysteria2", str("password")
		if o := str("obfs"); o != "" {
			out["obfs"] = map[string]any{"type": o, "password": str("obfs-password")}
		}
		if ports := str("ports"); ports != "" {
			out["server_ports"] = strings.Split(strings.ReplaceAll(ports, "-", ":"), ",")
		}
		tls("sni", true)
		return out, true
	case "tuic":
		out["type"], out["uuid"], out["password"] = "tuic", str("uuid"), str("password")
		if c := str("congestion-controller"); c != "" {
			out["congestion_control"] = c
		}
		if u := str("udp-relay-mode"); u != "" {
			out["udp_relay_mode"] = u
		}
		tls("sni", true)
		return out, true
	case "socks5":
		out["type"], out["version"] = "socks", "5"
		setIfAny(out, "username", str("username"))
		setIfAny(out, "password", str("password"))
		return out, true
	case "http":
		out["type"] = "http"
		setIfAny(out, "username", str("username"))
		setIfAny(out, "password", str("password"))
		tls("sni", false)
		return out, true
	default:
		return nil, false
	}

	// vless / vmess / trojan 的传输层。
	host, path := transportParams(m)
	switch str("network") {
	case "", "tcp":
	case "ws":
		ws, _ := m["ws-opts"].(map[string]any)
		if up, _ := ws["v2ray-http-upgrade"].(bool); up {
			out["transport"] = map[string]any{"type": "httpupgrade", "path": path, "host": host}
			break
		}
		t := map[string]any{"type": "ws", "path": orDefault(path, "/")}
		if host != "" {
			t["headers"] = map[string]any{"Host": host}
		}
		out["transport"] = t
	case "grpc":
		g, _ := m["grpc-opts"].(map[string]any)
		out["transport"] = map[string]any{"type": "grpc", "service_name": fmt.Sprint(orNil(g["grpc-service-name"]))}
	case "h2", "http":
		t := map[string]any{"type": "http", "path": orDefault(path, "/")}
		if host != "" {
			t["host"] = []string{host}
		}
		out["transport"] = t
	default:
		return nil, false // xhttp 等 sing-box 不支持的传输方式
	}
	return out, true
}

func stringList(v any) []string {
	switch x := v.(type) {
	case []string:
		return x
	case []any:
		out := make([]string, len(x))
		for i, s := range x {
			out[i] = fmt.Sprint(s)
		}
		return out
	}
	return nil
}

func setIfAny(m map[string]any, k, v string) {
	if v != "" {
		m[k] = v
	}
}
