package routing

import (
	"bytes"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

// Node 为渲染时可用的一个节点。Mihomo 为可直接编码进 proxies 的节点定义：自有节点为结构体，
// 外部节点为 map（名称以 Name 为准）。SingBox 为 sing-box 出站，为空表示 sing-box 不支持。
// Dialer 为外部节点的前置：DialerNode 为真时是节点名，否则是分组名。
type Node struct {
	Name       string
	Own        bool
	Mihomo     any
	SingBox    map[string]any
	Dialer     string
	DialerNode bool
}

// RuleSetRef 为渲染时规则集的下载信息。SingBoxURL / ShadowrocketURL 为主控转换后的地址，为空表示无法转换。
type RuleSetRef struct {
	Name            string
	Behavior        string
	URL             string
	Format          string // yaml / text / mrs，为空时由 Mihomo 按默认（yaml）处理
	Interval        int
	SingBoxURL      string
	ShadowrocketURL string
}

// ExpandGroups 计算各分组的最终成员：固定成员在前，随后为按筛选条件匹配的节点（保持节点顺序）。
// 没有任何成员的分组补 DIRECT，避免客户端因空分组拒绝加载，并返回提示。
func (p *Profile) ExpandGroups(nodes []Node) (map[string][]string, []string) {
	out := map[string][]string{}
	var warnings []string
	for _, g := range p.Groups {
		members := slices.Clone(g.Proxies)
		if g.Nodes != nil {
			matched := filterNodes(g.Nodes, nodes)
			if len(matched) == 0 {
				warnings = append(warnings, fmt.Sprintf("分组 %s 的节点筛选没有匹配到任何节点", g.Name))
			}
			members = append(members, matched...)
		}
		if len(members) == 0 {
			members = []string{"DIRECT"}
			warnings = append(warnings, fmt.Sprintf("分组 %s 为空，已补充 DIRECT", g.Name))
		}
		out[g.Name] = members
	}
	return out, warnings
}

func filterNodes(f *NodeFilter, nodes []Node) []string {
	include, _ := regexp.Compile(f.Include)
	var exclude *regexp.Regexp
	if f.Exclude != "" {
		exclude, _ = regexp.Compile(f.Exclude)
	}
	var out []string
	for _, n := range nodes {
		if (f.Source == SourceOwn && !n.Own) || (f.Source == SourceExternal && n.Own) {
			continue
		}
		if include != nil && f.Include != "" && !include.MatchString(n.Name) {
			continue
		}
		if exclude != nil && exclude.MatchString(n.Name) {
			continue
		}
		out = append(out, n.Name)
	}
	return out
}

type mihomoGroup struct {
	Name      string   `yaml:"name"`
	Type      string   `yaml:"type"`
	Proxies   []string `yaml:"proxies"`
	URL       string   `yaml:"url,omitempty"`
	Interval  int      `yaml:"interval,omitempty"`
	Tolerance int      `yaml:"tolerance,omitempty"`
	Strategy  string   `yaml:"strategy,omitempty"`
}

type mihomoRuleProvider struct {
	Type     string `yaml:"type"`
	Behavior string `yaml:"behavior"`
	Format   string `yaml:"format,omitempty"`
	URL      string `yaml:"url"`
	Path     string `yaml:"path"`
	Interval int    `yaml:"interval"`
}

// RenderMihomo 把方案渲染为完整的 Mihomo 配置：基础配置在前（保留原有顺序与注释），
// 随后为 proxies、proxy-groups、rule-providers（仅被引用的规则集）与 rules。
func (p *Profile) RenderMihomo(nodes []Node, sets map[string]RuleSetRef) ([]byte, []string, error) {
	root := &yaml.Node{Kind: yaml.MappingNode}
	if strings.TrimSpace(p.Mihomo.Base) != "" {
		var doc yaml.Node
		if err := yaml.Unmarshal([]byte(p.Mihomo.Base), &doc); err != nil {
			return nil, nil, fmt.Errorf("Mihomo 基础配置解析失败：%w", err)
		}
		if len(doc.Content) > 0 && doc.Content[0].Kind == yaml.MappingNode {
			base := doc.Content[0]
			for i := 0; i+1 < len(base.Content); i += 2 {
				if !slices.Contains(managedMihomoKeys, base.Content[i].Value) {
					root.Content = append(root.Content, base.Content[i], base.Content[i+1])
				}
			}
		}
	}

	nodes, warnings := p.prepareNodes(nodes)
	proxies := make([]any, 0, len(nodes))
	for _, n := range nodes {
		proxies = append(proxies, mihomoProxy(n))
	}
	expanded, expandWarnings := p.ExpandGroups(nodes)
	warnings = append(warnings, expandWarnings...)
	groups := make([]mihomoGroup, 0, len(p.Groups))
	for _, g := range p.Groups {
		mg := mihomoGroup{Name: g.Name, Type: g.Type, Proxies: expanded[g.Name]}
		if g.Type != GroupSelect {
			mg.URL, mg.Interval, mg.Tolerance = g.URL, g.Interval, g.Tolerance
		}
		if g.Type == GroupLoadBalance {
			mg.Strategy = g.Strategy
		}
		groups = append(groups, mg)
	}

	providers := &yaml.Node{Kind: yaml.MappingNode}
	for _, name := range p.RuleSetNames() {
		ref, ok := sets[name]
		if !ok {
			return nil, nil, fmt.Errorf("规则集不存在：%s", name)
		}
		var v yaml.Node
		if err := v.Encode(mihomoRuleProvider{
			Type: "http", Behavior: ref.Behavior, Format: ref.Format, URL: ref.URL,
			Path: "./ruleset/" + safeFileName(name) + ".yaml", Interval: ref.Interval,
		}); err != nil {
			return nil, nil, err
		}
		providers.Content = append(providers.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: name}, &v)
	}

	rules := make([]string, 0, len(p.Rules))
	for _, r := range p.Rules {
		rules = append(rules, r.MihomoLine())
	}

	for _, kv := range []struct {
		key string
		val any
	}{{"proxies", proxies}, {"proxy-groups", groups}, {"rule-providers", providers}, {"rules", rules}} {
		if kv.key == "rule-providers" && len(providers.Content) == 0 {
			continue
		}
		var v yaml.Node
		if node, ok := kv.val.(*yaml.Node); ok {
			v = *node
		} else if err := v.Encode(kv.val); err != nil {
			return nil, nil, err
		}
		root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: kv.key}, &v)
	}

	out, err := encodeYAML(root)
	if err != nil {
		return nil, nil, err
	}
	return out, warnings, nil
}

