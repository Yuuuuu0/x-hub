package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Yuuuuu0/x-hub/internal/master/routing"
	"github.com/Yuuuuu0/x-hub/internal/master/store"
)

// TestRuleSetRelay 覆盖外部规则集经主控中转：Mihomo 引用主控地址、原样输出并缓存，sing-box 转换同样走缓存。
func TestRuleSetRelay(t *testing.T) {
	h := newTestHandler(t)
	var hits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Write([]byte("payload:\n  - DOMAIN-SUFFIX,example.com\n"))
	}))
	defer upstream.Close()
	if _, err := h.Store.CreateRuleSet(&store.RuleSet{Name: "Ext", Behavior: "classical", Source: routing.SetURL,
		URL: upstream.URL + "/ext.yaml", Format: "yaml", Interval: 3600}); err != nil {
		t.Fatal(err)
	}

	// 未设置公开访问地址时保持原链接。
	refs, err := h.ruleSetRefs()
	if err != nil || refs["Ext"].URL != upstream.URL+"/ext.yaml" {
		t.Fatalf("refs without public url: %v %v", refs, err)
	}
	a := h.Settings.Access()
	a.PublicURL = "https://panel.example.com"
	if err := h.Settings.SaveAccess(a); err != nil {
		t.Fatal(err)
	}
	refs, _ = h.ruleSetRefs()
	base := "https://panel.example.com/" + h.Settings.Subscription().Path + "/rules/" + h.Settings.RulesToken() + "/"
	if refs["Ext"].URL != base+"Ext.yaml" || refs["Ext"].Format != "yaml" {
		t.Fatalf("mihomo ref: %+v", refs["Ext"])
	}

	get := func(file string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, strings.TrimPrefix(base, "https://panel.example.com")+file, nil))
		return rec
	}
	if rec := get("Ext.yaml"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "DOMAIN-SUFFIX,example.com") {
		t.Fatalf("relay: %d %s", rec.Code, rec.Body)
	}
	if rec := get("Ext.json"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "example.com") {
		t.Fatalf("convert: %d %s", rec.Code, rec.Body)
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("upstream fetched %d times, want 1 (cached)", n)
	}
	if rec := get("geo/../Ext.srs"); rec.Code != http.StatusNotFound {
		t.Fatalf("invalid geo tag: %d", rec.Code)
	}
}

// TestInspectRuleSet 覆盖规则集格式检测：内容与所选格式不一致时给出建议，与文件扩展名无关。
func TestInspectRuleSet(t *testing.T) {
	yamlBody := "# 注释\npayload:\n  - '91.108.4.0/22'\n  - '91.108.8.0/21'\n"
	cases := []struct {
		name, body, format string
		rules              int
		suggest            string
		warned             bool
	}{
		{"yaml 选 text", yamlBody, "text", 2, "yaml", true},
		{"yaml 选默认", yamlBody, "", 2, "", false},
		{"yaml 选 yaml", "---\n" + yamlBody, "yaml", 2, "", false},
		{"text 选默认", "1.1.1.0/24\n2.2.2.0/24\n3.3.3.0/24\n", "", 3, "text", true},
		{"text 选 text", "1.1.1.0/24\n", "text", 1, "", false},
		{"mrs 选 mrs", "\x28\xb5\x2f\xfd\x00", "mrs", 0, "", false},
		{"mrs 选 text", "\x28\xb5\x2f\xfd\x00", "text", 0, "mrs", true},
		{"text 选 mrs", "1.1.1.0/24\n", "mrs", 1, "text", true},
		{"网页", "<!DOCTYPE html>\n<html></html>\n", "", 0, "", true},
		{"空内容", "\n# 只有注释\n", "text", 0, "", true},
		{"空 payload", "payload:\n", "yaml", 0, "", true},
	}
	for _, c := range cases {
		got := inspectRuleSet([]byte(c.body), c.format)
		if got.Rules != c.rules || got.Suggest != c.suggest || (got.Warning != "") != c.warned {
			t.Errorf("%s: %+v", c.name, got)
		}
	}
}
