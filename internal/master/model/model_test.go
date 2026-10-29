package model

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

func realitySpec() *Spec {
	priv, _ := GenerateX25519()
	return &Spec{
		Network: NetworkTCP, Security: SecurityReality, VLESS: &VLESS{Flow: FlowVision}, Sniffing: true,
		Reality: &Reality{Target: "dl.google.com:443", ServerNames: []string{" dl.google.com ", ""},
			PrivateKey: priv, ShortIDs: []string{"AB12"}, SpiderX: ""},
	}
}

func tlsSpec(network string) *Spec {
	return &Spec{Network: network, Security: SecurityTLS,
		TLS: &TLS{ServerName: "cdn.example.com", CertMode: CertACME, ALPN: []string{"h2", "http/1.1"}},
		WS:  &WS{Path: "/ray"}}
}

func ssSpec() *Spec {
	key, _ := GenerateSSKey("2022-blake3-aes-128-gcm")
	return &Spec{Network: NetworkTCP, Security: SecurityNone, SS: &SS{Method: "2022-blake3-aes-128-gcm", ServerKey: key}}
}

func TestValidateNormalizes(t *testing.T) {
	s := realitySpec()
	if err := s.Validate(ProtoVLESS); err != nil {
		t.Fatal(err)
	}
	r := s.Reality
	if r.PublicKey == "" || r.ServerNames[0] != "dl.google.com" || len(r.ServerNames) != 1 ||
		r.ShortIDs[0] != "ab12" || r.Fingerprint != "chrome" || r.SpiderX != "/" {
		t.Fatalf("not normalized: %+v", r)
	}
	for name, c := range map[string]struct {
		proto  string
		mutate func(*Spec)
	}{
		"combo":        {ProtoVLESS, func(s *Spec) { s.Network = "grpc" }},
		"flow":         {ProtoVLESS, func(s *Spec) { s.VLESS.Flow = "xtls-rprx-direct" }},
		"vision+xhttp": {ProtoVLESS, func(s *Spec) { s.Network = NetworkXHTTP }},
		"target":       {ProtoVLESS, func(s *Spec) { s.Reality.Target = "dl.google.com" }},
		"sni":          {ProtoVLESS, func(s *Spec) { s.Reality.ServerNames = nil }},
		"private key":  {ProtoVLESS, func(s *Spec) { s.Reality.PrivateKey = "bad" }},
		"short id":     {ProtoVLESS, func(s *Spec) { s.Reality.ShortIDs = []string{"abc"} }},
		"fingerprint":  {ProtoVLESS, func(s *Spec) { s.Reality.Fingerprint = "netscape" }},
		"client ver":   {ProtoVLESS, func(s *Spec) { s.Reality.MinClientVer = "26.3" }},
		"trojan ws":    {ProtoTrojan, func(s *Spec) {}},
	} {
		s := realitySpec()
		c.mutate(s)
		if err := s.Validate(c.proto); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}

	ts := tlsSpec(NetworkWS)
	ts.TLS.CertMode = CertFile
	ts.TLS.CertFile = "relative.pem"
	if err := ts.Validate(ProtoVLESS); err == nil {
		t.Error("relative cert path must be rejected")
	}
	ss := ssSpec()
	ss.SS.Method = "2022-blake3-chacha20-poly1305"
	if err := ss.Validate(ProtoSS); err == nil {
		t.Error("chacha20 must be rejected for multi-user SS2022")
	}
}

func TestPublicKeyMatchesXray(t *testing.T) {
	// 由 xray v26.7.28 `xray x25519` 生成的真实密钥对。
	pub, err := PublicKeyFromPrivate("4BJUpdq1rb4rOXwSakYbVWAgyLuONyoo0a67I3BL9VY")
	if err != nil {
		t.Fatal(err)
	}
	if pub != "FVtmJ1_g7tk1aHNX0EPX-hBRNKrW9IOA0EEWhcwiKFE" {
		t.Fatalf("public key mismatch: %s", pub)
	}
}

func TestCredentials(t *testing.T) {
	ss := ssSpec()
	ss.Validate(ProtoSS)
	a := CredentialFor(ProtoSS, ss, "u", "secret", 1)
	if a != CredentialFor(ProtoSS, ss, "u", "secret", 1) {
		t.Fatal("derivation must be deterministic")
	}
	if a == CredentialFor(ProtoSS, ss, "u", "secret", 2) || a == CredentialFor(ProtoSS, ss, "u", "other", 1) {
		t.Fatal("credentials must differ per inbound and per secret")
	}
	if k, _ := base64.StdEncoding.DecodeString(a.Password); len(k) != 16 {
		t.Fatalf("aes-128 key must be 16 bytes, got %d", len(k))
	}
	if c := CredentialFor(ProtoTrojan, nil, "u", "secret", 1); len(c.Password) != 32 || c.UUID != "" {
		t.Fatalf("trojan credential: %+v", c)
	}
	if c := CredentialFor(ProtoVMess, nil, "u", "secret", 1); c.UUID != "u" {
		t.Fatalf("vmess credential: %+v", c)
	}
}

