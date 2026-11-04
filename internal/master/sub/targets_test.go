package sub

import (
	"fmt"
	"testing"

	"github.com/Yuuuuu0/x-hub/internal/master/store"
)

// TestTargets 覆盖服务器多地址、入站地址覆盖（含仅覆盖端口）、去重与标签在名称中的位置。
func TestTargets(t *testing.T) {
	n := &store.Node{Name: "东京", PublicHosts: []store.PublicAddr{{Host: "1.2.3.4"}, {Host: "2001:db8::1", Label: "IPv6"}}}
	vars := NameVars{Flag: "🇯🇵", Region: "日本", Days: "69D"}
	tmpl := "{flag} {region}-{remark}|📊{traffic}|⏳{days}"
	show := func(in *store.Inbound, tmpl string) string {
		var s string
		for _, t := range Targets(n, in, tmpl, vars) {
			s += fmt.Sprintf("%s:%d %s\n", t.Host, t.Port, t.Name)
		}
		return s
	}
	cases := []struct {
		name string
		in   *store.Inbound
		tmpl string
		want string
	}{
		{"继承服务器地址", &store.Inbound{Remark: "acck", Port: 443}, tmpl,
			"1.2.3.4:443 🇯🇵 日本-acck|⏳69D\n2001:db8::1:443 🇯🇵 日本-acck IPv6|⏳69D\n"},
		{"备注为空不留连接符", &store.Inbound{Port: 443}, tmpl,
			"1.2.3.4:443 🇯🇵 日本|⏳69D\n2001:db8::1:443 🇯🇵 日本 IPv6|⏳69D\n"},
		{"模板显式使用 line", &store.Inbound{Port: 443}, "{line}|{node}",
			"1.2.3.4:443 东京\n2001:db8::1:443 IPv6|东京\n"},
		{"无变量模板", &store.Inbound{Port: 443}, "固定名称",
			"1.2.3.4:443 固定名称\n2001:db8::1:443 固定名称 IPv6\n"},
		{"仅覆盖端口并追加标签、另加 CDN 地址", &store.Inbound{Port: 443, LinkAddrs: []store.LinkAddr{
			{Port: 8443, Label: "中转"}, {Host: "cdn.example.com", Label: "CDN"}, {Host: "1.2.3.4", Port: 8443}}}, "{node}",
			"1.2.3.4:8443 东京 中转\n2001:db8::1:8443 东京 IPv6 中转\ncdn.example.com:443 东京 CDN\n"},
	}
	for _, c := range cases {
		if got := show(c.in, c.tmpl); got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}
	if got := Targets(&store.Node{}, &store.Inbound{Port: 443}, tmpl, vars); len(got) != 0 {
		t.Errorf("no address: %v", got)
	}
}