// prepareNodes 处理与分组或内置出站重名的节点（追加序号），并移除前置分组无效的外部节点：
// 方案中没有该分组，或节点本身被前置分组（直接或间接）包含而形成循环。前置节点由调用方保证存在。
func (p *Profile) prepareNodes(nodes []Node) ([]Node, []string) {
	var warnings []string
	taken := map[string]bool{}
	for _, b := range Builtins {
		taken[b] = true
	}
	groupNames := map[string]bool{}
	for _, g := range p.Groups {
		taken[g.Name], groupNames[g.Name] = true, true
	}
	for _, n := range nodes {
		if !groupNames[n.Name] {
			taken[n.Name] = true
		}
	}
	renamed := map[string]string{}
	out := make([]Node, 0, len(nodes))
	for _, n := range nodes {
		if groupNames[n.Name] || slices.Contains(Builtins, n.Name) {
			name := n.Name
			for i := 2; taken[name]; i++ {
				name = fmt.Sprintf("%s %d", n.Name, i)
			}
			taken[name], renamed[n.Name] = true, name
			warnings = append(warnings, fmt.Sprintf("节点 %s 与分组或内置出站重名，已改为 %s", n.Name, name))
			n.Name = name
		}
		out = append(out, n)
	}

	for i := range out {
		if r, ok := renamed[out[i].Dialer]; ok && out[i].DialerNode {
			out[i].Dialer = r
		}
	}
	expanded, _ := p.ExpandGroups(out)
	kept := out[:0]
	for _, n := range out {
		switch {
		case n.Dialer == "" || n.DialerNode:
		case !groupNames[n.Dialer]:
			warnings = append(warnings, fmt.Sprintf("节点 %s 的前置分组 %s 不在此方案中，已跳过", n.Name, n.Dialer))
			continue
		case reaches(expanded, n.Dialer, n.Name):
			warnings = append(warnings, fmt.Sprintf("节点 %s 被其前置分组 %s 包含，会形成循环，已跳过", n.Name, n.Dialer))
			continue
		}
		kept = append(kept, n)
	}
	return kept, warnings
}

