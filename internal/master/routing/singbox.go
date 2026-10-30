package routing

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// singBoxDirect 为 sing-box 输出中直连出站的标签，与 Mihomo 的 DIRECT 同名，分组成员可直接引用。
const singBoxDirect = "DIRECT"

// RenderSingBox 把方案渲染为 sing-box 配置（1.12+）：基础配置在前，生成 outbounds（分组、节点与直连）、
// route.rules（基础配置中的规则在前）、route.rule_set（方案与基础配置引用到的规则集）与 route.final。
// sing-box 不支持的内容（RAW 规则、IP-ASN、REJECT 成员等）跳过并返回提示。
// geoBase 为主控中转 geosite / geoip 规则集的目录地址，为空时直接使用 SagerNet 官方地址。
func (p *Profile) RenderSingBox(nodes []Node, sets map[string]RuleSetRef, geoBase string) ([]byte, []string, error) {
	baseText := p.SingBox.Base
	if strings.TrimSpace(baseText) == "" {
		baseText = DefaultSingBoxBase
	}
	var cfg map[string]any
	if err := json.Unmarshal([]byte(baseText), &cfg); err != nil {
		return nil, nil, fmt.Errorf("sing-box 基础配置解析失败：%w", err)
	}
	var warnings []string
	warn := func(format string, args ...any) { warnings = append(warnings, fmt.Sprintf(format, args...)) }

	supported := make([]Node, 0, len(nodes))
	for _, n := range nodes {
		if n.SingBox == nil {
			warn("节点 %s 的协议或传输方式 sing-box 不支持，已跳过", n.Name)
			continue
		}
		supported = append(supported, n)
	}
	supported, prepWarnings := p.prepareNodes(supported)
	warnings = append(warnings, prepWarnings...)
	expanded, expandWarnings := p.ExpandGroups(supported)
	warnings = append(warnings, expandWarnings...)

	// sing-box 没有 REJECT / PASS 出站：从成员中移除；只剩它们的分组视为拒绝分组，以它为目标的规则改为拒绝。
	rejectGroups := map[string]string{} // 分组名 → reject 方式（default / drop）
	for _, g := range p.Groups {
		var kept []string
		method := ""
		for _, m := range expanded[g.Name] {
			switch m {
			case "REJECT":
				method = orStr(method, "default")
			case "REJECT-DROP":
				method = "drop"
			case "PASS":
			default:
				kept = append(kept, m)
			}
		}
		if len(kept) == 0 && method != "" {
			rejectGroups[g.Name] = method
		}
		expanded[g.Name] = kept
	}
	var outbounds []any
	for _, g := range p.Groups {
		if _, ok := rejectGroups[g.Name]; ok {
			continue
		}
		members := slices.DeleteFunc(expanded[g.Name], func(m string) bool { _, r := rejectGroups[m]; return r })
		if len(members) == 0 {
			members = []string{singBoxDirect}
			warn("分组 %s 在 sing-box 中没有可用成员，已补充 DIRECT", g.Name)
		}
		ob := map[string]any{"tag": g.Name, "outbounds": members}
		switch g.Type {
		case GroupSelect:
			ob["type"] = "selector"
		default:
			if g.Type != GroupURLTest {
				warn("分组 %s 的类型 %s 在 sing-box 中以自动测速（urltest）代替", g.Name, g.Type)
			}
			ob["type"], ob["url"], ob["interval"] = "urltest", g.URL, fmt.Sprintf("%ds", g.Interval)
			if g.Tolerance > 0 {
				ob["tolerance"] = g.Tolerance
			}
		}
		outbounds = append(outbounds, ob)
	}
	for _, n := range supported {
		ob := maps.Clone(n.SingBox)
		ob["tag"] = n.Name
		if n.Dialer != "" {
			ob["detour"] = n.Dialer
		}
		outbounds = append(outbounds, ob)
	}
	outbounds = append(outbounds, map[string]any{"type": "direct", "tag": singBoxDirect})
	cfg["outbounds"] = outbounds

	route, _ := cfg["route"].(map[string]any)
	if route == nil {
		route = map[string]any{}
	}
	rules, _ := route["rules"].([]any)
	used := []string{}
	useSet := func(tag string) {
		if !slices.Contains(used, tag) {
			used = append(used, tag)
		}
	}
	resolved := false
	final := ""
	for i, r := range p.Rules {
		n := i + 1
		if r.Type == "MATCH" {
			if _, ok := rejectGroups[r.Target]; ok || r.Target == "REJECT" || r.Target == "REJECT-DROP" {
				warn("MATCH 的目标在 sing-box 中无法作为默认出站，已改为 DIRECT")
				final = singBoxDirect
			} else {
				final = r.Target
			}
			continue
		}
		match, isIP, err := singBoxMatch(r, sets, useSet)
		if err != nil {
			warn("第 %d 条规则 %s", n, err)
			continue
		}
		if isIP && !r.NoResolve && !resolved {
			rules = append(rules, map[string]any{"action": "resolve"})
			resolved = true
		}
		switch method, rejected := rejectGroups[r.Target]; {
		case rejected:
			match["action"], match["method"] = "reject", method
		case r.Target == "REJECT":
			match["action"] = "reject"
		case r.Target == "REJECT-DROP":
			match["action"], match["method"] = "reject", "drop"
		case r.Target == "PASS":
			continue
		default:
			match["outbound"] = r.Target
		}
		rules = append(rules, match)
	}
	route["rules"] = rules
	if final == "" {
		final = singBoxDirect
	}
	route["final"] = final

	// 基础配置（如 DNS 规则）中引用的规则集也需要定义。
	for _, tag := range ruleSetTags(cfg) {
		useSet(tag)
	}
	var ruleSets []any
	for _, tag := range used {
		if def, ok := singBoxRuleSetDef(tag, sets, geoBase); ok {
			ruleSets = append(ruleSets, def)
		} else {
			warn("规则集 %s 无法用于 sing-box", tag)
		}
	}
	if len(ruleSets) > 0 {
		route["rule_set"] = ruleSets
	} else {
		delete(route, "rule_set")
	}
	cfg["route"] = route

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(cfg); err != nil {
		return nil, nil, err
	}
	return buf.Bytes(), warnings, nil
}

