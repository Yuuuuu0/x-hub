// Package geo 提供国家或地区代码（ISO 3166-1 两位）对应的中文名称与旗帜，用于节点命名与界面展示。
package geo

import "strings"

// Country 为一个国家或地区。
type Country struct {
	Code string `json:"code"`
	Name string `json:"name"`
	Flag string `json:"flag"`
}

// names 为常见 VPS 所在国家或地区的中文名称，按大致常用程度排列。
var names = [][2]string{
	{"HK", "香港"}, {"TW", "台湾"}, {"JP", "日本"}, {"SG", "新加坡"}, {"KR", "韩国"}, {"US", "美国"},
	{"MO", "澳门"}, {"CN", "中国"}, {"GB", "英国"}, {"DE", "德国"}, {"FR", "法国"}, {"NL", "荷兰"},
	{"CA", "加拿大"}, {"AU", "澳大利亚"}, {"RU", "俄罗斯"}, {"IN", "印度"}, {"MY", "马来西亚"}, {"TH", "泰国"},
	{"VN", "越南"}, {"PH", "菲律宾"}, {"ID", "印度尼西亚"}, {"TR", "土耳其"}, {"AE", "阿联酋"}, {"IL", "以色列"},
	{"SA", "沙特阿拉伯"}, {"IT", "意大利"}, {"ES", "西班牙"}, {"PT", "葡萄牙"}, {"CH", "瑞士"}, {"AT", "奥地利"},
	{"BE", "比利时"}, {"LU", "卢森堡"}, {"IE", "爱尔兰"}, {"SE", "瑞典"}, {"NO", "挪威"}, {"FI", "芬兰"},
	{"DK", "丹麦"}, {"IS", "冰岛"}, {"PL", "波兰"}, {"CZ", "捷克"}, {"HU", "匈牙利"}, {"RO", "罗马尼亚"},
	{"BG", "保加利亚"}, {"GR", "希腊"}, {"UA", "乌克兰"}, {"MD", "摩尔多瓦"}, {"LT", "立陶宛"}, {"LV", "拉脱维亚"},
	{"EE", "爱沙尼亚"}, {"RS", "塞尔维亚"}, {"HR", "克罗地亚"}, {"KZ", "哈萨克斯坦"}, {"MN", "蒙古"}, {"KH", "柬埔寨"},
	{"MM", "缅甸"}, {"LA", "老挝"}, {"BD", "孟加拉国"}, {"PK", "巴基斯坦"}, {"NP", "尼泊尔"}, {"LK", "斯里兰卡"},
	{"NZ", "新西兰"}, {"MX", "墨西哥"}, {"BR", "巴西"}, {"AR", "阿根廷"}, {"CL", "智利"}, {"CO", "哥伦比亚"},
	{"PE", "秘鲁"}, {"ZA", "南非"}, {"EG", "埃及"}, {"NG", "尼日利亚"}, {"KE", "肯尼亚"}, {"MA", "摩洛哥"},
}

var nameOf = func() map[string]string {
	m := make(map[string]string, len(names))
	for _, n := range names {
		m[n[0]] = n[1]
	}
	return m
}()

// Name 返回国家或地区的中文名称，未收录时返回代码本身。
func Name(code string) string {
	code = strings.ToUpper(code)
	if n, ok := nameOf[code]; ok {
		return n
	}
	return code
}

// Flag 返回国家或地区的旗帜 emoji（由两个区域指示符组成），代码无效时返回空。
func Flag(code string) string {
	code = strings.ToUpper(code)
	if len(code) != 2 || code[0] < 'A' || code[0] > 'Z' || code[1] < 'A' || code[1] > 'Z' {
		return ""
	}
	return string([]rune{0x1F1E6 + rune(code[0]-'A'), 0x1F1E6 + rune(code[1]-'A')})
}

// List 返回收录的国家或地区，供界面选择。
func List() []Country {
	out := make([]Country, len(names))
	for i, n := range names {
		out[i] = Country{Code: n[0], Name: n[1], Flag: Flag(n[0])}
	}
	return out
}
