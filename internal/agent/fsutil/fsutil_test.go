package fsutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFileAtomicReplace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(path, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil || string(b) != "new" {
		t.Fatalf("content %q err %v", b, err)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Fatalf("perm %v", fi.Mode().Perm())
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("temp file left behind: %v", entries)
	}
}

func TestWriteFilesFailureKeepsOriginals(t *testing.T) {
	dir := t.TempDir()
	key, cert := filepath.Join(dir, "privkey.pem"), filepath.Join(dir, "fullchain.pem")
	os.WriteFile(key, []byte("old-key"), 0o600)
	os.WriteFile(cert, []byte("old-cert"), 0o600)
	// 第二个文件所在目录不存在，写临时文件失败时第一个文件也不应被替换。
	bad := filepath.Join(dir, "missing", "fullchain.pem")
	if err := WriteFiles(File{key, []byte("new-key"), 0o600}, File{bad, []byte("new-cert"), 0o600}); err == nil {
		t.Fatal("expected error")
	}
	if b, _ := os.ReadFile(key); string(b) != "old-key" {
		t.Fatalf("key replaced on failure: %q", b)
	}
	if err := WriteFiles(File{key, []byte("new-key"), 0o600}, File{cert, []byte("new-cert"), 0o644}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(cert); string(b) != "new-cert" {
		t.Fatalf("cert %q", b)
	}
	if fi, _ := os.Stat(cert); fi.Mode().Perm() != 0o644 {
		t.Fatalf("perm %v", fi.Mode().Perm())
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 2 {
		t.Fatalf("temp file left behind: %v", entries)
	}
}