// singBoxMatch 把一条规则转换为 sing-box 路由规则的匹配条件；isIP 表示需要目标 IP 才能匹配。
func singBoxMatch(r Rule, sets map[string]RuleSetRef, useSet func(string)) (map[string]any, bool, error) {
	v := r.Value
	switch r.Type {
	case "RULE-SET":
		ref, ok := sets[v]
		if !ok || ref.SingBoxURL == "" {
			return nil, false, fmt.Errorf("引用的规则集 %s 无法用于 sing-box（mrs 格式不支持转换）", v)
		}
		useSet(v)
		return map[string]any{"rule_set": []string{v}}, ref.Behavior != "domain", nil
	case "GEOSITE":
		tag := "geosite-" + strings.ToLower(v)
		useSet(tag)
		return map[string]any{"rule_set": []string{tag}}, false, nil
	case "GEOIP":
		if strings.EqualFold(v, "LAN") || strings.EqualFold(v, "private") {
			return map[string]any{"ip_is_private": true}, true, nil
		}
		tag := "geoip-" + strings.ToLower(v)
		useSet(tag)
		return map[string]any{"rule_set": []string{tag}}, true, nil
	}
	field, value, err := classicalField(r.Type, v)
	if err != nil {
		return nil, false, err
	}
	return map[string]any{field: value}, field == "ip_cidr", nil
}

