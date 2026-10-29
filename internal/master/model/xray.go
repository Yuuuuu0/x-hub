package model

// 被控端证书目录（相对 Xray 工作目录，即被控端数据目录）。ACME 证书由被控端写入此处。
const acmeCertDir = "certs"

// ACMECertPaths 返回 ACME 证书在被控端的相对路径。
func ACMECertPaths(domain string) (cert, key string) {
	return acmeCertDir + "/" + domain + "/fullchain.pem", acmeCertDir + "/" + domain + "/privkey.pem"
}

// XrayInbound 生成服务端 inbound 配置。
func XrayInbound(id int64, listen string, port int, protocol string, s *Spec, clients []Client) map[string]any {
	if protocol == ProtoSS && len(clients) == 0 {
		clients = []Client{ssPlaceholder(s)}
	}
	cs := make([]any, 0, len(clients))
	for _, c := range clients {
		switch protocol {
		case ProtoVLESS:
			cs = append(cs, map[string]any{"id": c.Cred.UUID, "email": c.Email, "flow": s.VLESS.Flow})
		case ProtoVMess:
			cs = append(cs, map[string]any{"id": c.Cred.UUID, "email": c.Email})
		case ProtoTrojan, ProtoSS:
			cs = append(cs, map[string]any{"password": c.Cred.Password, "email": c.Email})
		}
	}
	settings := map[string]any{"clients": cs}
	switch protocol {
	case ProtoVLESS:
		settings["decryption"] = "none"
	case ProtoSS:
		settings["method"], settings["password"], settings["network"] = s.SS.Method, s.SS.ServerKey, "tcp,udp"
	}

	stream := map[string]any{"network": s.Network, "security": s.Security}
	switch s.Security {
	case SecurityReality:
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
		stream["realitySettings"] = reality
	case SecurityTLS:
		cert, key := s.TLS.CertFile, s.TLS.KeyFile
		if s.TLS.CertMode == CertACME {
			cert, key = ACMECertPaths(s.TLS.ServerName)
		}
		tls := map[string]any{
			"serverName":   s.TLS.ServerName,
			"certificates": []any{map[string]any{"certificateFile": cert, "keyFile": key}},
		}
		if len(s.TLS.ALPN) > 0 {
			tls["alpn"] = s.TLS.ALPN
		}
		stream["tlsSettings"] = tls
	}
	switch s.Network {
	case NetworkWS:
		ws := map[string]any{"path": s.WS.Path}
		if s.WS.Host != "" {
			ws["host"] = s.WS.Host
		}
		stream["wsSettings"] = ws
	case NetworkXHTTP:
		x := map[string]any{"path": s.XHTTP.Path, "mode": s.XHTTP.Mode}
		if s.XHTTP.Host != "" {
			x["host"] = s.XHTTP.Host
		}
		stream["xhttpSettings"] = x
	}

	in := map[string]any{
		"tag":            InboundTag(id),
		"port":           port,
		"protocol":       protocol,
		"settings":       settings,
		"streamSettings": stream,
	}
	if listen != "" {
		in["listen"] = listen
	}
	if s.Sniffing {
		in["sniffing"] = map[string]any{"enabled": true, "destOverride": []string{"http", "tls", "quic"}, "routeOnly": true}
	}
	return in
}
