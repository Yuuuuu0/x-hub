package conn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/Yuuuuu0/x-hub/internal/agent/config"
	"github.com/Yuuuuu0/x-hub/internal/agent/xray"
	"github.com/Yuuuuu0/x-hub/internal/shared/protocol"
)

func TestNextBackoff(t *testing.T) {
	other := errors.New("dial failed")
	cases := []struct {
		name      string
		backoff   time.Duration
		connected bool
		alive     time.Duration
		err       error
		wait      time.Duration
		next      time.Duration
	}{
		{"stable session resets", 32 * time.Second, true, time.Minute, nil, time.Second, time.Second},
		{"short session keeps backing off", 4 * time.Second, true, time.Second, nil, 4 * time.Second, 8 * time.Second},
		{"dial failure backs off", 4 * time.Second, false, 0, other, 4 * time.Second, 8 * time.Second},
		{"capped", maxBackoff, true, time.Second, nil, maxBackoff, maxBackoff},
		{"unauthorized", 2 * time.Second, false, 0, errUnauthorized, unauthBackoff, 2 * time.Second},
	}
	for _, tc := range cases {
		wait, next := nextBackoff(tc.backoff, tc.connected, tc.alive, tc.err)
		if wait != tc.wait || next != tc.next {
			t.Errorf("%s: got wait=%v next=%v, want %v %v", tc.name, wait, next, tc.wait, tc.next)
		}
	}
}

func TestFirstChunkBySize(t *testing.T) {
	mk := func(seq int64, users int) protocol.TrafficBatch {
		b := protocol.TrafficBatch{Seq: seq, Hour: 3600, Users: map[string][2]int64{}}
		for i := range users {
			b.Users[fmt.Sprintf("s%d.i%d", i, seq)] = [2]int64{1 << 40, 1 << 40}
		}
		return b
	}
	var batches []protocol.TrafficBatch
	for i := range 50 {
		batches = append(batches, mk(int64(i+1), 100))
	}
	const limit = 32 << 10
	chunk := firstChunk(batches, limit, maxBatches)
	if len(chunk) == 0 || len(chunk) == len(batches) {
		t.Fatalf("chunk size %d", len(chunk))
	}
	msg, _ := protocol.Marshal(protocol.TypeTraffic, "", "", protocol.Traffic{Batches: chunk})
	if len(msg) > limit {
		t.Fatalf("message %d bytes exceeds limit", len(msg))
	}
	next, _ := protocol.Marshal(protocol.TypeTraffic, "", "", protocol.Traffic{Batches: batches[:len(chunk)+1]})
	if len(next) <= limit-256 {
		t.Fatalf("chunk not maximal: %d batches, next would be %d bytes", len(chunk), len(next))
	}

	// 条数上限同样生效。
	if got := firstChunk(batches, 1<<30, 7); len(got) != 7 {
		t.Fatalf("count limit: %d", len(got))
	}
	// 首个批次单独超限时仍返回它，避免停滞。
	if got := firstChunk([]protocol.TrafficBatch{mk(1, 1000), mk(2, 1)}, 1024, maxBatches); len(got) != 1 || got[0].Seq != 1 {
		t.Fatalf("oversized first: %d", len(got))
	}
}

// 耗时的 Xray 安装不应阻塞读循环，且会话断开后安装不应被取消。
func TestReadLoopRunsSlowCommandsAsync(t *testing.T) {
	release := make(chan struct{})
	dlStarted := make(chan struct{})
	dlResult := make(chan bool, 1) // 下载请求是否在放行前被取消
	logsReplied := make(chan error, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/agent/dl/xray/", func(w http.ResponseWriter, r *http.Request) {
		close(dlStarted)
		select {
		case <-release:
			dlResult <- false
		case <-r.Context().Done():
			dlResult <- true
		}
		http.Error(w, `{"error":"gone"}`, http.StatusNotFound)
	})
	mux.HandleFunc("/agent/ws", func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer ws.CloseNow()
		ctx := r.Context()
		send := func(typ, id string, payload any) {
			msg, _ := protocol.Marshal(typ, id, "", payload)
			ws.Write(ctx, websocket.MessageText, msg)
		}
		send(protocol.TypeXray, "install", protocol.XrayCommand{Action: protocol.XrayInstall, Version: "v1.8.0"})
		<-dlStarted
		send(protocol.TypeLogs, "logs", protocol.LogsRequest{Lines: 10})
		rctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		for {
			_, data, err := ws.Read(rctx)
			if err != nil {
				logsReplied <- err
				return
			}
			var env protocol.Envelope
			if json.Unmarshal(data, &env) == nil && env.Re == "logs" {
				logsReplied <- nil
				break
			}
		}
		// 保持连接直到客户端断开。
		ws.Read(ctx)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	xm, err := xray.New(t.TempDir(), log)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.New(filepath.Join(t.TempDir(), "agent.json"))
	cfg.Endpoints = []protocol.Endpoint{{URL: srv.URL}}
	c := &Client{cfg: cfg, xray: xm, log: log}

	root, rootCancel := context.WithCancel(context.Background())
	defer rootCancel()
	sctx, scancel := context.WithCancel(root)
	ws, _, err := websocket.Dial(sctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/agent/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	loopDone := make(chan error, 1)
	go func() { loopDone <- c.readLoop(sctx, root, ws) }()

	select {
	case err := <-logsReplied:
		if err != nil {
			t.Fatalf("read loop blocked by install: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for logs reply")
	}

	// 模拟断线：会话结束后安装仍应继续。
	scancel()
	<-loopDone
	time.Sleep(200 * time.Millisecond)
	close(release)
	if canceled := <-dlResult; canceled {
		t.Fatal("install was canceled by session disconnect")
	}
	time.Sleep(200 * time.Millisecond) // 等安装失败返回，避免与临时目录清理竞争
}
