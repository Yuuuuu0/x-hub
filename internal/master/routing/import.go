package routing

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// 规则集来源与行为。
const (
	SetHosted = "hosted"
	SetURL    = "url"
)

// SetBehaviors 为支持的规则集行为。
var SetBehaviors = []string{"domain", "ipcidr", "classical"}

// ImportedSet 为从 Mihomo 配置中解析出的规则集。
type ImportedSet struct {
	Name     string
	Behavior string
	Source   string
	URL      string
	Format   string
	Interval int
	Content  string
}

// ImportResult 为导入结果。Notes 说明被忽略或转换的内容。
type ImportResult struct {
	Profile *Profile
	Sets    []ImportedSet
	Notes   []string
}

// logicRules 为以原文保存的 Mihomo 逻辑规则前缀。
var logicRules = []string{"AND", "OR", "NOT", "SUB-RULE"}

// ImportMihomo 把现有 Mihomo 配置拆分为分流方案：proxy-groups → 分组，rule-providers → 规则集，
// rules → 规则，其余键保留为基础配置。使用 use / filter / include-all 的分组转换为节点筛选。
func ImportMihomo(text string) (*ImportResult, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(text), &doc); err != nil {
		return nil, fmt.Errorf("YAML 解析失败：%w", err)
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("配置顶层应为键值映射")
	}
	root := doc.Content[0]
	res := &ImportResult{Profile: &Profile{}}
	base := &yaml.Node{Kind: yaml.MappingNode}

	for i := 0; i+1 < len(root.Content); i += 2 {
		key, val := root.Content[i], root.Content[i+1]
		var err error
		switch key.Value {
		case "proxies":
			var list []any
			val.Decode(&list)
			res.Notes = append(res.Notes, fmt.Sprintf("忽略了 %d 个固定节点定义：节点由 x-hub 按订阅生成，外部节点请在「外部节点」中添加", len(list)))
		case "proxy-providers":
			res.Notes = append(res.Notes, "忽略了 proxy-providers：引用它的分组已转换为按名称筛选 x-hub 节点，外部订阅请在「外部节点」中添加")
		case "proxy-groups":
			err = res.importGroups(val)
		case "rule-providers":
			err = res.importSets(val)
		case "rules":
			err = res.importRules(val)
		default:
			base.Content = append(base.Content, key, val)
		}
		if err != nil {
			return nil, fmt.Errorf("%s：%w", key.Value, err)
		}
	}

	// 分组中引用的固定节点名在 x-hub 中不存在，移除并提示。
	groupNames := map[string]bool{}
	for _, g := range res.Profile.Groups {
		groupNames[g.Name] = true
	}
	for i := range res.Profile.Groups {
		g := &res.Profile.Groups[i]
		kept := g.Proxies[:0]
		for _, m := range g.Proxies {
			if groupNames[m] || slices.Contains(Builtins, m) {
				kept = append(kept, m)
			} else {
				res.Notes = append(res.Notes, fmt.Sprintf("分组 %s 中的成员 %s 不是分组或内置出站，已移除", g.Name, m))
			}
		}
		g.Proxies = kept
	}

	if len(base.Content) > 0 {
		b, err := encodeYAML(base)
		if err != nil {
			return nil, err
		}
		res.Profile.Mihomo.Base = string(b)
	}
	return res, nil
}

func (res *ImportResult) importGroups(val *yaml.Node) error {
	var list []struct {
		Name              string   `yaml:"name"`
		Type              string   `yaml:"type"`
		Proxies           []string `yaml:"proxies"`
		Use               []string `yaml:"use"`
		Filter            string   `yaml:"filter"`
		ExcludeFilter     string   `yaml:"exclude-filter"`
		IncludeAll        bool     `yaml:"include-all"`
		IncludeAllProxies bool     `yaml:"include-all-proxies"`
		URL               string   `yaml:"url"`
		Interval          int      `yaml:"interval"`
		Tolerance         int      `yaml:"tolerance"`
		Strategy          string   `yaml:"strategy"`
	}
	if err := val.Decode(&list); err != nil {
		return err
	}
	for _, g := range list {
		group := Group{
			Name: g.Name, Type: g.Type, Proxies: g.Proxies, URL: g.URL,
			Interval: g.Interval, Tolerance: g.Tolerance, Strategy: g.Strategy,
		}
		if group.Proxies == nil {
			group.Proxies = []string{}
		}
		if len(g.Use) > 0 || g.IncludeAll || g.IncludeAllProxies || g.Filter != "" {
			group.Nodes = &NodeFilter{Include: g.Filter, Exclude: g.ExcludeFilter, Source: SourceAll}
		}
		if !slices.Contains(groupTypes, group.Type) {
			res.Notes = append(res.Notes, fmt.Sprintf("分组 %s 的类型 %s 不受支持，已改为 select", g.Name, g.Type))
			group.Type = GroupSelect
		}
		res.Profile.Groups = append(res.Profile.Groups, group)
	}
	return nil
}

func (res *ImportResult) importSets(val *yaml.Node) error {
	for i := 0; i+1 < len(val.Content); i += 2 {
		name := val.Content[i].Value
		var p struct {
			Type     string   `yaml:"type"`
			Behavior string   `yaml:"behavior"`
			Format   string   `yaml:"format"`
			URL      string   `yaml:"url"`
			Interval int      `yaml:"interval"`
			Payload  []string `yaml:"payload"`
		}
		if err := val.Content[i+1].Decode(&p); err != nil {
			return err
		}
		set := ImportedSet{Name: name, Behavior: p.Behavior, Format: p.Format, Interval: p.Interval}
		if set.Interval <= 0 {
			set.Interval = 86400
		}
		switch p.Type {
		case "http":
			set.Source, set.URL = SetURL, p.URL
		case "inline":
			set.Source, set.Content = SetHosted, strings.Join(p.Payload, "\n")
		default:
			res.Notes = append(res.Notes, fmt.Sprintf("规则集 %s 的类型 %s 不受支持（仅支持 http 与 inline），已跳过", name, p.Type))
			continue
		}
		if !slices.Contains(SetBehaviors, set.Behavior) {
			res.Notes = append(res.Notes, fmt.Sprintf("规则集 %s 的行为 %s 不受支持，已跳过", name, p.Behavior))
			continue
		}
		res.Sets = append(res.Sets, set)
	}
	return nil
}

func (res *ImportResult) importRules(val *yaml.Node) error {
	var lines []string
	if err := val.Decode(&lines); err != nil {
		return err
	}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		parts := strings.Split(line, ",")
		typ := strings.ToUpper(strings.TrimSpace(parts[0]))
		switch {
		case slices.Contains(logicRules, typ):
			res.Profile.Rules = append(res.Profile.Rules, Rule{Type: RuleRaw, Raw: line})
		case typ == "MATCH" && len(parts) >= 2:
			res.Profile.Rules = append(res.Profile.Rules, Rule{Type: "MATCH", Target: strings.TrimSpace(parts[1])})
		case len(parts) >= 3 && slices.Contains(ruleTypes, typ):
			r := Rule{Type: typ, Value: strings.TrimSpace(parts[1]), Target: strings.TrimSpace(parts[2])}
			for _, opt := range parts[3:] {
				if strings.TrimSpace(opt) == "no-resolve" {
					r.NoResolve = true
				}
			}
			res.Profile.Rules = append(res.Profile.Rules, r)
		default:
			res.Notes = append(res.Notes, "无法识别的规则，已以原文保留："+line)
			res.Profile.Rules = append(res.Profile.Rules, Rule{Type: RuleRaw, Raw: line})
		}
	}
	return nil
}
