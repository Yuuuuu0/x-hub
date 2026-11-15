package xray

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func makeFakeXrayZip(t *testing.T, tag string) ([]byte, string) {
	t.Helper()
	ver := strings.TrimPrefix(tag, "v")
	script := fmt.Sprintf(`#!/bin/sh
if [ "$1" = "version" ]; then
  echo "Xray %s (Xray, Penetrates Everything.)"
  exit 0
fi
if [ "$1" = "run" ] && [ "$2" = "-test" ]; then
  exit 0
fi
exec sleep 60
`, ver)

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	files := map[string]string{
		"xray":        script,
		"geoip.dat":   "geoip",
		"geosite.dat": "geosite",
	}
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(w, body); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(buf.Bytes())
	return buf.Bytes(), hex.EncodeToString(sum[:])
}

func assertNoStagingDirs(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "staging") {
			t.Fatalf("leftover staging entry found: %s", e.Name())
		}
	}
}

func TestInstallConcurrentIdempotentAndStagingCleanup(t *testing.T) {
	origCheck := startCheck
	startCheck = 50 * time.Millisecond
	t.Cleanup(func() { startCheck = origCheck })

	dir := t.TempDir()
	m, err := New(dir, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Stop() })

	var fetchCalls atomic.Int32
	fetchStarted := make(chan struct{})
	releaseFetch := make(chan struct{})

	fetcher := func(ctx context.Context, tag string) (io.ReadCloser, string, error) {
		data, sum := makeFakeXrayZip(t, tag)
		if fetchCalls.Add(1) == 1 {
			close(fetchStarted)
			<-releaseFetch
		}
		return io.NopCloser(bytes.NewReader(data)), sum, nil
	}

	// 并发对同一版本发起两次 Install：第一个完成后第二个在锁内命中幂等分支，不重复下载
	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		errs[0] = m.Install(context.Background(), "v26.8.1", fetcher)
	}()
	<-fetchStarted
	go func() {
		defer wg.Done()
		errs[1] = m.Install(context.Background(), "v26.8.1", fetcher)
	}()
	// 确保第二个协程已进入等待 installMu 的状态再放行
	time.Sleep(20 * time.Millisecond)
	close(releaseFetch)
	wg.Wait()

	for i, e := range errs {
		if e != nil {
			t.Fatalf("concurrent install[%d] failed: %v", i, e)
		}
	}
	if calls := fetchCalls.Load(); calls != 1 {
		t.Fatalf("fetchCalls = %d, want 1 (second call must be idempotent)", calls)
	}
	if st := m.Status(); !st.Installed || st.Version != "v26.8.1" || !st.Running {
		t.Fatalf("unexpected status after concurrent install: %+v", st)
	}

	// 重复安装相同运行中版本：直接返回 nil 且不触发下载
	if err := m.Install(context.Background(), "v26.8.1", fetcher); err != nil {
		t.Fatalf("repeat install failed: %v", err)
	}
	if calls := fetchCalls.Load(); calls != 1 {
		t.Fatalf("repeat install should not fetch, got %d calls", calls)
	}

	// 并发切换不同版本：由 installMu 串行化，各自使用独立 staging-* 目录且退出后全部清理
	simpleFetcher := func(ctx context.Context, tag string) (io.ReadCloser, string, error) {
		data, sum := makeFakeXrayZip(t, tag)
		return io.NopCloser(bytes.NewReader(data)), sum, nil
	}
	wg.Add(2)
	var errA, errB error
	go func() {
		defer wg.Done()
		errA = m.Install(context.Background(), "v26.8.2", simpleFetcher)
	}()
	go func() {
		defer wg.Done()
		errB = m.Install(context.Background(), "v26.8.3", simpleFetcher)
	}()
	wg.Wait()
	if errA != nil || errB != nil {
		t.Fatalf("concurrent different-tag install failed: errA=%v errB=%v", errA, errB)
	}
	st := m.Status()
	if !st.Running || (st.Version != "v26.8.2" && st.Version != "v26.8.3") {
		t.Fatalf("unexpected final status: %+v", st)
	}
	assertNoStagingDirs(t, dir)
	if _, err := os.Stat(filepath.Join(dir, "bin.bak")); !os.IsNotExist(err) {
		t.Fatalf("bin.bak should be cleaned up, err=%v", err)
	}
}
