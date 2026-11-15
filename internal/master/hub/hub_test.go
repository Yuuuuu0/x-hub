package hub

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/Yuuuuu0/x-hub/internal/master/store"
	"github.com/Yuuuuu0/x-hub/internal/shared/protocol"
)

// testEnv 为一个挂载了 Hub 的测试服务器，所有连接都以同一台服务器的身份接入。
type testEnv struct {
	hub    *Hub
	nodeID int64
	url    string
}

func newTestEnv(t *testing.T, onHello func(*Hub, int64)) *testEnv {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	nodeID, err := st.CreateNode("n", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	env := &testEnv{nodeID: nodeID}
	hooks := Hooks{Endpoint: func() protocol.Endpoint { return protocol.Endpoint{} }}
	if onHello != nil {
		hooks.OnHello = func(id int64, _ protocol.Hello) { onHello(env.hub, id) }
	}
	env.hub = New(st, slog.New(slog.DiscardHandler), hooks)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		env.hub.Serve(w, r, nodeID, "127.0.0.1")
	}))
	t.Cleanup(srv.Close)
	env.url = "ws" + strings.TrimPrefix(srv.URL, "http")
	return env
}

// dial 以被控端身份接入并完成握手，返回连接。
func (e *testEnv) dial(t *testing.T) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, e.url, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.CloseNow() })
	msg, _ := protocol.Marshal(protocol.TypeHello, "", "", protocol.Hello{AgentVersion: "test"})
	if err := ws.Write(ctx, websocket.MessageText, msg); err != nil {
		t.Fatal(err)
	}
	_, data, err := ws.Read(ctx)
	var env protocol.Envelope
	if err != nil || json.Unmarshal(data, &env) != nil || env.T != protocol.TypeWelcome {
		t.Fatalf("welcome: %s %v", data, err)
	}
	return ws
}

// serveResults 持续读取指令并以成功结果应答，直到连接关闭。
func serveResults(ws *websocket.Conn) {
	for {
		_, data, err := ws.Read(context.Background())
		if err != nil {
			return
		}
		var env protocol.Envelope
		if json.Unmarshal(data, &env) != nil || env.ID == "" {
			continue
		}
		msg, _ := protocol.Marshal(protocol.TypeResult, "", env.ID, protocol.Result{OK: true})
		ws.Write(context.Background(), websocket.MessageText, msg)
	}
}

// waitRegistered 等待 Hub 中登记的连接变为不同于 prev 的新连接。
func waitRegistered(t *testing.T, h *Hub, nodeID int64, prev *conn) *conn {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if c := h.get(nodeID); c != nil && c != prev {
			return c
		}
		if time.Now().After(deadline) {
			t.Fatal("connection not registered")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestOnHelloAfterRegister 握手回调触发时连接已完成注册，回调内可立即查询到在线状态。
func TestOnHelloAfterRegister(t *testing.T) {
	online := make(chan bool, 1)
	env := newTestEnv(t, func(h *Hub, id int64) { online <- h.State(id).Online })
	env.dial(t)
	select {
	case ok := <-online:
		if !ok {
			t.Fatal("OnHello fired before register")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("OnHello not called")
	}
}

// TestReplaceAndKickDoNotBlock 旧连接半开（不再读取）时，新连接替换与 Kick 都应立即完成，不等待关闭握手。
func TestReplaceAndKickDoNotBlock(t *testing.T) {
	env := newTestEnv(t, nil)
	env.dial(t) // 旧连接握手后不再读取，模拟半开
	first := waitRegistered(t, env.hub, env.nodeID, nil)

	start := time.Now()
	ws := env.dial(t)
	go serveResults(ws)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	// 新连接注册后写协程才会启动，替换被阻塞时请求会超时。
	if err := env.hub.Request(ctx, env.nodeID, protocol.TypeXray, protocol.XrayCommand{Action: protocol.XrayStart}, nil); err != nil {
		t.Fatalf("request via new conn: %v", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("replacement took %s", d)
	}

	second := waitRegistered(t, env.hub, env.nodeID, first)
	env.dial(t) // 再换一条不读取的连接后 Kick
	waitRegistered(t, env.hub, env.nodeID, second)
	start = time.Now()
	env.hub.Kick(env.nodeID)
	if d := time.Since(start); d > time.Second {
		t.Fatalf("kick took %s", d)
	}
}

// TestSetXray 验证 SetXray 在连接在线时立即更新内存态与数据库版本，离线时静默忽略。
func TestSetXray(t *testing.T) {
	env := newTestEnv(t, nil)

	// 离线调用不应报错或更新数据库
	env.hub.SetXray(env.nodeID, protocol.XrayStatus{Installed: true, Version: "v26.8.1", Running: true})
	if st := env.hub.State(env.nodeID); st.Online || st.Xray.Version != "" {
		t.Fatalf("offline SetXray should be no-op, got %+v", st)
	}
	n, err := env.hub.store.GetNode(env.nodeID)
	if err != nil {
		t.Fatal(err)
	}
	if n.XrayVersion != "" {
		t.Fatalf("offline SetXray should not update store, got %q", n.XrayVersion)
	}

	// 在线调用应同步更新 State.Xray、xrayVer、lastTouch 与数据库
	ws := env.dial(t)
	defer ws.CloseNow()
	c := waitRegistered(t, env.hub, env.nodeID, nil)

	want := protocol.XrayStatus{Installed: true, Version: "v26.8.1", Enabled: true, Running: true, StartedAt: 12345}
	env.hub.SetXray(env.nodeID, want)

	st := env.hub.State(env.nodeID)
	if st.Xray != want {
		t.Fatalf("State.Xray = %+v, want %+v", st.Xray, want)
	}
	c.smu.RLock()
	gotVer := c.xrayVer
	c.smu.RUnlock()
	if gotVer != "v26.8.1" {
		t.Fatalf("conn.xrayVer = %q, want %q", gotVer, "v26.8.1")
	}
	n, err = env.hub.store.GetNode(env.nodeID)
	if err != nil {
		t.Fatal(err)
	}
	if n.XrayVersion != "v26.8.1" {
		t.Fatalf("store.XrayVersion = %q, want %q", n.XrayVersion, "v26.8.1")
	}

	// 同版本仅运行态变化时，应更新 State.Xray 但不重置版本
	stopped := protocol.XrayStatus{Installed: true, Version: "v26.8.1", Enabled: false, Running: false}
	env.hub.SetXray(env.nodeID, stopped)
	if st := env.hub.State(env.nodeID); st.Xray != stopped {
		t.Fatalf("State.Xray after stop = %+v, want %+v", st.Xray, stopped)
	}
}
