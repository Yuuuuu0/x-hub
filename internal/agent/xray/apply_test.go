//go:build unix

package xray

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeXray 模拟 Xray：-test 总是通过；配置中含 FAILSTART 时启动后立即退出（模拟端口被占用），否则常驻。
const fakeXray = `#!/bin/sh
case "$1" in
version) echo "Xray 1.8.0 (Xray, Penetrates Everything.)" ;;
run)
  if [ "$2" = "-test" ]; then exit 0; fi
  if grep -q FAILSTART "$3"; then echo "bind: address already in use"; exit 1; fi
  exec sleep 600
  ;;
esac
`

func newFakeManager(t *testing.T) *Manager {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bin", "xray"), []byte(fakeXray), 0o755); err != nil {
		t.Fatal(err)
	}
	m, err := New(dir, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if !m.Status().Installed {
		t.Fatal("fake xray not detected")
	}
	t.Cleanup(func() { m.Stop() })
	return m
}

func TestApplyRollbackWhenXrayExits(t *testing.T) {
	m := newFakeManager(t)
	if err := m.Start(); err != nil {
		t.Fatal(err)
	}
	good := []byte(`{"inbounds":[],"outbounds":[]}`)
	if err := m.Apply(1, "h1", good, true); err != nil {
		t.Fatal(err)
	}
	if rev, hash := m.Applied(); rev != 1 || hash != "h1" {
		t.Fatalf("applied %d %s", rev, hash)
	}

	bad := []byte(`{"inbounds":[],"outbounds":[],"remarks":"FAILSTART"}`)
	if err := m.Apply(2, "h2", bad, true); err == nil {
		t.Fatal("expected apply error")
	}
	if rev, hash := m.Applied(); rev != 1 || hash != "h1" {
		t.Fatalf("failed apply must not commit: %d %s", rev, hash)
	}
	if b, _ := os.ReadFile(m.configPath()); strings.Contains(string(b), "FAILSTART") {
		t.Fatal("config not restored")
	}
	if b, _ := os.ReadFile(m.masterConfigPath()); string(b) != string(good) {
		t.Fatalf("master config changed: %s", b)
	}
	if !m.running() {
		t.Fatal("xray should be restarted with previous config")
	}
	// 重新打开后已应用版本仍为上一版。
	m2, err := New(m.dir, m.log)
	if err != nil {
		t.Fatal(err)
	}
	if rev, _ := m2.Applied(); rev != 1 {
		t.Fatalf("persisted rev %d", rev)
	}
}

func TestNewRejectsCorruptState(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "xray-state.json"), []byte(`{"enabled":tr`), 0o600)
	if _, err := New(dir, slog.New(slog.NewTextHandler(io.Discard, nil))); err == nil {
		t.Fatal("expected error for corrupt state")
	}
}
