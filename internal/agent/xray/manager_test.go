package xray

import (
	"io"
	"log/slog"
	"strings"
	"testing"
)

// Xray 解析相对证书路径时不看工作目录，只看 XRAY_LOCATION_CERT（缺省为可执行文件目录），
// 必须指向数据目录才能找到被控端写入 certs/<域名>/ 的 ACME 证书。
func TestCommandResolvesCertsFromDataDir(t *testing.T) {
	dir := t.TempDir()
	m, err := New(dir, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	var got string
	for _, kv := range m.command("version").Env {
		if v, ok := strings.CutPrefix(kv, "XRAY_LOCATION_CERT="); ok {
			got = v // 与 exec 一致，重复键以最后一个为准
		}
	}
	if got != dir {
		t.Fatalf("XRAY_LOCATION_CERT = %q, want %q", got, dir)
	}
}