// classicalField 把单条规则（类型 + 值）转换为 sing-box 规则字段，路由规则与规则集共用。
func classicalField(typ, v string) (string, any, error) {
	switch typ {
	case "DOMAIN":
		return "domain", []string{v}, nil
	case "DOMAIN-SUFFIX":
		return "domain_suffix", []string{v}, nil
	case "DOMAIN-KEYWORD":
		return "domain_keyword", []string{v}, nil
	case "DOMAIN-REGEX":
		return "domain_regex", []string{v}, nil
	case "IP-CIDR", "IP-CIDR6":
		return "ip_cidr", []string{v}, nil
	case "SRC-IP-CIDR":
		return "source_ip_cidr", []string{v}, nil
	case "PROCESS-NAME":
		return "process_name", []string{v}, nil
	case "PROCESS-PATH":
		return "process_path", []string{v}, nil
	case "NETWORK":
		return "network", []string{strings.ToLower(v)}, nil
	case "DST-PORT", "SRC-PORT":
		ports, ranges, err := parsePorts(v)
		if err != nil {
			return "", nil, err
		}
		prefix := ""
		if typ == "SRC-PORT" {
			prefix = "source_"
		}
		if len(ranges) > 0 {
			if len(ports) > 0 {
				return "", nil, errors.New("端口与端口范围混用，sing-box 不支持")
			}
			return prefix + "port_range", ranges, nil
		}
		return prefix + "port", ports, nil
	}
	return "", nil, fmt.Errorf("类型 %s sing-box 不支持，已跳过", typ)
}

// parsePorts 解析 Mihomo 的端口写法：80 / 80/443 / 1000-2000。
func parsePorts(v string) ([]int, []string, error) {
	var ports []int
	var ranges []string
	for _, part := range strings.Split(v, "/") {
		part = strings.TrimSpace(part)
		if a, b, ok := strings.Cut(part, "-"); ok {
			ranges = append(ranges, a+":"+b)
			continue
		}
		n, err := strconv.Atoi(part)
		if err != nil {
			return nil, nil, fmt.Errorf("端口 %s 无效", part)
		}
		ports = append(ports, n)
	}
	return ports, ranges, nil
}

