package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/Yuuuuu0/x-hub/internal/master/auth"
	"github.com/Yuuuuu0/x-hub/internal/shared/protocol"
)

func loginCookies(t *testing.T, h *Handler) []*http.Cookie {
	t.Helper()
	if err := h.Store.CreateAdmin("admin", auth.HashPassword("pass1234")); err != nil {
		t.Fatal(err)
	}
	rec, _ := postLogin(h, map[string]string{"username": "admin", "password": "pass1234"})
	if rec.Code != http.StatusOK {
		t.Fatalf("login failed: %d %s", rec.Code, rec.Body.String())
	}
	return rec.Result().Cookies()
}

func doAPI(t *testing.T, h *Handler, cookies []*http.Cookie, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(b)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, "/panel1234"+path, reader)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func enrollNode(t *testing.T, srvURL string, h *Handler, name string) (int64, string) {
	t.Helper()
	id, err := h.Store.CreateNode(name, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	token := auth.RandomToken()
	if _, err := h.Store.CreateEnrollToken(id, auth.HashToken(token), time.Hour); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(protocol.EnrollRequest{Token: token, Hostname: name})
	resp, err := http.Post(srvURL+"/agent/enroll", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("enroll %s: status %d", name, resp.StatusCode)
	}
	var er protocol.EnrollResponse
	if err := json.NewDecoder(resp.Body).Decode(&er); err != nil {
		t.Fatal(err)
	}
	return id, er.Secret
}

func dialAgentWS(t *testing.T, srvURL string, nodeID int64, secret string, hello protocol.Hello) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	wsURL := "ws" + srvURL[len("http"):] + "/agent/ws"
	ws, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": {fmt.Sprintf("Bearer %d.%s", nodeID, secret)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.CloseNow() })
	msg, _ := protocol.Marshal(protocol.TypeHello, "", "", hello)
	if err := ws.Write(ctx, websocket.MessageText, msg); err != nil {
		t.Fatal(err)
	}
	_, data, err := ws.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var env protocol.Envelope
	if err := json.Unmarshal(data, &env); err != nil || env.T != protocol.TypeWelcome {
		t.Fatalf("expect welcome, got %s err=%v", data, err)
	}
	return ws
}

func withFastInstallTimers(t *testing.T, poll, reconnect, converge time.Duration) {
	t.Helper()
	origPoll, origRec, origConv := installPollInterval, installReconnectWait, installConvergeWait
	installPollInterval, installReconnectWait, installConvergeWait = poll, reconnect, converge
	t.Cleanup(func() {
		installPollInterval, installReconnectWait, installConvergeWait = origPoll, origRec, origConv
	})
}

func setupMockReleases(t *testing.T, h *Handler, releases []map[string]any, assets map[string][]byte, failPaths map[string]bool) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/releases" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(releases)
			return
		}
		if failPaths != nil && failPaths[r.URL.Path] {
			http.Error(w, "download blocked", http.StatusBadGateway)
			return
		}
		for key, content := range assets {
			if r.URL.Path == "/download/"+key+".dgst" {
				sum := sha256.Sum256(content)
				fmt.Fprintf(w, "SHA2-256= %s\n", hex.EncodeToString(sum[:]))
				return
			}
			if r.URL.Path == "/download/"+key {
				w.Write(content)
				return
			}
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	h.Releases.SetEndpoints(srv.URL+"/releases", srv.URL+"/download/%s/%s")
}

