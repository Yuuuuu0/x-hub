package routing

import (
	"strings"
	"testing"
)

// TestRenderShadowrocket 覆盖分组输出、规则转换、跳过不支持的规则与 FINAL。
func TestRenderShadowrocket(t *testing.T) {
	p := &Profile{
		Groups: []Group{
			{Name: "代理", Type: GroupSelect, Proxies: []string{"自动", "DIRECT"}, Nodes: &NodeFilter{Source: SourceAll}},
			{Name: "自动", Type: GroupURLTest, Nodes: &NodeFilter{Include: "美国", Source: SourceAll}, URL: "http://cp.cloudflare.com", Interval: 300, Tolerance: 50},
			{Name: "广告", Type: GroupSelect, Proxies: []string{"REJECT"}},
		},
		Rules: []Rule{
			{Type: "RULE-SET", Value: "Ads", Target: "广告"},
			{Type: "DOMAIN-SUFFIX", Value: "example.com", Target: "代理"},
			{Type: "GEOSITE", Value: "cn", Target: "DIRECT"},
			{Type: "DST-PORT", Value: "6881-6889", Target: "REJECT"},
			{Type: RuleRaw, Raw: "AND,((NETWORK,udp)),REJECT"},
			{Type: "GEOIP", Value: "LAN", Target: "DIRECT", NoResolve: true},
			{Type: "GEOIP", Value: "CN", Target: "DIRECT"},
			{Type: "MATCH", Target: "代理"},
		},
	}
	nodes := []Node{{Name: "美国 1", Own: true}, {Name: "a,b"}}
	sets := map[string]RuleSetRef{"Ads": {Name: "Ads", ShadowrocketURL: "https://p/rules/t/Ads.list"}}
	out, warnings, err := p.RenderShadowrocket(nodes, sets)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{
		"[General]\n",
		"\n[Proxy Group]\n代理 = select,自动,DIRECT,美国 1\n自动 = url-test,美国 1,url=http://cp.cloudflare.com,interval=300,tolerance=50\n广告 = select,REJECT\n",
		"\n[Rule]\nRULE-SET,https://p/rules/t/Ads.list,广告\nDOMAIN-SUFFIX,example.com,代理\nIP-CIDR,10.0.0.0/8,DIRECT,no-resolve\n",
		"IP-CIDR6,fe80::/10,DIRECT,no-resolve\nGEOIP,CN,DIRECT\nFINAL,代理\n",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in:\n%s", want, s)
		}
	}
	if strings.Contains(s, "a,b") || strings.Contains(s, "GEOSITE") || strings.Contains(s, "6881") {
		t.Fatalf("unsupported content leaked:\n%s", s)
	}
	if len(warnings) != 4 {
		t.Fatalf("warnings: %v", warnings)
	}
}

// TestShadowrocketRuleSet 覆盖三种行为的规则集转换。
func TestShadowrocketRuleSet(t *testing.T) {
	out, skipped := ShadowrocketRuleSet("domain", []string{"+.google.com", "'.youtube.com'", "*.cdn.com", "a.com"})
	if string(out) != "DOMAIN-SUFFIX,google.com\nDOMAIN-SUFFIX,youtube.com\nDOMAIN-WILDCARD,*.cdn.com\nDOMAIN,a.com\n" || skipped != 0 {
		t.Fatalf("domain: %q", out)
	}
	out, _ = ShadowrocketRuleSet("ipcidr", []string{"1.0.0.0/8", "2001:db8::/32"})
	if string(out) != "IP-CIDR,1.0.0.0/8\nIP-CIDR6,2001:db8::/32\n" {
		t.Fatalf("ipcidr: %q", out)
	}
	out, skipped = ShadowrocketRuleSet("classical", []string{"DOMAIN-SUFFIX,a.com", "IP-CIDR,1.1.1.1/32,no-resolve", "PROCESS-NAME,curl", "GEOIP,CN"})
	if string(out) != "DOMAIN-SUFFIX,a.com\nIP-CIDR,1.1.1.1/32,no-resolve\n" || skipped != 2 {
		t.Fatalf("classical: %q %d", out, skipped)
	}
}
