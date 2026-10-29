// Package routing 实现分流方案：分组、规则与各客户端基础配置的模型、校验，
// 以及把同一份方案渲染为各客户端配置、从现有 Mihomo 配置导入。
package routing

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// 分组类型。
const (
	GroupSelect      = "select"
	GroupURLTest     = "url-test"
	GroupFallback    = "fallback"
	GroupLoadBalance = "load-balance"
)

var groupTypes = []string{GroupSelect, GroupURLTest, GroupFallback, GroupLoadBalance}

// 节点来源。
const (
	SourceAll      = "all"
	SourceOwn      = "own"      // x-hub 管理的服务器
	SourceExternal = "external" // 外部节点（阶段二）
)

// Builtins 为可作为分组成员与规则目标的内置出站。
var Builtins = []string{"DIRECT", "REJECT", "REJECT-DROP", "PASS"}

// RuleRaw 为以原文保存的规则类型（Mihomo 的 AND / OR / NOT 等逻辑规则），只用于 Mihomo 输出。
const RuleRaw = "RAW"

// ruleTypes 为支持的规则类型。
var ruleTypes = []string{
	"RULE-SET", "DOMAIN", "DOMAIN-SUFFIX", "DOMAIN-KEYWORD", "DOMAIN-REGEX", "GEOSITE", "GEOIP",
	"IP-CIDR", "IP-CIDR6", "IP-ASN", "SRC-IP-CIDR", "DST-PORT", "SRC-PORT", "PROCESS-NAME", "PROCESS-PATH",
	"NETWORK", "MATCH", RuleRaw,
}

// noResolveTypes 为支持 no-resolve 选项的规则类型。
var noResolveTypes = []string{"RULE-SET", "GEOIP", "IP-CIDR", "IP-CIDR6", "IP-ASN"}

// Profile 为一个分流方案。
type Profile struct {
	Groups       []Group    `json:"groups"`
	Rules        []Rule     `json:"rules"`
	Mihomo       ClientBase `json:"mihomo"`
	SingBox      ClientBase `json:"singbox"`
	Shadowrocket ClientBase `json:"shadowrocket"`
}

// ClientBase 为某客户端的基础配置原文（DNS、TUN、嗅探、端口等），不含节点、分组、规则集与规则。
// sing-box 与 Shadowrocket 的基础配置为空时分别使用 DefaultSingBoxBase 与 DefaultShadowrocketBase。
type ClientBase struct {
	Base string `json:"base"`
}

// Group 为一个代理分组。成员依次为固定成员与按筛选条件展开的节点。
type Group struct {
	Name      string      `json:"name"`
	Type      string      `json:"type"`
	Proxies   []string    `json:"proxies"`         // 固定成员：其他分组或内置出站
	Nodes     *NodeFilter `json:"nodes,omitempty"` // 为空表示不包含节点
	URL       string      `json:"url,omitempty"`
	Interval  int         `json:"interval,omitempty"`
	Tolerance int         `json:"tolerance,omitempty"`
	Strategy  string      `json:"strategy,omitempty"` // load-balance 策略
}

// NodeFilter 为按名称筛选节点的条件，正则为空表示不限。
type NodeFilter struct {
	Include string `json:"include"`
	Exclude string `json:"exclude"`
	Source  string `json:"source"`
}

// Rule 为一条分流规则。Type 为 RAW 时 Raw 为完整原文。
type Rule struct {
	Type      string `json:"type"`
	Value     string `json:"value,omitempty"`
	Target    string `json:"target,omitempty"`
	NoResolve bool   `json:"noResolve,omitempty"`
	Raw       string `json:"raw,omitempty"`
}

// managedMihomoKeys 为由方案生成、不允许出现在 Mihomo 基础配置中的键。
var managedMihomoKeys = []string{"proxies", "proxy-groups", "proxy-providers", "rule-providers", "rules"}

