package settings

import "testing"

func TestValidateAccess(t *testing.T) {
	ok := Access{Listen: ":2096", PanelPath: "/abcd1234/", PublicURL: "https://hub.example.com:2096/", TLSMode: TLSProxy,
		TrustedProxies: []string{" 173.245.48.0/20 ", "", "127.0.0.1"}}
	if err := ValidateAccess(&ok); err != nil {
		t.Fatal(err)
	}
	if ok.PanelPath != "abcd1234" || ok.PublicURL != "https://hub.example.com:2096" || len(ok.TrustedProxies) != 2 {
		t.Fatalf("normalization failed: %+v", ok)
	}

	bad := []Access{
		{Listen: "2096", PanelPath: "abcd1234", TLSMode: TLSProxy},
		{Listen: ":70000", PanelPath: "abcd1234", TLSMode: TLSProxy},
		{Listen: ":2096", PanelPath: "ab", TLSMode: TLSProxy},
		{Listen: ":2096", PanelPath: "agent", TLSMode: TLSProxy},
		{Listen: ":2096", PanelPath: "abcd1234", PublicURL: "https://hub.example.com/panel", TLSMode: TLSProxy},
		{Listen: ":2096", PanelPath: "abcd1234", TLSMode: TLSFile},
		{Listen: ":2096", PanelPath: "abcd1234", TLSMode: "acme"},
		{Listen: ":2096", PanelPath: "abcd1234", TLSMode: TLSProxy, TrustedProxies: []string{"not-an-ip"}},
	}
	for i, a := range bad {
		if err := ValidateAccess(&a); err == nil {
			t.Errorf("case %d should fail: %+v", i, a)
		}
	}
}
