package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"github.com/coder/websocket"

	"github.com/Yuuuuu0/x-hub/internal/master/accounting"
	"github.com/Yuuuuu0/x-hub/internal/master/auth"
	"github.com/Yuuuuu0/x-hub/internal/master/hub"
	"github.com/Yuuuuu0/x-hub/internal/master/server"
	"github.com/Yuuuuu0/x-hub/internal/master/settings"
	"github.com/Yuuuuu0/x-hub/internal/master/store"
	"github.com/Yuuuuu0/x-hub/internal/master/syncer"
	"github.com/Yuuuuu0/x-hub/internal/master/xrayrel"
	"github.com/Yuuuuu0/x-hub/internal/shared/protocol"
)

func newTestHandler(t *testing.T) *Handler {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	cfg, _, err := settings.Load(st, func() string { return "panel1234" })
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.DiscardHandler)
	h := &Handler{
		Store:    st,
		Settings: cfg,
		Certs:    server.NewCerts(filepath.Join(dir, "tls"), cfg),
		Releases: xrayrel.New(filepath.Join(dir, "cache")),
		Log:      log,
		Web:      fs.FS(fstest.MapFS{"index.html": {Data: []byte(`<base href="/">`)}}),
	}
	h.Hub = hub.New(st, log, hub.Hooks{Endpoint: h.Endpoint})
	h.Syncer = syncer.New(st, h.Hub, log)
	h.Accounting = accounting.New(st, cfg, h.Syncer, log)
	h.Hub.SetTrafficHooks(h.Accounting.LastSeq, h.Accounting.Ingest)
	h.Init()
	return h
}

// TestEnrollAndConnect 覆盖接入令牌一次性消费、凭据校验与 WebSocket 握手。
func TestEnrollAndConnect(t *testing.T) {
	h := newTestHandler(t)
	srv := httptest.NewServer(h)
	defer srv.Close()

	id, _ := h.Store.CreateNode("n1", "", "")
	token := auth.RandomToken()
	if _, err := h.Store.CreateEnrollToken(id, auth.HashToken(token), time.Hour); err != nil {
		t.Fatal(err)
	}

	enroll := func() (*http.Response, protocol.EnrollResponse) {
		body, _ := json.Marshal(protocol.EnrollRequest{Token: token, Hostname: "h"})
		resp, err := http.Post(srv.URL+"/agent/enroll", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var er protocol.EnrollResponse
		json.NewDecoder(resp.Body).Decode(&er)
		return resp, er
	}
	resp, er := enroll()
	if resp.StatusCode != http.StatusOK || er.NodeID != id || er.Secret == "" {
		t.Fatalf("enroll: %d %+v", resp.StatusCode, er)
	}
	if resp, _ := enroll(); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("token reuse must be rejected, got %d", resp.StatusCode)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	wsURL := "ws" + srv.URL[len("http"):] + "/agent/ws"

	_, bad, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer 1.wrong"}}})
	if err == nil || bad == nil || bad.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad secret must get 401, err=%v", err)
	}

	ws, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer 1." + er.Secret}}})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	hello, _ := protocol.Marshal(protocol.TypeHello, "", "", protocol.Hello{AgentVersion: "t", Host: protocol.HostInfo{Arch: "amd64"}, Xray: protocol.XrayStatus{Installed: true, Version: "v26.7.28"}})
	if err := ws.Write(ctx, websocket.MessageText, hello); err != nil {
		t.Fatal(err)
	}
	_, data, err := ws.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var env protocol.Envelope
	if json.Unmarshal(data, &env); env.T != protocol.TypeWelcome {
		t.Fatalf("expect welcome, got %s", env.T)
	}
	if !h.Hub.State(id).Online {
		t.Fatal("node should be online after handshake")
	}
	n, _ := h.Store.GetNode(id)
	if n.XrayVersion != "v26.7.28" || n.AgentVersion != "t" {
		t.Fatalf("runtime not persisted: %+v", n)
	}
}

// TestRouting 覆盖面板路径以外一律 404、SPA 回退与 base 改写。
func TestRouting(t *testing.T) {
	h := newTestHandler(t)
	cases := map[string]int{"/": 404, "/random": 404, "/panel1234": 302, "/panel1234/nodes": 200, "/panel1234/api/nodes": 401}
	for path, want := range cases {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != want {
			t.Errorf("%s: got %d want %d", path, rec.Code, want)
		}
		if path == "/panel1234/nodes" && !bytes.Contains(rec.Body.Bytes(), []byte(`<base href="/panel1234/">`)) {
			t.Errorf("base href not rewritten: %s", rec.Body.String())
		}
	}
}