// Validate 校验方案并规范化字段，ruleSets 为规则集库中的名称。返回发现的全部问题。
func (p *Profile) Validate(ruleSets map[string]bool) []string {
	var issues []string
	add := func(format string, args ...any) { issues = append(issues, fmt.Sprintf(format, args...)) }

	names := map[string]bool{}
	for i := range p.Groups {
		g := &p.Groups[i]
		g.Name = strings.TrimSpace(g.Name)
		switch {
		case g.Name == "":
			add("第 %d 个分组缺少名称", i+1)
		case slices.Contains(Builtins, g.Name):
			add("分组名称不能使用内置出站名 %s", g.Name)
		case names[g.Name]:
			add("分组名称重复：%s", g.Name)
		}
		names[g.Name] = true
		if g.Type == "" {
			g.Type = GroupSelect
		}
		if !slices.Contains(groupTypes, g.Type) {
			add("分组 %s 的类型无效：%s", g.Name, g.Type)
		}
		if g.Nodes != nil {
			if g.Nodes.Source == "" {
				g.Nodes.Source = SourceAll
			}
			if !slices.Contains([]string{SourceAll, SourceOwn, SourceExternal}, g.Nodes.Source) {
				add("分组 %s 的节点来源无效：%s", g.Name, g.Nodes.Source)
			}
			for _, re := range []string{g.Nodes.Include, g.Nodes.Exclude} {
				if _, err := regexp.Compile(re); err != nil {
					add("分组 %s 的正则无效：%s", g.Name, re)
				}
			}
		}
		if len(g.Proxies) == 0 && g.Nodes == nil {
			add("分组 %s 没有任何成员", g.Name)
		}
		if g.Type != GroupSelect {
			if g.URL == "" {
				g.URL = "https://www.gstatic.com/generate_204"
			}
			if g.Interval <= 0 {
				g.Interval = 300
			}
		}
	}
	for _, g := range p.Groups {
		for _, m := range g.Proxies {
			switch {
			case m == g.Name:
				add("分组 %s 不能包含自身", g.Name)
			case !names[m] && !slices.Contains(Builtins, m):
				add("分组 %s 引用了不存在的成员：%s", g.Name, m)
			}
		}
	}
	if cyc := p.findCycle(); cyc != "" {
		add("分组之间存在循环引用：%s", cyc)
	}

	for i := range p.Rules {
		r := &p.Rules[i]
		r.Type = strings.ToUpper(strings.TrimSpace(r.Type))
		r.Value, r.Target, r.Raw = strings.TrimSpace(r.Value), strings.TrimSpace(r.Target), strings.TrimSpace(r.Raw)
		n := i + 1
		if !slices.Contains(ruleTypes, r.Type) {
			add("第 %d 条规则的类型无效：%s", n, r.Type)
			continue
		}
		if r.Type == RuleRaw {
			if r.Raw == "" {
				add("第 %d 条规则为空", n)
			}
			continue
		}
		if r.Type == "MATCH" {
			r.Value = ""
			if i != len(p.Rules)-1 {
				add("MATCH 规则必须位于最后")
			}
		} else if r.Value == "" {
			add("第 %d 条规则缺少匹配内容", n)
		}
		if r.Type == "RULE-SET" && !ruleSets[r.Value] {
			add("第 %d 条规则引用了不存在的规则集：%s", n, r.Value)
		}
		if !names[r.Target] && !slices.Contains(Builtins, r.Target) {
			add("第 %d 条规则的目标不存在：%s", n, r.Target)
		}
		if r.NoResolve && !slices.Contains(noResolveTypes, r.Type) {
			r.NoResolve = false
		}
	}

	if strings.TrimSpace(p.Mihomo.Base) != "" {
		var base map[string]any
		if err := yaml.Unmarshal([]byte(p.Mihomo.Base), &base); err != nil {
			add("Mihomo 基础配置不是合法的 YAML：%v", err)
		}
		for _, k := range managedMihomoKeys {
			if _, ok := base[k]; ok {
				add("Mihomo 基础配置中不应包含 %s，这部分由方案的分组、规则集与规则生成", k)
			}
		}
	}
	if strings.TrimSpace(p.SingBox.Base) != "" {
		var base map[string]any
		if err := json.Unmarshal([]byte(p.SingBox.Base), &base); err != nil {
			add("sing-box 基础配置不是合法的 JSON：%v", err)
		}
		if _, ok := base["outbounds"]; ok {
			add("sing-box 基础配置中不应包含 outbounds，这部分由方案的分组与节点生成")
		}
		if route, ok := base["route"].(map[string]any); ok {
			for _, k := range []string{"rule_set", "final"} {
				if _, ok := route[k]; ok {
					add("sing-box 基础配置的 route 中不应包含 %s，这部分由方案生成", k)
				}
			}
		}
	}
	for _, line := range strings.Split(p.Shadowrocket.Base, "\n") {
		for _, sec := range managedShadowrocketSections {
			if strings.EqualFold(strings.TrimSpace(line), sec) {
				add("Shadowrocket 基础配置中不应包含 %s，这部分由方案生成", sec)
			}
		}
	}
	return issues
}