// TestXraySettingsAndDefaultRelease 验证设置中仅保留 maxVersion，且默认版本始终取受 maxVersion 约束的最新稳定版。
func TestXraySettingsAndDefaultRelease(t *testing.T) {
	h := newTestHandler(t)
	cookies := loginCookies(t, h)

	setupMockReleases(t, h, []map[string]any{
		{"tag_name": "v26.9.1-rc1", "prerelease": true, "draft": false, "published_at": "2026-09-01T00:00:00Z"},
		{"tag_name": "v26.9.0", "prerelease": false, "draft": false, "published_at": "2026-08-30T00:00:00Z"},
		{"tag_name": "v26.8.1", "prerelease": false, "draft": false, "published_at": "2026-08-15T00:00:00Z"},
		{"tag_name": "v26.7.28", "prerelease": false, "draft": false, "published_at": "2026-07-28T00:00:00Z"},
	}, nil, nil)

	// PUT /api/settings/xray 设置 maxVersion，响应仅含 maxVersion
	rec := doAPI(t, h, cookies, http.MethodPut, "/api/settings/xray", map[string]string{"maxVersion": "v26.8.1"})
	if rec.Code != http.StatusOK {
		t.Fatalf("putXray status = %d: %s", rec.Code, rec.Body.String())
	}
	var xrayResp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &xrayResp); err != nil {
		t.Fatal(err)
	}
	if _, hasDefault := xrayResp["defaultVersion"]; hasDefault || xrayResp["maxVersion"] != "v26.8.1" || len(xrayResp) != 1 {
		t.Fatalf("unexpected putXray response: %v", xrayResp)
	}

	// GET /api/settings 中 xray 仅含 maxVersion
	rec = doAPI(t, h, cookies, http.MethodGet, "/api/settings", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("getSettings status = %d: %s", rec.Code, rec.Body.String())
	}
	var settingsResp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &settingsResp); err != nil {
		t.Fatal(err)
	}
	xrayMap, _ := settingsResp["xray"].(map[string]any)
	if _, hasDefault := xrayMap["defaultVersion"]; hasDefault || xrayMap["maxVersion"] != "v26.8.1" || len(xrayMap) != 1 {
		t.Fatalf("unexpected settings.xray: %v", xrayMap)
	}

	// GET /api/xray/releases 返回受 maxVersion 约束的最新稳定版
	rec = doAPI(t, h, cookies, http.MethodGet, "/api/xray/releases", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("xrayReleases status = %d: %s", rec.Code, rec.Body.String())
	}
	var relResp struct {
		Default  string `json:"default"`
		Releases []struct {
			Tag     string `json:"tag"`
			Allowed bool   `json:"allowed"`
		} `json:"releases"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &relResp); err != nil {
		t.Fatal(err)
	}
	if relResp.Default != "v26.8.1" {
		t.Fatalf("default = %q, want v26.8.1", relResp.Default)
	}

	// 清空 maxVersion 后 default 应回升到最新稳定版 v26.9.0（跳过 prerelease）
	rec = doAPI(t, h, cookies, http.MethodPut, "/api/settings/xray", map[string]string{"maxVersion": ""})
	if rec.Code != http.StatusOK {
		t.Fatalf("clear maxVersion status = %d: %s", rec.Code, rec.Body.String())
	}
	rec = doAPI(t, h, cookies, http.MethodGet, "/api/xray/releases", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("xrayReleases status = %d: %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &relResp); err != nil {
		t.Fatal(err)
	}
	if relResp.Default != "v26.9.0" {
		t.Fatalf("default without maxVersion = %q, want v26.9.0", relResp.Default)
	}
}

// TestBatchInstallStatusesAndReconnectConverge 覆盖：
// - 未接入节点过滤
// - 离线节点保留原版本
// - 已是目标版本 skipped
// - 断线后后台完成安装并重连收敛为 done，且立即同步 Hub 与数据库状态
// - 真实安装失败时 version 保持当前版本
func TestBatchInstallStatusesAndReconnectConverge(t *testing.T) {
	withFastInstallTimers(t, 10*time.Millisecond, 2*time.Second, 200*time.Millisecond)
	h := newTestHandler(t)
	cookies := loginCookies(t, h)
	srv := httptest.NewServer(h)
	defer srv.Close()

	setupMockReleases(t, h,
		[]map[string]any{{"tag_name": "v26.8.1", "prerelease": false, "draft": false}},
		map[string][]byte{
			"v26.8.1/Xray-linux-64.zip": []byte("zip-amd64"),
		},
		nil,
	)

	// 1. 未接入节点：不应出现在 batch-install 返回结果中
	if _, err := h.Store.CreateNode("unenrolled", "", nil); err != nil {
		t.Fatal(err)
	}

	// 2. 已接入但离线节点：应返回 offline，version 为 n.XrayVersion
	offlineID, _ := enrollNode(t, srv.URL, h, "offline-node")
	if err := h.Store.UpdateNodeXray(offlineID, "v26.7.28", nil); err != nil {
		t.Fatal(err)
	}

	// 3. 已是目标版本节点：应返回 skipped，version 为 target
	skippedID, skippedSec := enrollNode(t, srv.URL, h, "skipped-node")
	wsSkipped := dialAgentWS(t, srv.URL, skippedID, skippedSec, protocol.Hello{
		AgentVersion: "1.0",
		Host:         protocol.HostInfo{Arch: "amd64"},
		Xray:         protocol.XrayStatus{Installed: true, Version: "v26.8.1", Running: true},
	})
	go func() {
		for {
			if _, _, err := wsSkipped.Read(context.Background()); err != nil {
				return
			}
		}
	}()

	// 4. 真实安装失败节点：应返回 failed，version 保持当前版本 v26.7.28
	failID, failSec := enrollNode(t, srv.URL, h, "fail-node")
	wsFail := dialAgentWS(t, srv.URL, failID, failSec, protocol.Hello{
		AgentVersion: "1.0",
		Host:         protocol.HostInfo{Arch: "amd64"},
		Xray:         protocol.XrayStatus{Installed: true, Version: "v26.7.28", Running: true},
	})
	go func() {
		for {
			_, data, err := wsFail.Read(context.Background())
			if err != nil {
				return
			}
			var env protocol.Envelope
			if json.Unmarshal(data, &env) != nil || env.ID == "" {
				continue
			}
			msg, _ := protocol.Marshal(protocol.TypeResult, "", env.ID, protocol.Result{Error: "校验失败"})
			wsFail.Write(context.Background(), websocket.MessageText, msg)
		}
	}()

	// 5. 断线重连收敛节点：收到安装指令后断线，随后以目标版本且 Running=true 重连，应收敛为 done
	recID, recSec := enrollNode(t, srv.URL, h, "reconnect-node")
	wsRec := dialAgentWS(t, srv.URL, recID, recSec, protocol.Hello{
		AgentVersion: "1.0",
		Host:         protocol.HostInfo{Arch: "amd64"},
		Xray:         protocol.XrayStatus{Installed: true, Version: "v26.7.28", Running: true},
	})
	go func() {
		for {
			_, data, err := wsRec.Read(context.Background())
			if err != nil {
				return
			}
			var env protocol.Envelope
			if json.Unmarshal(data, &env) != nil || env.T != protocol.TypeXray {
				continue
			}
			// 模拟安装期间连接瞬断
			wsRec.CloseNow()
			time.Sleep(40 * time.Millisecond)
			// 被控端后台完成安装后重连上线并上报新版本与运行态
			wsNew := dialAgentWS(t, srv.URL, recID, recSec, protocol.Hello{
				AgentVersion: "1.0",
				Host:         protocol.HostInfo{Arch: "amd64"},
				Xray:         protocol.XrayStatus{Installed: true, Version: "v26.8.1", Enabled: true, Running: true},
			})
			for {
				if _, _, err := wsNew.Read(context.Background()); err != nil {
					return
				}
			}
		}
	}()

	rec := doAPI(t, h, cookies, http.MethodPost, "/api/xray/batch-install", map[string]string{"version": "v26.8.1"})
	if rec.Code != http.StatusOK {
		t.Fatalf("batchInstall status = %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Results []struct {
			NodeID  int64  `json:"nodeId"`
			Name    string `json:"name"`
			Version string `json:"version"`
			Status  string `json:"status"`
			Error   string `json:"error"`
		} `json:"results"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Results) != 4 {
		t.Fatalf("results count = %d (unenrolled must be filtered), got %+v", len(resp.Results), resp.Results)
	}

	byName := map[string]struct {
		Version string
		Status  string
		Error   string
	}{}
	for _, r := range resp.Results {
		byName[r.Name] = struct {
			Version string
			Status  string
			Error   string
		}{r.Version, r.Status, r.Error}
	}

	if got := byName["offline-node"]; got.Status != "offline" || got.Version != "v26.7.28" {
		t.Fatalf("offline-node = %+v, want offline / v26.7.28", got)
	}
	if got := byName["skipped-node"]; got.Status != "skipped" || got.Version != "v26.8.1" {
		t.Fatalf("skipped-node = %+v, want skipped / v26.8.1", got)
	}
	if got := byName["fail-node"]; got.Status != "failed" || got.Version != "v26.7.28" || got.Error == "" {
		t.Fatalf("fail-node = %+v, want failed / v26.7.28 with error", got)
	}
	if got := byName["reconnect-node"]; got.Status != "done" || got.Version != "v26.8.1" {
		t.Fatalf("reconnect-node = %+v, want done / v26.8.1", got)
	}

	// 验证 Hub 状态与数据库在返回后已同步更新
	if st := h.Hub.State(recID); !st.Online || st.Xray.Version != "v26.8.1" || !st.Xray.Running {
		t.Fatalf("Hub.State after converge = %+v", st)
	}
	if n, err := h.Store.GetNode(recID); err != nil || n.XrayVersion != "v26.8.1" {
		t.Fatalf("Store.GetNode after converge = %+v, err=%v", n, err)
	}
}

