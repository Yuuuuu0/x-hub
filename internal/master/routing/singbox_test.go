package routing

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestRenderSingBox 覆盖分组转换、拒绝分组、规则转换、resolve 插入、前置与规则集定义。
func TestRenderSingBox(t *testing.T) {
	p := &Profile{
		Groups: []Group{
			{Name: "代理", Type: GroupSelect, Proxies: []string{"自动", "DIRECT", "REJECT"}, Nodes: &NodeFilter{Source: SourceAll}},
			{Name: "自动", Type: GroupFallback, Nodes: &NodeFilter{Include: "美国", Source: SourceAll}, URL: "https://cp.cloudflare.com", Interval: 300},
			{Name: "广告", Type: GroupSelect, Proxies: []string{"REJECT"}},
		},
		Rules: []Rule{
			{Type: "RULE-SET", Value: "Ads", Target: "广告"},
			{Type: "DOMAIN-SUFFIX", Value: "example.com", Target: "代理"},
			{Type: "DST-PORT", Value: "6881-6889", Target: "REJECT-DROP"},
			{Type: "IP-ASN", Value: "13335", Target: "代理"},
			{Type: RuleRaw, Raw: "AND,((NETWORK,udp)),REJECT"},
			{Type: "GEOIP", Value: "LAN", Target: "DIRECT", NoResolve: true},
			{Type: "GEOIP", Value: "CN", Target: "DIRECT"},
			{Type: "MATCH", Target: "代理"},
		},
	}
	nodes := []Node{
		{Name: "美国 1", Own: true, SingBox: map[string]any{"type": "vless", "tag": "美国 1"}},
		{Name: "XHTTP", Own: true},
		{Name: "住宅", SingBox: map[string]any{"type": "socks"}, Dialer: "美国 1", DialerNode: true},
	}
	sets := map[string]RuleSetRef{"Ads": {Name: "Ads", Behavior: "domain", Interval: 86400, SingBoxURL: "https://p/rules/t/Ads.json"}}
	out, warnings, err := p.RenderSingBox(nodes, sets, "https://p/rules/t/geo/")
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Outbounds []map[string]any `json:"outbounds"`
		Route     struct {
			Rules   []map[string]any `json:"rules"`
			RuleSet []map[string]any `json:"rule_set"`
			Final   string           `json:"final"`
		} `json:"route"`
		DNS map[string]any `json:"dns"`
	}
	if err := json.Unmarshal(out, &cfg); err != nil {
		t.Fatal(err)
	}
	tags := map[string]map[string]any{}
	for _, o := range cfg.Outbounds {
		tags[o["tag"].(string)] = o
	}
	if tags["广告"] != nil || tags["XHTTP"] != nil || tags["自动"]["type"] != "urltest" || tags["DIRECT"]["type"] != "direct" {
		t.Fatalf("outbounds: %v", cfg.Outbounds)
	}
	if m := tags["代理"]["outbounds"].([]any); len(m) != 4 || m[0] != "自动" || m[2] != "美国 1" || m[3] != "住宅" {
		t.Fatalf("selector members: %v", m)
	}
	if tags["住宅"]["detour"] != "美国 1" {
		t.Fatalf("detour: %v", tags["住宅"])
	}
	// 基础配置的 sniff / hijack-dns 在前；Ads → reject；端口范围 → drop；首个需要 IP 的规则前插入 resolve。
	rules := cfg.Route.Rules
	if len(rules) != 8 || rules[0]["action"] != "sniff" || rules[2]["action"] != "reject" || rules[3]["outbound"] != "代理" ||
		rules[4]["method"] != "drop" || rules[5]["ip_is_private"] != true || rules[6]["action"] != "resolve" || rules[7]["rule_set"] == nil {
		b, _ := json.MarshalIndent(rules, "", " ")
		t.Fatalf("rules: %s", b)
	}
	if cfg.Route.Final != "代理" || len(cfg.Route.RuleSet) != 3 {
		t.Fatalf("final / rule_set: %s %v", cfg.Route.Final, cfg.Route.RuleSet)
	}
	for _, rs := range cfg.Route.RuleSet {
		if rs["download_detour"] != "DIRECT" || (rs["tag"] != "Ads" && rs["url"] != "https://p/rules/t/geo/"+rs["tag"].(string)+".srs") {
			t.Fatalf("rule set via master: %v", rs)
		}
	}
	joined := strings.Join(warnings, "\n")
	for _, w := range []string{"XHTTP", "IP-ASN", "RAW", "fallback"} {
		if !strings.Contains(joined, w) {
			t.Errorf("missing warning about %s: %s", w, joined)
		}
	}
}

// TestSingBoxRuleSet 覆盖三种行为的规则集转换。
func TestSingBoxRuleSet(t *testing.T) {
	cases := []struct {
		behavior string
		lines    []string
		want     string
		skipped  int
	}{
		{"domain", []string{"+.google.com", "'.youtube.com'", "*.cdn.com", "a.com"},
			`{"rules":[{"domain_suffix":["google.com",".youtube.com"]},{"domain_regex":["^[^.]+\\.cdn\\.com$"]},{"domain":["a.com"]}],"version":2}`, 0},
		{"ipcidr", []string{"1.0.0.0/8"}, `{"rules":[{"ip_cidr":["1.0.0.0/8"]}],"version":2}`, 0},
		{"classical", []string{"DOMAIN-SUFFIX,a.com", "IP-CIDR,1.1.1.1/32,no-resolve", "DST-PORT,443", "GEOIP,CN", "PROCESS-NAME,curl"},
			`{"rules":[{"domain_suffix":["a.com"]},{"ip_cidr":["1.1.1.1/32"]},{"port":[443]},{"process_name":["curl"]}],"version":2}`, 1},
	}
	for _, c := range cases {
		out, skipped := SingBoxRuleSet(c.behavior, c.lines)
		var a, b any
		json.Unmarshal(out, &a)
		json.Unmarshal([]byte(c.want), &b)
		ja, _ := json.Marshal(a)
		jb, _ := json.Marshal(b)
		if string(ja) != string(jb) || skipped != c.skipped {
			t.Errorf("%s: got %s (%d skipped), want %s", c.behavior, ja, skipped, jb)
		}
	}
	lines, err := ParseMihomoRuleSet([]byte("payload:\n  - DOMAIN,a.com\n"), "")
	if err != nil || len(lines) != 1 {
		t.Fatalf("yaml: %v %v", lines, err)
	}
	if _, err := ParseMihomoRuleSet(nil, "mrs"); err == nil {
		t.Fatal("mrs must be rejected")
	}
}