// findCycle 返回分组循环引用的路径，无循环时返回空。
func (p *Profile) findCycle() string {
	members := map[string][]string{}
	for _, g := range p.Groups {
		members[g.Name] = g.Proxies
	}
	state := map[string]int{} // 0 未访问，1 访问中，2 已完成
	var path []string
	var visit func(string) bool
	visit = func(n string) bool {
		switch state[n] {
		case 1:
			path = append(path, n)
			return true
		case 2:
			return false
		}
		state[n] = 1
		path = append(path, n)
		for _, m := range members[n] {
			if _, isGroup := members[m]; isGroup && visit(m) {
				return true
			}
		}
		path = path[:len(path)-1]
		state[n] = 2
		return false
	}
	for _, g := range p.Groups {
		if visit(g.Name) {
			return strings.Join(path, " → ")
		}
	}
	return ""
}

// RuleSetNames 返回方案中引用的规则集名称（按首次出现顺序）。
func (p *Profile) RuleSetNames() []string {
	var out []string
	for _, r := range p.Rules {
		if r.Type == "RULE-SET" && !slices.Contains(out, r.Value) {
			out = append(out, r.Value)
		}
	}
	return out
}

// RenameRuleSet 把引用旧规则集名称的规则改为新名称，返回是否有修改。
func (p *Profile) RenameRuleSet(oldName, newName string) bool {
	changed := false
	for i := range p.Rules {
		if p.Rules[i].Type == "RULE-SET" && p.Rules[i].Value == oldName {
			p.Rules[i].Value, changed = newName, true
		}
	}
	return changed
}

// DefaultProfile 返回新建方案的起点：手动 / 自动选择两个分组，局域网与国内直连。
func DefaultProfile() *Profile {
	return &Profile{
		Groups: []Group{
			{Name: "节点选择", Type: GroupSelect, Proxies: []string{"自动选择", "DIRECT"}, Nodes: &NodeFilter{Source: SourceAll}},
			{Name: "自动选择", Type: GroupURLTest, Nodes: &NodeFilter{Source: SourceAll}, URL: "https://www.gstatic.com/generate_204", Interval: 300, Tolerance: 50},
		},
		Rules: []Rule{
			{Type: "GEOIP", Value: "LAN", Target: "DIRECT", NoResolve: true},
			{Type: "GEOSITE", Value: "CN", Target: "DIRECT"},
			{Type: "GEOIP", Value: "CN", Target: "DIRECT"},
			{Type: "MATCH", Target: "节点选择"},
		},
		Mihomo:       ClientBase{Base: defaultMihomoBase},
		SingBox:      ClientBase{Base: DefaultSingBoxBase},
		Shadowrocket: ClientBase{Base: DefaultShadowrocketBase},
	}
}

// BuiltinProfile 返回未选择方案的订阅使用的内置精简方案：与默认方案相同，基础配置只含端口与模式。
func BuiltinProfile() *Profile {
	p := DefaultProfile()
	p.Mihomo.Base = "mixed-port: 7890\nallow-lan: false\nmode: rule\nlog-level: info\n"
	return p
}

const defaultMihomoBase = `mixed-port: 7890
allow-lan: false
mode: rule
log-level: info
unified-delay: true
tcp-concurrent: true

dns:
  enable: true
  enhanced-mode: fake-ip
  fake-ip-range: 198.18.0.1/16
  default-nameserver:
    - 223.5.5.5
  nameserver-policy:
    "geosite:cn,private":
      - https://223.5.5.5/dns-query
  nameserver:
    - https://1.1.1.1/dns-query
    - https://8.8.8.8/dns-query
`