func TestXrayInbound(t *testing.T) {
	s := realitySpec()
	s.Validate(ProtoVLESS)
	b, _ := json.Marshal(XrayInbound(7, "", 443, ProtoVLESS, s, []Client{{Email: ClientEmail(1, 7), Cred: Credential{UUID: "u-1"}}}))
	for _, want := range []string{`"tag":"in-7"`, `"email":"s1.i7"`, `"flow":"xtls-rprx-vision"`, `"target":"dl.google.com:443"`, `"routeOnly":true`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("reality inbound missing %s: %s", want, b)
		}
	}

	ws := tlsSpec(NetworkWS)
	ws.Validate(ProtoVMess)
	b, _ = json.Marshal(XrayInbound(8, "", 443, ProtoVMess, ws, []Client{{Email: "s1.i8", Cred: Credential{UUID: "u-1"}}}))
	for _, want := range []string{`"certificateFile":"certs/cdn.example.com/fullchain.pem"`, `"wsSettings":{"path":"/ray"}`, `"protocol":"vmess"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("vmess inbound missing %s: %s", want, b)
		}
	}
	if ws.ACMEDomain() != "cdn.example.com" {
		t.Error("acme domain not reported")
	}

	ss := ssSpec()
	ss.Validate(ProtoSS)
	b, _ = json.Marshal(XrayInbound(9, "", 8388, ProtoSS, ss, nil))
	if !strings.Contains(string(b), ssPlaceholderEmail) || !strings.Contains(string(b), `"method":"2022-blake3-aes-128-gcm"`) {
		t.Errorf("ss inbound without subscriptions must keep a placeholder user: %s", b)
	}
	b2, _ := json.Marshal(XrayInbound(9, "", 8388, ProtoSS, ss, nil))
	if string(b) != string(b2) {
		t.Error("placeholder must be deterministic so the config hash is stable")
	}
}

func TestOutputs(t *testing.T) {
	target := LinkTarget{Host: "2001:db8::1", Port: 443, Name: "东京 01"}

	s := realitySpec()
	s.Validate(ProtoVLESS)
	u, _ := url.Parse(ShareLink(ProtoVLESS, s, target, Credential{UUID: "u-1"}))
	q := u.Query()
	if u.Scheme != "vless" || u.User.Username() != "u-1" || u.Host != "[2001:db8::1]:443" || u.Fragment != "东京 01" ||
		q.Get("pbk") != s.Reality.PublicKey || q.Get("sid") != "ab12" || q.Get("flow") != FlowVision || q.Get("sni") != "dl.google.com" {
		t.Fatalf("vless reality link: %s", u)
	}

	x := realitySpec()
	x.Network, x.VLESS.Flow, x.XHTTP = NetworkXHTTP, "", &XHTTP{Path: "/x"}
	if err := x.Validate(ProtoVLESS); err != nil {
		t.Fatal(err)
	}
	u, _ = url.Parse(ShareLink(ProtoVLESS, x, target, Credential{UUID: "u-1"}))
	if q = u.Query(); q.Get("type") != "xhttp" || q.Get("path") != "/x" || q.Get("mode") != "auto" || q.Has("flow") {
		t.Fatalf("xhttp link: %s", u)
	}
	if _, ok := SingBox(ProtoVLESS, x, target, Credential{UUID: "u-1"}); ok {
		t.Error("sing-box does not support xhttp")
	}

	ws := tlsSpec(NetworkWS)
	ws.Validate(ProtoVMess)
	raw, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(ShareLink(ProtoVMess, ws, target, Credential{UUID: "u-1"}), "vmess://"))
	var vm map[string]string
	json.Unmarshal(raw, &vm)
	if vm["net"] != "ws" || vm["host"] != "cdn.example.com" || vm["path"] != "/ray" || vm["tls"] != "tls" || vm["alpn"] != "h2,http/1.1" {
		t.Fatalf("vmess link: %v", vm)
	}

	tr := tlsSpec(NetworkTCP)
	tr.WS = nil
	tr.Validate(ProtoTrojan)
	if l := ShareLink(ProtoTrojan, tr, target, Credential{Password: "p"}); !strings.HasPrefix(l, "trojan://p@[2001:db8::1]:443?") {
		t.Fatalf("trojan link: %s", l)
	}
	if m := Mihomo(ProtoTrojan, tr, target, Credential{Password: "p"}); m.SNI != "cdn.example.com" || m.TLS {
		t.Fatalf("mihomo trojan uses sni: %+v", m)
	}

	ss := ssSpec()
	ss.Validate(ProtoSS)
	l := ShareLink(ProtoSS, ss, target, Credential{Password: "user"})
	userinfo, _ := base64.RawURLEncoding.DecodeString(strings.SplitN(strings.TrimPrefix(l, "ss://"), "@", 2)[0])
	if string(userinfo) != "2022-blake3-aes-128-gcm:"+ss.SS.ServerKey+":user" {
		t.Fatalf("ss link userinfo: %s", userinfo)
	}
	if m := Mihomo(ProtoSS, ss, target, Credential{Password: "user"}); m.Type != "ss" || m.Password != ss.SS.ServerKey+":user" {
		t.Fatalf("mihomo ss: %+v", m)
	}
}