// TestBatchInstallPrewarmFailureAndRetryOnNewConn 验证主控预热失败时不触碰被控端，以及断线重连后未收敛时的重试路径。
func TestBatchInstallPrewarmFailureAndRetryOnNewConn(t *testing.T) {
	withFastInstallTimers(t, 10*time.Millisecond, time.Second, 40*time.Millisecond)
	h := newTestHandler(t)
	cookies := loginCookies(t, h)
	srv := httptest.NewServer(h)
	defer srv.Close()

	setupMockReleases(t, h,
		[]map[string]any{
			{"tag_name": "v26.8.1", "prerelease": false, "draft": false},
			{"tag_name": "v26.8.2", "prerelease": false, "draft": false},
		},
		map[string][]byte{
			"v26.8.2/Xray-linux-64.zip": []byte("zip-v26.8.2"),
		},
		map[string]bool{
			"/download/v26.8.1/Xray-linux-64.zip.dgst": true, // 预热失败
		},
	)

	// 1. 预热失败节点：主控应直接返回 failed，不向被控端发送 TypeXray 指令
	var prewarmFailSent atomic.Bool
	n1, s1 := enrollNode(t, srv.URL, h, "prewarm-fail")
	ws1 := dialAgentWS(t, srv.URL, n1, s1, protocol.Hello{
		AgentVersion: "1.0",
		Host:         protocol.HostInfo{Arch: "amd64"},
		Xray:         protocol.XrayStatus{Installed: true, Version: "v26.7.28", Running: true},
	})
	go func() {
		for {
			_, data, err := ws1.Read(context.Background())
			if err != nil {
				return
			}
			var env protocol.Envelope
			if json.Unmarshal(data, &env) == nil && env.T == protocol.TypeXray {
				prewarmFailSent.Store(true)
			}
		}
	}()

	rec := doAPI(t, h, cookies, http.MethodPost, "/api/xray/batch-install", map[string]string{"version": "v26.8.1"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Results []struct {
			Name    string `json:"name"`
			Version string `json:"version"`
			Status  string `json:"status"`
			Error   string `json:"error"`
		} `json:"results"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Results) != 1 || resp.Results[0].Status != "failed" || resp.Results[0].Version != "v26.7.28" || resp.Results[0].Error == "" {
		t.Fatalf("unexpected prewarm fail result: %+v", resp.Results)
	}
	if prewarmFailSent.Load() {
		t.Fatal("agent must not receive TypeXray when master prewarm fails")
	}

	// 2. 断线重连后仍为旧版本，满 installConvergeWait 后在新连接重试一次成功并立即 Sync SetXray
	var retryCount atomic.Int32
	var mu sync.Mutex
	n2, s2 := enrollNode(t, srv.URL, h, "retry-node")
	if err := h.Store.SetNodeXrayPin(n2, "v26.8.2"); err != nil {
		t.Fatal(err)
	}
	ws2 := dialAgentWS(t, srv.URL, n2, s2, protocol.Hello{
		AgentVersion: "1.0",
		Host:         protocol.HostInfo{Arch: "amd64"},
		Xray:         protocol.XrayStatus{Installed: true, Version: "v26.7.28", Running: true},
	})
	go func() {
		for {
			_, data, err := ws2.Read(context.Background())
			if err != nil {
				return
			}
			var env protocol.Envelope
			if json.Unmarshal(data, &env) != nil || env.T != protocol.TypeXray {
				continue
			}
			retryCount.Add(1)
			// 首次收到后断开且未完成安装
			ws2.CloseNow()
			time.Sleep(15 * time.Millisecond)
			// 以旧版本重连
			wsNew := dialAgentWS(t, srv.URL, n2, s2, protocol.Hello{
				AgentVersion: "1.0",
				Host:         protocol.HostInfo{Arch: "amd64"},
				Xray:         protocol.XrayStatus{Installed: true, Version: "v26.7.28", Running: true},
			})
			for {
				_, ndata, err := wsNew.Read(context.Background())
				if err != nil {
					return
				}
				var nenv protocol.Envelope
				if json.Unmarshal(ndata, &nenv) != nil || nenv.T != protocol.TypeXray {
					continue
				}
				retryCount.Add(1)
				mu.Lock()
				res := protocol.XrayStatus{Installed: true, Version: "v26.8.2", Enabled: true, Running: true}
				b, _ := json.Marshal(res)
				reply, _ := protocol.Marshal(protocol.TypeResult, "", nenv.ID, protocol.Result{OK: true, Data: b})
				wsNew.Write(context.Background(), websocket.MessageText, reply)
				mu.Unlock()
			}
		}
	}()

	rec2 := doAPI(t, h, cookies, http.MethodPost, fmt.Sprintf("/api/nodes/%d/xray", n2), protocol.XrayCommand{
		Action:  protocol.XrayInstall,
		Version: "v26.8.2",
	})
	if rec2.Code != http.StatusOK {
		t.Fatalf("xrayAction retry status = %d: %s", rec2.Code, rec2.Body.String())
	}
	if retryCount.Load() != 2 {
		t.Fatalf("expected initial + 1 retry request, got %d", retryCount.Load())
	}
	if st := h.Hub.State(n2); st.Xray.Version != "v26.8.2" || !st.Xray.Running {
		t.Fatalf("Hub.State not updated after xrayAction: %+v", st)
	}
	if node, err := h.Store.GetNode(n2); err != nil || node.XrayVersion != "v26.8.2" {
		t.Fatalf("Store.GetNode not updated after xrayAction: %+v, err=%v", node, err)
	}
}

// TestInstallEdgeCasesAndXrayActionSync 验证非 ErrOffline 超时边界收敛、连续离线超时返回 ErrOffline，以及常规启停同步 SetXray。
func TestInstallEdgeCasesAndXrayActionSync(t *testing.T) {
	withFastInstallTimers(t, 10*time.Millisecond, 60*time.Millisecond, 60*time.Millisecond)
	h := newTestHandler(t)
	cookies := loginCookies(t, h)
	srv := httptest.NewServer(h)
	defer srv.Close()

	// 1. 请求超时（非 ErrOffline）但节点状态已变为目标版本且 Running：应判定成功
	n1, s1 := enrollNode(t, srv.URL, h, "timeout-already-done")
	ws1 := dialAgentWS(t, srv.URL, n1, s1, protocol.Hello{
		AgentVersion: "1.0",
		Host:         protocol.HostInfo{Arch: "386"}, // 跳过预热
		Xray:         protocol.XrayStatus{Installed: true, Version: "v26.7.28", Running: true},
	})
	go func() {
		for {
			_, data, err := ws1.Read(context.Background())
			if err != nil {
				return
			}
			var env protocol.Envelope
			if json.Unmarshal(data, &env) != nil || env.T != protocol.TypeXray {
				continue
			}
			// 模拟被控端已完成切换并更新 Hub 状态，但响应来不及返回导致请求超时
			h.Hub.SetXray(n1, protocol.XrayStatus{Installed: true, Version: "v26.8.1", Enabled: true, Running: true})
		}
	}()
	reqCtx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	st, err := h.installXrayNode(reqCtx, n1, "v26.8.1")
	if err != nil || st.Version != "v26.8.1" || !st.Running {
		t.Fatalf("expected converge on timeout boundary, got st=%+v err=%v", st, err)
	}

	// 2. 断线后连续离线超过 installReconnectWait：返回 ErrOffline，批量结果保留原版本
	n2, s2 := enrollNode(t, srv.URL, h, "offline-timeout")
	ws2 := dialAgentWS(t, srv.URL, n2, s2, protocol.Hello{
		AgentVersion: "1.0",
		Host:         protocol.HostInfo{Arch: "386"},
		Xray:         protocol.XrayStatus{Installed: true, Version: "v26.7.28", Running: true},
	})
	go func() {
		for {
			_, data, err := ws2.Read(context.Background())
			if err != nil {
				return
			}
			var env protocol.Envelope
			if json.Unmarshal(data, &env) != nil || env.T != protocol.TypeXray {
				continue
			}
			ws2.CloseNow()
		}
	}()
	if _, err := h.installXrayNode(context.Background(), n2, "v26.8.1"); err == nil {
		t.Fatal("expected ErrOffline after exceeding installReconnectWait")
	}

	// 3. XrayStop 后立即同步 SetXray，不等待下次心跳
	n3, s3 := enrollNode(t, srv.URL, h, "stop-sync")
	ws3 := dialAgentWS(t, srv.URL, n3, s3, protocol.Hello{
		AgentVersion: "1.0",
		Host:         protocol.HostInfo{Arch: "386"},
		Xray:         protocol.XrayStatus{Installed: true, Version: "v26.8.1", Enabled: true, Running: true},
	})
	go func() {
		for {
			_, data, err := ws3.Read(context.Background())
			if err != nil {
				return
			}
			var env protocol.Envelope
			if json.Unmarshal(data, &env) != nil || env.T != protocol.TypeXray {
				continue
			}
			res, _ := json.Marshal(protocol.XrayStatus{Installed: true, Version: "v26.8.1", Enabled: false, Running: false})
			reply, _ := protocol.Marshal(protocol.TypeResult, "", env.ID, protocol.Result{OK: true, Data: res})
			ws3.Write(context.Background(), websocket.MessageText, reply)
		}
	}()
	rec := doAPI(t, h, cookies, http.MethodPost, fmt.Sprintf("/api/nodes/%d/xray", n3), protocol.XrayCommand{Action: protocol.XrayStop})
	if rec.Code != http.StatusOK {
		t.Fatalf("xrayAction stop status = %d: %s", rec.Code, rec.Body.String())
	}
	if cur := h.Hub.State(n3); cur.Xray.Running || cur.Xray.Enabled {
		t.Fatalf("expected Hub.State to reflect stopped xray immediately, got %+v", cur.Xray)
	}
}
