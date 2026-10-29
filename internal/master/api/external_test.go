package api

import (
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/Yuuuuu0/x-hub/internal/master/external"
	"github.com/Yuuuuu0/x-hub/internal/master/store"
)

func addExternal(t *testing.T, h *Handler, name, content, chain string, subIDs ...int64) {
	t.Helper()
	ps, _, err := external.Parse(content)
	if err != nil {
		t.Fatal(err)
	}
	nodes, _ := json.Marshal(ps)
	if _, err := h.Store.CreateExternalSource(&store.ExternalSource{Kind: store.ExternalManual, Name: name, Enabled: true,
		Content: content, Chain: chain, SubIDs: subIDs, Nodes: nodes}); err != nil {
		t.Fatal(err)
	}
}

// TestExternalNodesInSubscription 覆盖外部节点的可见范围、重名、前置入站与前置分组，以及 Base64 中排除带前置的节点。
func TestExternalNodesInSubscription(t *testing.T) {
	h := newTestHandler(t)
	n1, _ := h.Store.CreateNode("东京", "", "jp.example.com")
	in1 := addInbound(t, h, n1, 443, "")
	subA, _ := CreateSubscription(h.Store, "A", "")
	subB, _ := CreateSubscription(h.Store, "B", "")
	a, _ := h.Store.GetSubscription(subA)
	b, _ := h.Store.GetSubscription(subB)

	addExternal(t, h, "机场", "trojan://pw@x.com:443#东京\nss://YWVzLTEyOC1nY206cHc@y.com:8388#自动选择", "", subA)
	addExternal(t, h, "家宽", "socks5://u:p@5.6.7.8:1080#家宽", "inbound:"+strconv.FormatInt(in1, 10), subA)
	addExternal(t, h, "落地", "socks5://u:p@9.9.9.9:1080#落地", "group:自动选择", subA)

	var cfg struct {
		Proxies []map[string]any `yaml:"proxies"`
		Groups  []struct {
			Name    string   `yaml:"name"`
			Proxies []string `yaml:"proxies"`
		} `yaml:"proxy-groups"`
	}
	if err := yaml.Unmarshal(fetchSub(h, a.Token, "clash-verge", "").Body.Bytes(), &cfg); err != nil {
		t.Fatal(err)
	}
	byName := map[string]map[string]any{}
	for _, p := range cfg.Proxies {
		byName[p["name"].(string)] = p
	}
	// 「东京」与自有节点重名追加序号；「自动选择」与内置分组重名追加序号；
	// 「落地」的前置分组「自动选择」包含它自身，形成循环而被跳过。
	if len(cfg.Proxies) != 4 || byName["东京 2"] == nil || byName["自动选择 2"] == nil || byName["落地"] != nil {
		t.Fatalf("proxies: %v", cfg.Proxies)
	}
	if byName["家宽"]["dialer-proxy"] != "东京" || byName["东京 2"]["type"] != "trojan" {
		t.Fatalf("chain: %v", byName["家宽"])
	}
	if cfg.Proxies[0]["name"] != "东京" {
		t.Fatalf("own nodes must come first: %v", cfg.Proxies[0])
	}

	plain, _ := base64.StdEncoding.DecodeString(fetchSub(h, a.Token, "v2rayN", "").Body.String())
	links := strings.Split(string(plain), "\n")
	if len(links) != 3 || strings.Contains(string(plain), "socks") || !strings.HasSuffix(links[1], "#%E4%B8%9C%E4%BA%AC%202") {
		t.Fatalf("base64: %v", links)
	}

	// 订阅 B 不可见外部节点。
	plain, _ = base64.StdEncoding.DecodeString(fetchSub(h, b.Token, "v2rayN", "").Body.String())
	if strings.Count(string(plain), "\n") != 0 || !strings.HasPrefix(string(plain), "vless://") {
		t.Fatalf("visibility: %s", plain)
	}

	// 前置入站不在订阅范围内时，整个来源跳过。
	in2 := addInbound(t, h, n1, 8443, "备用")
	h.Store.SetSubscriptionScope(subA, false, []int64{in2})
	a, _ = h.Store.GetSubscription(subA)
	nodes, warnings, _ := h.subscriptionNodes(a)
	for _, n := range nodes {
		if n.Name == "家宽" {
			t.Fatal("chained node must be skipped when its inbound is out of scope")
		}
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings: %v", warnings)
	}
}
