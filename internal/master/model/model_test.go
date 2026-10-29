package model

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

func validSpec() *Spec {
	priv, _ := GenerateX25519()
	return &Spec{
		Network: NetworkTCP, Security: SecurityReality, VLESS: &VLESS{Flow: FlowVision}, Sniffing: true,
		Reality: &Reality{Target: "www.microsoft.com:443", ServerNames: []string{" www.microsoft.com ", ""},
			PrivateKey: priv, ShortIDs: []string{"AB12"}, SpiderX: ""},
	}
}

func TestValidateNormalizes(t *testing.T) {
	s := validSpec()
	if err := s.Validate(ProtoVLESS); err != nil {
		t.Fatal(err)
	}
	r := s.Reality
	if r.PublicKey == "" || r.ServerNames[0] != "www.microsoft.com" || len(r.ServerNames) != 1 ||
		r.ShortIDs[0] != "ab12" || r.Fingerprint != "chrome" || r.SpiderX != "/" {
		t.Fatalf("not normalized: %+v", r)
	}
	for name, mutate := range map[string]func(*Spec){
		"protocol":    func(s *Spec) { s.Network = "ws" },
		"flow":        func(s *Spec) { s.VLESS.Flow = "xtls-rprx-direct" },
		"target":      func(s *Spec) { s.Reality.Target = "www.microsoft.com" },
		"sni":         func(s *Spec) { s.Reality.ServerNames = nil },
		"private key": func(s *Spec) { s.Reality.PrivateKey = "bad" },
		"short id":    func(s *Spec) { s.Reality.ShortIDs = []string{"abc"} },
		"fingerprint": func(s *Spec) { s.Reality.Fingerprint = "netscape" },
		"client ver":  func(s *Spec) { s.Reality.MinClientVer = "26.3" },
		"client ver2": func(s *Spec) { s.Reality.MinClientVer = "1.256.0" },
	} {
		s := validSpec()
		mutate(s)
		if err := s.Validate(ProtoVLESS); err == nil {
			t.Errorf("%s: expected error", name)
		}
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

func TestXrayInboundAndLink(t *testing.T) {
	s := validSpec()
	s.Validate(ProtoVLESS)
	in := XrayInbound(7, "", 443, s, []Client{{Email: ClientEmail(1, 7), UUID: "u-1"}})
	b, _ := json.Marshal(in)
	for _, want := range []string{`"tag":"in-7"`, `"email":"s1.i7"`, `"flow":"xtls-rprx-vision"`, `"target":"www.microsoft.com:443"`, `"routeOnly":true`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("inbound missing %s: %s", want, b)
		}
	}

	if strings.Contains(string(b), "minClientVer") {
		t.Errorf("minClientVer must be omitted when empty")
	}
	s.Reality.MinClientVer = "v1.8.0"
	if err := s.Validate(ProtoVLESS); err != nil || s.Reality.MinClientVer != "1.8.0" {
		t.Fatalf("minClientVer normalize: %v %s", err, s.Reality.MinClientVer)
	}
	b, _ = json.Marshal(XrayInbound(7, "", 443, s, nil))
	if !strings.Contains(string(b), `"minClientVer":"1.8.0"`) {
		t.Errorf("minClientVer missing: %s", b)
	}

	link := ShareLink(s, LinkTarget{Host: "2001:db8::1", Port: 443, Name: "东京 01"}, "u-1")
	u, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if u.Scheme != "vless" || u.User.Username() != "u-1" || u.Host != "[2001:db8::1]:443" || u.Fragment != "东京 01" {
		t.Fatalf("bad link: %s", link)
	}
	if q.Get("pbk") != s.Reality.PublicKey || q.Get("sid") != "ab12" || q.Get("flow") != FlowVision || q.Get("sni") != "www.microsoft.com" {
		t.Fatalf("bad query: %s", link)
	}
}
