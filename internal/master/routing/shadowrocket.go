package routing

import (
	"fmt"
	"strings"
)

// lanCIDRs 为 GEOIP,LAN 在 Shadowrocket 中展开的私有地址段。
var lanCIDRs = []string{"10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12", "192.168.0.0/16", "fc00::/7", "fe80::/10"}

// shadowrocketTypes 为 Shadowrocket 支持、可按原样输出的规则类型。
var shadowrocketTypes = []string{"DOMAIN", "DOMAIN-SUFFIX", "DOMAIN-KEYWORD", "IP-CIDR", "IP-CIDR6", "IP-ASN", "GEOIP", "DST-PORT"}

// managedShadowrocketSections 为由方案生成、不允许出现在 Shadowrocket 基础配置中的段落。
var managedShadowrocketSections = []string{"[Proxy Group]", "[Rule]"}

// RenderShadowrocket 把方案渲染为 Shadowrocket 配置：基础配置（[General] 等）在前，随后生成 [Proxy Group] 与 [Rule]。
// 节点不写入配置，分组直接列出节点名称，由同一订阅的通用（Base64）链接提供；nodes 应为通用订阅中的节点。
func (p *Profile) RenderShadowrocket(nodes []Node, sets map[string]RuleSetRef) ([]byte, []string, error) {
	var warnings []string
	warn := func(format string, args ...any) { warnings = append(warnings, fmt.Sprintf(format, args...)) }

	usable := make([]Node, 0, len(nodes))
	for _, n := range nodes {
		if strings.ContainsAny(n.Name, ",=") {
			warn("节点 %s 的名称含有逗号或等号，Shadowrocket 分组无法引用，已跳过", n.Name)
			continue
		}
		usable = append(usable, n)
	}
	usable, prepWarnings := p.prepareNodes(usable)
	warnings = append(warnings, prepWarnings...)
	expanded, expandWarnings := p.ExpandGroups(usable)
	warnings = append(warnings, expandWarnings...)

	base := p.Shadowrocket.Base
	if strings.TrimSpace(base) == "" {
		base = DefaultShadowrocketBase
	}
	var b strings.Builder
	b.WriteString(strings.TrimRight(base, "\n") + "\n\n[Proxy Group]\n")
	for _, g := range p.Groups {
		members := make([]string, 0, len(expanded[g.Name]))
		for _, m := range expanded[g.Name] {
			if m == "PASS" {
				continue
			}
			members = append(members, m)
		}
		if len(members) == 0 {
			members = []string{"DIRECT"}
		}
		line := g.Name + " = " + g.Type + "," + strings.Join(members, ",")
		if g.Type != GroupSelect {
			line += fmt.Sprintf(",url=%s,interval=%d", g.URL, g.Interval)
			if g.Type == GroupURLTest && g.Tolerance > 0 {
				line += fmt.Sprintf(",tolerance=%d", g.Tolerance)
			}
		}
		b.WriteString(line + "\n")
	}

	b.WriteString("\n[Rule]\n")
	final := "DIRECT"
	for i, r := range p.Rules {
		n := i + 1
		if r.Target == "PASS" {
			continue
		}
		switch r.Type {
		case "MATCH":
			final = r.Target
			continue
		case RuleRaw:
			warn("第 %d 条规则为逻辑规则（原文），Shadowrocket 中已跳过", n)
			continue
		case "RULE-SET":
			ref, ok := sets[r.Value]
			if !ok || ref.ShadowrocketURL == "" {
				warn("第 %d 条规则引用的规则集 %s 无法用于 Shadowrocket（mrs 格式不支持转换）", n, r.Value)
				continue
			}
			b.WriteString(withNoResolve("RULE-SET,"+ref.ShadowrocketURL+","+r.Target, r.NoResolve) + "\n")
			continue
		case "GEOIP":
			if strings.EqualFold(r.Value, "LAN") || strings.EqualFold(r.Value, "private") {
				for _, c := range lanCIDRs {
					typ := "IP-CIDR"
					if strings.Contains(c, ":") {
						typ = "IP-CIDR6"
					}
					b.WriteString(typ + "," + c + "," + r.Target + ",no-resolve\n")
				}
				continue
			}
		case "DST-PORT":
			if strings.ContainsAny(r.Value, "-/") {
				warn("第 %d 条规则的端口范围 Shadowrocket 不支持，已跳过", n)
				continue
			}
		}
		if !isShadowrocketType(r.Type) {
			warn("第 %d 条规则的类型 %s Shadowrocket 不支持，已跳过", n, r.Type)
			continue
		}
		b.WriteString(withNoResolve(r.Type+","+r.Value+","+r.Target, r.NoResolve) + "\n")
	}
	b.WriteString("FINAL," + final + "\n")
	return []byte(b.String()), warnings, nil
}

func isShadowrocketType(t string) bool {
	for _, s := range shadowrocketTypes {
		if s == t {
			return true
		}
	}
	return false
}

func withNoResolve(line string, noResolve bool) string {
	if noResolve {
		return line + ",no-resolve"
	}
	return line
}

// ShadowrocketRuleSet 把规则集条目转换为 Shadowrocket（Surge 兼容）规则列表，每行「类型,值」，
// 返回内容与无法转换的条目数。
func ShadowrocketRuleSet(behavior string, lines []string) ([]byte, int) {
	var b strings.Builder
	skipped := 0
	for _, line := range lines {
		line = strings.Trim(strings.TrimSpace(line), `'"`)
		if line == "" {
			continue
		}
		switch behavior {
		case "domain":
			switch {
			case strings.HasPrefix(line, "+."):
				b.WriteString("DOMAIN-SUFFIX," + line[2:] + "\n")
			case strings.HasPrefix(line, "."):
				b.WriteString("DOMAIN-SUFFIX," + line[1:] + "\n")
			case strings.Contains(line, "*"):
				b.WriteString("DOMAIN-WILDCARD," + line + "\n")
			default:
				b.WriteString("DOMAIN," + line + "\n")
			}
		case "ipcidr":
			if strings.Contains(line, ":") {
				b.WriteString("IP-CIDR6," + line + "\n")
			} else {
				b.WriteString("IP-CIDR," + line + "\n")
			}
		default:
			parts := strings.Split(line, ",")
			typ := strings.ToUpper(strings.TrimSpace(parts[0]))
			if len(parts) < 2 || !isShadowrocketType(typ) || typ == "GEOIP" {
				skipped++
				continue
			}
			out := typ + "," + strings.TrimSpace(parts[1])
			if len(parts) > 2 && strings.TrimSpace(parts[len(parts)-1]) == "no-resolve" {
				out += ",no-resolve"
			}
			b.WriteString(out + "\n")
		}
	}
	return []byte(b.String()), skipped
}

// DefaultShadowrocketBase 为 Shadowrocket 基础配置的默认值。
const DefaultShadowrocketBase = `[General]
bypass-system = true
skip-proxy = 192.168.0.0/16, 10.0.0.0/8, 172.16.0.0/12, localhost, *.local, captive.apple.com
tun-excluded-routes = 10.0.0.0/8, 100.64.0.0/10, 127.0.0.0/8, 169.254.0.0/16, 172.16.0.0/12, 192.168.0.0/16, 224.0.0.0/4, 255.255.255.255/32
dns-server = system
ipv6 = true
`
