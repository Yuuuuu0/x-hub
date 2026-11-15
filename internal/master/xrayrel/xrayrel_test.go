package xrayrel

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

func TestEnsureDecoupledFromFirstCallerCancel(t *testing.T) {
	payload := []byte("fake-xray-zip-content")
	sumBytes := sha256.Sum256(payload)
	wantSum := hex.EncodeToString(sumBytes[:])

	var dlHits atomic.Int32
	dlStarted := make(chan struct{})
	releaseDL := make(chan struct{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/download/v26.8.1/Xray-linux-64.zip.dgst":
			fmt.Fprintf(w, "SHA2-256= %s\n", wantSum)
		case "/download/v26.8.1/Xray-linux-64.zip":
			if dlHits.Add(1) == 1 {
				close(dlStarted)
			}
			w.WriteHeader(http.StatusOK)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			<-releaseDL
			w.Write(payload)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	svc := New(t.TempDir())
	svc.SetEndpoints(srv.URL+"/releases", srv.URL+"/download/%s/%s")

	// 首个调用方启动下载后立即取消上下文，模拟反代超时中断首个 HTTP 请求。
	ctx1, cancel1 := context.WithCancel(context.Background())
	errCh1 := make(chan error, 1)
	go func() {
		_, _, err := svc.Ensure(ctx1, "v26.8.1", "amd64")
		errCh1 <- err
	}()

	select {
	case <-dlStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("download did not start")
	}

	// 第二个调用方在下载进行中加入等待。
	type ensureRes struct {
		path string
		sum  string
		err  error
	}
	resCh2 := make(chan ensureRes, 1)
	go func() {
		p, s, err := svc.Ensure(context.Background(), "v26.8.1", "amd64")
		resCh2 <- ensureRes{p, s, err}
	}()

	// 取消首个调用方，验证其及时返回且不中断后台下载。
	cancel1()
	select {
	case err := <-errCh1:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("caller 1 err = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("caller 1 did not return promptly after cancel")
	}

	// 放行上游响应，第二个调用方应复用同一上游下载并成功拿到结果。
	close(releaseDL)
	select {
	case r2 := <-resCh2:
		if r2.err != nil {
			t.Fatalf("caller 2 failed: %v", r2.err)
		}
		if r2.sum != wantSum {
			t.Fatalf("caller 2 sum = %s, want %s", r2.sum, wantSum)
		}
		got, err := os.ReadFile(r2.path)
		if err != nil || string(got) != string(payload) {
			t.Fatalf("cached file mismatch: err=%v got=%q", err, got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("caller 2 timed out")
	}

	if hits := dlHits.Load(); hits != 1 {
		t.Fatalf("upstream zip requests = %d, want 1", hits)
	}

	// 后续调用应直接命中本地缓存。
	p3, s3, err := svc.Ensure(context.Background(), "v26.8.1", "amd64")
	if err != nil || s3 != wantSum || p3 == "" {
		t.Fatalf("cache hit failed: path=%q sum=%q err=%v", p3, s3, err)
	}
	if hits := dlHits.Load(); hits != 1 {
		t.Fatalf("expected cache hit without upstream request, got %d hits", hits)
	}
}

func TestEnsureWaiterContextCancelAndRetryAfterError(t *testing.T) {
	payload := []byte("retry-zip-content")
	sumBytes := sha256.Sum256(payload)
	wantSum := hex.EncodeToString(sumBytes[:])

	var attempt atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/download/v26.8.2/Xray-linux-64.zip.dgst":
			if attempt.Add(1) == 1 {
				http.Error(w, "upstream error", http.StatusBadGateway)
				return
			}
			fmt.Fprintf(w, "SHA2-256= %s\n", wantSum)
		case "/download/v26.8.2/Xray-linux-64.zip":
			w.Write(payload)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	svc := New(t.TempDir())
	svc.SetEndpoints(srv.URL+"/releases", srv.URL+"/download/%s/%s")

	if _, _, err := svc.Ensure(context.Background(), "v26.8.2", "amd64"); err == nil {
		t.Fatal("expected error on first failed attempt")
	}
	p, s, err := svc.Ensure(context.Background(), "v26.8.2", "amd64")
	if err != nil || s != wantSum || p == "" {
		t.Fatalf("retry after failure failed: path=%q sum=%q err=%v", p, s, err)
	}
}