// ruleSetTags 收集配置中各处 rule_set 字段引用的标签（如 DNS 规则）。
func ruleSetTags(v any) []string {
	var out []string
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			for k, val := range x {
				if k == "rule_set" {
					switch t := val.(type) {
					case string:
						out = append(out, t)
					case []any:
						for _, s := range t {
							if str, ok := s.(string); ok {
								out = append(out, str)
							}
						}
					}
					continue
				}
				walk(val)
			}
		case []any:
			for _, e := range x {
				walk(e)
			}
		}
	}
	if m, ok := v.(map[string]any); ok {
		for k, val := range m {
			if k != "route" { // route.rule_set 由方案生成，route.rules 中的引用在下面单独收集
				walk(val)
			}
		}
		if route, ok := m["route"].(map[string]any); ok {
			walk(route["rules"])
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// singBoxRuleSetDef 返回规则集定义：规则集库中的规则集指向主控转换后的地址；geosite-* / geoip-* 使用
// SagerNet 官方规则集，有 geoBase 时经主控中转。主控提供的规则集直连下载，否则经默认出站下载。
func singBoxRuleSetDef(tag string, sets map[string]RuleSetRef, geoBase string) (map[string]any, bool) {
	if ref, ok := sets[tag]; ok {
		if ref.SingBoxURL == "" {
			return nil, false
		}
		return singBoxHostedSet(tag, "source", ref.SingBoxURL, fmt.Sprintf("%ds", ref.Interval)), true
	}
	if !strings.HasPrefix(tag, "geosite-") && !strings.HasPrefix(tag, "geoip-") {
		return nil, false
	}
	if geoBase != "" {
		return singBoxHostedSet(tag, "binary", geoBase+tag+".srs", "1d"), true
	}
	return map[string]any{"type": "remote", "tag": tag, "format": "binary", "url": GeoRuleSetURL(tag)}, true
}

// singBoxHostedSet 返回主控提供的远程规则集定义。规则集与订阅本身一样直连下载，避免节点不可用时
// 规则集无法加载、客户端无法启动；download_detour 在 1.14 起提示弃用，替代的 http_client 在 1.12 / 1.13
// 上无法解析，暂沿用。
func singBoxHostedSet(tag, format, url, interval string) map[string]any {
	return map[string]any{"type": "remote", "tag": tag, "format": format, "url": url,
		"download_detour": singBoxDirect, "update_interval": interval}
}

// GeoRuleSetURL 返回 SagerNet 官方 geosite-* / geoip-* 规则集的下载地址。
func GeoRuleSetURL(tag string) string {
	repo := "sing-geoip"
	if strings.HasPrefix(tag, "geosite-") {
		repo = "sing-geosite"
	}
	return "https://raw.githubusercontent.com/SagerNet/" + repo + "/rule-set/" + tag + ".srs"
}

// ParseMihomoRuleSet 解析 Mihomo 规则集文件的条目：format 为 text 时每行一条，为空或 yaml 时读取 payload。
func ParseMihomoRuleSet(body []byte, format string) ([]string, error) {
	switch format {
	case "text":
		return RuleLines(string(body)), nil
	case "", "yaml":
		var doc struct {
			Payload []string `yaml:"payload"`
		}
		if err := yaml.Unmarshal(body, &doc); err != nil {
			return nil, fmt.Errorf("规则集不是合法的 YAML：%w", err)
		}
		return doc.Payload, nil
	}
	return nil, fmt.Errorf("规则集格式 %s 无法转换为 sing-box", format)
}

var wildcard = regexp.MustCompile(`\\\*`)

// SingBoxRuleSet 把规则集条目转换为 sing-box 源格式规则集（JSON），返回内容与无法转换的条目数。
// 每个字段一条规则，规则之间为「或」关系。
func SingBoxRuleSet(behavior string, lines []string) ([]byte, int) {
	fields := map[string][]any{}
	var order []string
	add := func(k string, v any) {
		if _, ok := fields[k]; !ok {
			order = append(order, k)
		}
		switch x := v.(type) {
		case []string:
			for _, s := range x {
				fields[k] = append(fields[k], s)
			}
		case []int:
			for _, n := range x {
				fields[k] = append(fields[k], n)
			}
		}
	}
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
				add("domain_suffix", []string{line[2:]})
			case strings.HasPrefix(line, "."):
				add("domain_suffix", []string{line})
			case strings.Contains(line, "*"):
				add("domain_regex", []string{"^" + wildcard.ReplaceAllString(regexp.QuoteMeta(line), `[^.]+`) + "$"})
			default:
				add("domain", []string{line})
			}
		case "ipcidr":
			add("ip_cidr", []string{line})
		default:
			parts := strings.Split(line, ",")
			if len(parts) < 2 {
				skipped++
				continue
			}
			k, v, err := classicalField(strings.ToUpper(strings.TrimSpace(parts[0])), strings.TrimSpace(parts[1]))
			if err != nil {
				skipped++
				continue
			}
			add(k, v)
		}
	}
	rules := make([]any, 0, len(order))
	for _, k := range order {
		rules = append(rules, map[string]any{k: fields[k]})
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	enc.Encode(map[string]any{"version": 2, "rules": rules})
	return buf.Bytes(), skipped
}

func orStr(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

// DefaultSingBoxBase 为 sing-box 基础配置的默认值：TUN 与本地 mixed 入站，国内域名走本地 DNS。
// 远程 DNS 未指定 detour，经默认出站（route.final）连接。
const DefaultSingBoxBase = `{
  "log": {
    "level": "warn",
    "timestamp": true
  },
  "dns": {
    "servers": [
      { "type": "https", "tag": "remote", "server": "1.1.1.1" },
      { "type": "udp", "tag": "local", "server": "223.5.5.5" }
    ],
    "rules": [
      { "rule_set": "geosite-cn", "server": "local" }
    ],
    "final": "remote"
  },
  "inbounds": [
    { "type": "tun", "tag": "tun-in", "address": ["172.19.0.1/30", "fdfe:dcba:9876::1/126"], "auto_route": true, "strict_route": true },
    { "type": "mixed", "tag": "mixed-in", "listen": "127.0.0.1", "listen_port": 2080 }
  ],
  "route": {
    "rules": [
      { "action": "sniff" },
      { "protocol": "dns", "action": "hijack-dns" }
    ],
    "auto_detect_interface": true,
    "default_domain_resolver": "local"
  }
}
`
