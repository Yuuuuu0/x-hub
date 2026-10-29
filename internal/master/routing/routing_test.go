package routing

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const sample = `mode: rule
mixed-port: 7897
# 嗅探设置
sniffer:
  enable: true
dns:
  enable: true
  enhanced-mode: fake-ip
proxies:
  - {name: 旧节点, type: ss, server: 1.1.1.1, port: 1, cipher: aes-128-gcm, password: x}
proxy-providers:
  p1:
    type: http
    url: https://example.com/sub
proxy-groups:
  - name: 日常
    type: select
    proxies: [香港, 美国, 旧节点, DIRECT]
  - name: 香港
    type: select
    use: [p1]
    filter: (?i)香港
  - name: 美国
    type: url-test
    use: [p1]
    filter: (?i)美国
    exclude-filter: 家宽
  - name: 广告
    type: select
    proxies: [REJECT]
rule-providers:
  MyReject:
    type: http
    behavior: domain
    url: https://example.com/reject.txt
    path: ./a.yaml
    interval: 3600
  Inline:
    type: inline
    behavior: classical
    payload:
      - DOMAIN-SUFFIX,example.org
rules:
  - RULE-SET,MyReject,广告
  - RULE-SET,Inline,日常,no-resolve
  - AND,((DOMAIN,a.com),(NETWORK,UDP)),REJECT
  - GEOIP,CN,DIRECT
  - MATCH,日常
`

func TestImportAndRender(t *testing.T) {
	res, err := ImportMihomo(sample)
	if err != nil {
		t.Fatal(err)
	}
	p := res.Profile
	if len(p.Groups) != 4 || p.Groups[1].Nodes == nil || p.Groups[1].Nodes.Include != "(?i)香港" ||
		p.Groups[2].Nodes.Exclude != "家宽" || p.Groups[0].Nodes != nil {
		t.Fatalf("groups: %+v", p.Groups)
	}
	if strings.Join(p.Groups[0].Proxies, ",") != "香港,美国,DIRECT" {
		t.Fatalf("unknown proxy member should be dropped: %v", p.Groups[0].Proxies)
	}
	if len(res.Sets) != 2 || res.Sets[0].Source != SetURL || res.Sets[1].Source != SetHosted || res.Sets[1].Content != "DOMAIN-SUFFIX,example.org" {
		t.Fatalf("sets: %+v", res.Sets)
	}
	if len(p.Rules) != 5 || p.Rules[2].Type != RuleRaw || !p.Rules[1].NoResolve || p.Rules[4].Type != "MATCH" {
		t.Fatalf("rules: %+v", p.Rules)
	}
	if strings.Contains(p.Mihomo.Base, "proxy-providers") || !strings.Contains(p.Mihomo.Base, "# 嗅探设置") {
		t.Fatalf("base should keep comments and drop managed keys:\n%s", p.Mihomo.Base)
	}
	if issues := p.Validate(map[string]bool{"MyReject": true, "Inline": true}); len(issues) != 0 {
		t.Fatalf("imported profile should validate: %v", issues)
	}

	nodes := []Node{
		{Name: "香港-01", Own: true, Mihomo: map[string]any{"name": "香港-01", "type": "vless"}},
		{Name: "美国-01", Own: true, Mihomo: map[string]any{"name": "美国-01", "type": "vless"}},
		{Name: "美国家宽", Own: true, Mihomo: map[string]any{"name": "美国家宽", "type": "vless"}},
	}
	sets := map[string]RuleSetRef{
		"MyReject": {Name: "MyReject", Behavior: "domain", URL: "https://example.com/reject.txt", Interval: 3600},
		"Inline":   {Name: "Inline", Behavior: "classical", URL: "https://hub/rules/t/Inline.yaml", Interval: 86400},
	}
	out, warnings, err := p.RenderMihomo(nodes, sets)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		MixedPort int `yaml:"mixed-port"`
		Proxies   []map[string]any
		Groups    []struct {
			Name    string
			Proxies []string
		} `yaml:"proxy-groups"`
		Providers map[string]map[string]any `yaml:"rule-providers"`
		Rules     []string
	}
	if err := yaml.Unmarshal(out, &cfg); err != nil {
		t.Fatalf("output is not valid yaml: %v\n%s", err, out)
	}
	if cfg.MixedPort != 7897 || len(cfg.Proxies) != 3 {
		t.Fatalf("base / proxies: %+v", cfg)
	}
	got := map[string]string{}
	for _, g := range cfg.Groups {
		got[g.Name] = strings.Join(g.Proxies, ",")
	}
	if got["香港"] != "香港-01" || got["美国"] != "美国-01" || got["广告"] != "REJECT" {
		t.Fatalf("expanded groups: %v", got)
	}
	if cfg.Providers["MyReject"]["url"] != "https://example.com/reject.txt" || cfg.Providers["Inline"]["path"] != "./ruleset/Inline.yaml" {
		t.Fatalf("providers: %v", cfg.Providers)
	}
	if cfg.Rules[1] != "RULE-SET,Inline,日常,no-resolve" || cfg.Rules[2] != "AND,((DOMAIN,a.com),(NETWORK,UDP)),REJECT" {
		t.Fatalf("rules: %v", cfg.Rules)
	}
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}

	// 没有节点时分组补 DIRECT 并给出提示。
	out, warnings, _ = p.RenderMihomo(nil, sets)
	if len(warnings) == 0 || !strings.Contains(string(out), "- DIRECT") {
		t.Fatalf("empty groups must fall back to DIRECT: %v", warnings)
	}
}

func TestValidate(t *testing.T) {
	p := DefaultProfile()
	if issues := p.Validate(nil); len(issues) != 0 {
		t.Fatalf("default profile: %v", issues)
	}
	bad := &Profile{
		Groups: []Group{
			{Name: "A", Proxies: []string{"B"}},
			{Name: "B", Proxies: []string{"A"}},
			{Name: "C", Nodes: &NodeFilter{Include: "(?<=x)"}},
		},
		Rules: []Rule{
			{Type: "MATCH", Target: "A"},
			{Type: "RULE-SET", Value: "missing", Target: "nope"},
		},
		Mihomo: ClientBase{Base: "rules: []\n"},
	}
	joined := strings.Join(bad.Validate(map[string]bool{}), "\n")
	for _, want := range []string{"循环引用", "正则无效", "MATCH 规则必须位于最后", "不存在的规则集", "目标不存在", "不应包含 rules"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing issue %q in:\n%s", want, joined)
		}
	}
}

func TestHostedMihomo(t *testing.T) {
	out := string(HostedMihomo("# 注释\n+.a.com\n\n b.com \n"))
	if out != "payload:\n    - +.a.com\n    - b.com\n" {
		t.Fatalf("hosted: %q", out)
	}
}

func TestUnescapeYAML(t *testing.T) {
	// 依次为：emoji 转义需还原；转义的反斜杠后跟 U 不是转义序列，保持原样；控制字符保持转义。
	in := "- \"\\U0001F310 日常\"\n- \"a\\\\U0001F310b\"\n- \"\\u0007\""
	want := "- \"🌐 日常\"\n- \"a\\\\U0001F310b\"\n- \"\\u0007\""
	if got := string(unescapeYAML([]byte(in))); got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}
