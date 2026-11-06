package syncer

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/Yuuuuu0/x-hub/internal/master/hub"
	"github.com/Yuuuuu0/x-hub/internal/master/store"
	"github.com/Yuuuuu0/x-hub/internal/shared/protocol"
)

// TestRetryAfterApplyFailure 在线下发失败后按退避自动重试，直到成功；Forget 后同步协程退出。
func TestRetryAfterApplyFailure(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	nodeID, err := st.CreateNode("n", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.DiscardHandler)
	var s *Syncer
	h := hub.New(st, log, hub.Hooks{
		Endpoint: func() protocol.Endpoint { return protocol.Endpoint{} },
		OnHello:  func(id int64, hello protocol.Hello) { s.OnHello(id, hello) },
	})
	s = New(st, h, log)
	s.retryBase, s.retryMax = 50*time.Millisecond, 200*time.Millisecond

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.Serve(w, r, nodeID, "127.0.0.1")
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.CloseNow() })
	hello, _ := protocol.Marshal(protocol.TypeHello, "", "", protocol.Hello{AgentVersion: "test"})
	if err := ws.Write(ctx, websocket.MessageText, hello); err != nil {
		t.Fatal(err)
	}

	// 模拟被控端：前两次下发返回失败，第三次成功。
	var applies atomic.Int32
	go func() {
		for {
			_, data, err := ws.Read(ctx)
			if err != nil {
				return
			}
			var env protocol.Envelope
			if json.Unmarshal(data, &env) != nil || env.T != protocol.TypeApply {
				continue
			}
			var apply protocol.Apply
			json.Unmarshal(env.D, &apply)
			res := protocol.Result{OK: true}
			if applies.Add(1) <= 2 {
				res = protocol.Result{Error: "boom"}
			} else {
				res.Data, _ = json.Marshal(protocol.ApplyResult{Rev: apply.Rev, Hash: apply.Hash})
			}
			msg, _ := protocol.Marshal(protocol.TypeResult, "", env.ID, res)
			ws.Write(ctx, websocket.MessageText, msg)
		}
	}()

	for {
		n, err := st.GetNode(nodeID)
		if err != nil {
			t.Fatal(err)
		}
		if n.AppliedHash != "" && s.State(n) == StateSynced {
			break
		}
		if ctx.Err() != nil {
			t.Fatalf("not synced after retries, applies=%d", applies.Load())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if n := applies.Load(); n != 3 {
		t.Fatalf("applies=%d, want 3", n)
	}

	s.Forget(nodeID)
	s.mu.Lock()
	left := len(s.workers)
	s.mu.Unlock()
	if left != 0 {
		t.Fatalf("workers left after Forget: %d", left)
	}
}

// TestNextBackoff 退避从起始值翻倍到上限，期望配置变化时重置。
func TestNextBackoff(t *testing.T) {
	s := &Syncer{retryBase: 30 * time.Second, retryMax: 5 * time.Minute}
	var d time.Duration
	var got []time.Duration
	for range 6 {
		d = s.nextBackoff(d, false)
		got = append(got, d)
	}
	want := []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute, 4 * time.Minute, 5 * time.Minute, 5 * time.Minute}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("backoff %v, want %v", got, want)
		}
	}
	if s.nextBackoff(d, true) != 30*time.Second {
		t.Fatal("config change should reset backoff")
	}
}