// reaches 判断分组 group 是否直接或经由子分组包含成员 target。
func reaches(expanded map[string][]string, group, target string) bool {
	seen := map[string]bool{}
	var walk func(string) bool
	walk = func(g string) bool {
		if seen[g] {
			return false
		}
		seen[g] = true
		for _, m := range expanded[g] {
			if m == target {
				return true
			}
			if _, isGroup := expanded[m]; isGroup && walk(m) {
				return true
			}
		}
		return false
	}
	return walk(group)
}

// mihomoProxy 返回节点在 proxies 中的定义。外部节点按 name、type、server、port 在前、其余键按字母序编码，
// 并写入最终名称与前置。
func mihomoProxy(n Node) any {
	m, ok := n.Mihomo.(map[string]any)
	if !ok {
		return n.Mihomo
	}
	m = maps.Clone(m)
	m["name"] = n.Name
	if n.Dialer != "" {
		m["dialer-proxy"] = n.Dialer
	}
	first := []string{"name", "type", "server", "port"}
	keys := slices.Collect(maps.Keys(m))
	slices.SortFunc(keys, func(a, b string) int {
		ia, ib := slices.Index(first, a), slices.Index(first, b)
		switch {
		case ia >= 0 && ib >= 0:
			return ia - ib
		case ia >= 0:
			return -1
		case ib >= 0:
			return 1
		}
		return strings.Compare(a, b)
	})
	node := &yaml.Node{Kind: yaml.MappingNode}
	for _, k := range keys {
		var v yaml.Node
		if err := v.Encode(m[k]); err != nil {
			continue
		}
		node.Content = append(node.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: k}, &v)
	}
	return node
}

// encodeYAML 以两空格缩进编码，并把 yaml.v3 转义的 emoji 等可打印字符还原为原文，便于阅读。
func encodeYAML(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return unescapeYAML(buf.Bytes()), nil
}

var yamlEscape = regexp.MustCompile(`\\(\\|U[0-9A-Fa-f]{8}|u[0-9A-Fa-f]{4})`)

// unescapeYAML 还原双引号字符串中的 \U / \u 转义（仅限可打印字符，控制字符保持转义），
// 转义的反斜杠原样保留。
func unescapeYAML(b []byte) []byte {
	return yamlEscape.ReplaceAllFunc(b, func(m []byte) []byte {
		if m[1] == '\\' {
			return m
		}
		code, err := strconv.ParseUint(string(m[2:]), 16, 32)
		if r := rune(code); err == nil && unicode.IsGraphic(r) {
			return []byte(string(r))
		}
		return m
	})
}

// MihomoLine 返回规则在 Mihomo 配置中的写法。
func (r Rule) MihomoLine() string {
	switch r.Type {
	case RuleRaw:
		return r.Raw
	case "MATCH":
		return "MATCH," + r.Target
	}
	line := r.Type + "," + r.Value + "," + r.Target
	if r.NoResolve {
		line += ",no-resolve"
	}
	return line
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]`)

// safeFileName 把规则集名称转换为客户端缓存文件名，非 ASCII 字符替换为下划线。
func safeFileName(name string) string {
	s := unsafeName.ReplaceAllString(name, "_")
	if strings.Trim(s, "_") == "" {
		return fmt.Sprintf("set_%x", name)
	}
	return s
}

// HostedMihomo 把托管规则集的内容（每行一条，# 开头为注释）转换为 Mihomo 的 YAML 规则集格式。
func HostedMihomo(content string) []byte {
	payload := RuleLines(content)
	b, _ := yaml.Marshal(map[string][]string{"payload": payload})
	return b
}

// RuleLines 返回托管规则集的有效行。
func RuleLines(content string) []string {
	out := []string{}
	for _, line := range strings.Split(content, "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			out = append(out, line)
		}
	}
	return out
}
