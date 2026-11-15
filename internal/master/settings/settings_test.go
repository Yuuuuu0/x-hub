package settings

import (
	"path/filepath"
	"testing"

	"github.com/Yuuuuu0/x-hub/internal/master/store"
)

func TestValidateAccess(t *testing.T) {
	ok := Access{Listen: ":2096", PanelPath: "/abcd1234/", PublicURL: "https://hub.example.com:2096/", TLSMode: TLSProxy,
		TrustedProxies: []string{" 173.245.48.0/20 ", "", "127.0.0.1"}}
	if err := ValidateAccess(&ok); err != nil {
		t.Fatal(err)
	}
	if ok.PanelPath != "abcd1234" || ok.PublicURL != "https://hub.example.com:2096" || len(ok.TrustedProxies) != 2 {
		t.Fatalf("normalization failed: %+v", ok)
	}

	bad := []Access{
		{Listen: "2096", PanelPath: "abcd1234", TLSMode: TLSProxy},
		{Listen: ":70000", PanelPath: "abcd1234", TLSMode: TLSProxy},
		{Listen: ":2096", PanelPath: "ab", TLSMode: TLSProxy},
		{Listen: ":2096", PanelPath: "agent", TLSMode: TLSProxy},
		{Listen: ":2096", PanelPath: "abcd1234", PublicURL: "https://hub.example.com/panel", TLSMode: TLSProxy},
		{Listen: ":2096", PanelPath: "abcd1234", TLSMode: TLSFile},
		{Listen: ":2096", PanelPath: "abcd1234", TLSMode: "acme"},
		{Listen: ":2096", PanelPath: "abcd1234", TLSMode: TLSProxy, TrustedProxies: []string{"not-an-ip"}},
	}
	for i, a := range bad {
		if err := ValidateAccess(&a); err == nil {
			t.Errorf("case %d should fail: %+v", i, a)
		}
	}
}

// TestSaveAndLoadLegacyXray 验证 SaveXray 校验逻辑，以及兼容加载数据库中残留的 defaultVersion 字段。
func TestSaveAndLoadLegacyXray(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	// 模拟旧版本数据库写入了含 defaultVersion 的 JSON
	legacy := map[string]string{
		"defaultVersion": "v25.1.1",
		"maxVersion":     "v26.7.28",
	}
	if err := st.PutSetting(keyXray, legacy); err != nil {
		t.Fatal(err)
	}

	svc, _, err := Load(st, func() string { return "panel1234" })
	if err != nil {
		t.Fatalf("load with legacy defaultVersion failed: %v", err)
	}
	if got := svc.Xray().MaxVersion; got != "v26.7.28" {
		t.Fatalf("MaxVersion = %q, want %q", got, "v26.7.28")
	}

	if err := svc.SaveXray(Xray{MaxVersion: "invalid"}); err == nil {
		t.Fatal("invalid MaxVersion must be rejected")
	}
	if err := svc.SaveXray(Xray{MaxVersion: "v26.8.1"}); err != nil {
		t.Fatalf("valid MaxVersion rejected: %v", err)
	}
	if got := svc.Xray().MaxVersion; got != "v26.8.1" {
		t.Fatalf("updated MaxVersion = %q, want %q", got, "v26.8.1")
	}
	if err := svc.SaveXray(Xray{}); err != nil {
		t.Fatalf("empty MaxVersion rejected: %v", err)
	}
	if got := svc.Xray().MaxVersion; got != "" {
		t.Fatalf("cleared MaxVersion = %q, want empty", got)
	}

	reloaded, _, err := Load(st, func() string { return "panel1234" })
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.Xray().MaxVersion; got != "" {
		t.Fatalf("reloaded MaxVersion = %q, want empty", got)
	}
}
