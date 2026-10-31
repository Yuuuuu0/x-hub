package sub

import "testing"

// TestRenderName 覆盖分段省略、连接符清理与未知变量。
func TestRenderName(t *testing.T) {
	tmpl := "{flag} {region}-{remark}|📊{traffic}|⏳{days}"
	cases := []struct {
		vars map[string]string
		want string
	}{
		{map[string]string{"flag": "🇯🇵", "region": "日本", "remark": "acck", "traffic": "1.14TB", "days": "69D"}, "🇯🇵 日本-acck|📊1.14TB|⏳69D"},
		{map[string]string{"flag": "🇯🇵", "region": "日本", "remark": "", "traffic": "", "days": "69D"}, "🇯🇵 日本|⏳69D"},
		{map[string]string{"flag": "", "region": "", "remark": "", "traffic": "", "days": ""}, ""},
	}
	for _, c := range cases {
		if got := RenderName(tmpl, c.vars); got != c.want {
			t.Errorf("RenderName(%v) = %q, want %q", c.vars, got, c.want)
		}
	}
	if got := RenderName("{node} {unknown}", map[string]string{"node": "东京"}); got != "东京 {unknown}" {
		t.Errorf("unknown variable: %q", got)
	}
}

func TestFormatRemaining(t *testing.T) {
	for b, want := range map[int64]string{
		1253400000000: "1.14TB", 512 << 30: "512GB", 300 << 20: "300MB", -5: "0MB", 1536 << 20: "1.5GB",
	} {
		if got := FormatRemaining(b); got != want {
			t.Errorf("FormatRemaining(%d) = %q, want %q", b, got, want)
		}
	}
}

func TestRealityNeedsMLKEM(t *testing.T) {
	for v, want := range map[string]bool{"v26.7.11": false, "v26.7.28": false, "v26.7.29": true, "26.10.31": true, "v27.1.1": true, "": false} {
		if got := realityNeedsMLKEM(v); got != want {
			t.Errorf("%q: got %v, want %v", v, got, want)
		}
	}
}
