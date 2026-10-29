package api

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/Yuuuuu0/x-hub/internal/master/model"
	"github.com/Yuuuuu0/x-hub/internal/master/store"
	"github.com/Yuuuuu0/x-hub/internal/master/sub"
	"github.com/Yuuuuu0/x-hub/internal/shared/protocol"
)

// addInbound 在服务器上创建一个合法的 Reality 入站。
func addInbound(t *testing.T, h *Handler, nodeID int64, port int, remark string) int64 {
	t.Helper()
	priv, _ := model.GenerateX25519()
	spec := &model.Spec{Network: model.NetworkTCP, Security: model.SecurityReality, VLESS: &model.VLESS{Flow: model.FlowVision},
		Reality: &model.Reality{Target: "dl.google.com:443", ServerNames: []string{"dl.google.com"}, PrivateKey: priv, ShortIDs: []string{"ab"}}}
	if err := spec.Validate(model.ProtoVLESS); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(spec)
	id, err := h.Store.CreateInbound(&store.Inbound{NodeID: nodeID, Remark: remark, Enabled: true, Port: port, Protocol: model.ProtoVLESS, Spec: raw})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func fetchSub(h *Handler, token, ua, query string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/"+h.Settings.Subscription().Path+"/"+token+query, nil)
	req.Header.Set("User-Agent", ua)
	h.ServeHTTP(rec, req)
	return rec
}

// TestSubscriptionEndpoint 覆盖格式识别、入站范围、名称去重、停用与无效令牌。
func TestSubscriptionEndpoint(t *testing.T) {
	h := newTestHandler(t)
	n1, _ := h.Store.CreateNode("东京", "", "jp.example.com")
	n2, _ := h.Store.CreateNode("东京", "", "2001:db8::1")
	i1 := addInbound(t, h, n1, 443, "")
	addInbound(t, h, n2, 443, "")
	subID, _ := CreateSubscription(h.Store, "朋友A", "")
	s, _ := h.Store.GetSubscription(subID)

	rec := fetchSub(h, s.Token, "v2rayN/7.0", "")
	if rec.Code != 200 || rec.Header().Get("Subscription-Userinfo") != "upload=0; download=0" ||
		rec.Header().Get("Profile-Update-Interval") != "12" {
		t.Fatalf("base64: %d %v", rec.Code, rec.Header())
	}
	plain, err := base64.StdEncoding.DecodeString(rec.Body.String())
	if err != nil {
		t.Fatal(err)
	}
	links := strings.Split(string(plain), "\n")
	if len(links) != 2 || !strings.HasPrefix(links[0], "vless://"+s.UUID+"@jp.example.com:443") ||
		!strings.Contains(links[1], "@[2001:db8::1]:443") || !strings.HasSuffix(links[1], "#%E4%B8%9C%E4%BA%AC%202") {
		t.Fatalf("links: %v", links)
	}

	rec = fetchSub(h, s.Token, "clash-verge/v2.0 mihomo/1.19", "")
	if !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/yaml") {
		t.Fatalf("mihomo content-type: %s", rec.Header().Get("Content-Type"))
	}
	var cfg struct {
		Proxies []struct {
			Name        string `yaml:"name"`
			Flow        string `yaml:"flow"`
			RealityOpts struct {
				PublicKey string `yaml:"public-key"`
			} `yaml:"reality-opts"`
		} `yaml:"proxies"`
		ProxyGroups []struct {
			Proxies []string `yaml:"proxies"`
		} `yaml:"proxy-groups"`
	}
	if err := yaml.Unmarshal(rec.Body.Bytes(), &cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Proxies) != 2 || cfg.Proxies[1].Name != "东京 2" || cfg.Proxies[0].Flow != model.FlowVision ||
		cfg.Proxies[0].RealityOpts.PublicKey == "" || len(cfg.ProxyGroups) != 2 {
		t.Fatalf("mihomo: %+v", cfg)
	}

	if rec = fetchSub(h, s.Token, "clash-verge", "?format=base64"); !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/plain") {
		t.Fatal("format override ignored")
	}

	rec = fetchSub(h, s.Token, "SFA/1.12.0 (Android)", "")
	var sb struct {
		Outbounds []map[string]any `json:"outbounds"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &sb); err != nil || len(sb.Outbounds) != 5 || sb.Outbounds[2]["type"] != "vless" {
		t.Fatalf("sing-box: %v %s", err, rec.Body.String())
	}

	// 限定入站范围后只输出授权的入站。
	if err := h.Store.SetSubscriptionScope(subID, false, []int64{i1}); err != nil {
		t.Fatal(err)
	}
	plain, _ = base64.StdEncoding.DecodeString(fetchSub(h, s.Token, "", "").Body.String())
	if strings.Count(string(plain), "vless://") != 1 {
		t.Fatalf("scope not applied: %s", plain)
	}

	h.Store.UpdateSubscription(subID, s.Name, "", false)
	if rec = fetchSub(h, s.Token, "", ""); rec.Code != 404 {
		t.Fatalf("disabled subscription must 404, got %d", rec.Code)
	}
	if rec = fetchSub(h, "nope", "", ""); rec.Code != 404 {
		t.Fatalf("unknown token must 404, got %d", rec.Code)
	}
}

// TestNodeNameVars 覆盖地区（检测与手动指定）、按计费方式计算的剩余流量与剩余天数。
func TestNodeNameVars(t *testing.T) {
	h := newTestHandler(t)
	id, _ := h.Store.CreateNode("东京", "", "jp.example.com")
	h.Store.UpdateNodeRuntime(id, "dev", "", nil, protocol.HostInfo{Country: "JP"}, "1.2.3.4")
	n, _ := h.Store.GetNode(id)
	if v := h.nameVars()(n); v.Flag != "🇯🇵" || v.Region != "日本" || v.Traffic != "" || v.Days != "" {
		t.Fatalf("detected: %+v", v)
	}
	expire := time.Now().Add(69*24*time.Hour - time.Hour).Unix()
	h.Store.UpdateNode(id, &store.NodeSettings{Name: "东京", PublicHost: "jp.example.com", NetResetDay: 1, NetQuota: 2 << 40,
		NetCountMode: "tx", Country: "HK", Region: "香港 CMI", ExpireAt: expire})
	n, _ = h.Store.GetNode(id)
	if v := h.nameVars()(n); v.Flag != "🇭🇰" || v.Region != "香港 CMI" || v.Traffic != "2TB" || v.Days != "69D" {
		t.Fatalf("manual: %+v", v)
	}
	in := &store.Inbound{Remark: "acck", Port: 443}
	if got := sub.Target(n, in, h.Settings.Subscription().NameTemplate, h.nameVars()(n)).Name; got != "🇭🇰 香港 CMI-acck|📊2TB|⏳69D" {
		t.Fatalf("name: %q", got)
	}
}
