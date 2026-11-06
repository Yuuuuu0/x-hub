package app

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/Yuuuuu0/x-hub/internal/master/store"
)

// TestRestore 恢复时不修改用户提供的备份文件，原数据库连同 -wal/-shm 一起改名保留。
func TestRestore(t *testing.T) {
	src := filepath.Join(t.TempDir(), "backup.db")
	st, err := store.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CreateAdmin("admin", "hash"); err != nil {
		t.Fatal(err)
	}
	st.Close()
	orig, _ := os.ReadFile(src)

	dataDir := t.TempDir()
	db := filepath.Join(dataDir, "xhub.db")
	for _, s := range dbFiles {
		os.WriteFile(db+s, []byte("old"+s), 0o600)
	}
	if err := Restore(dataDir, src); err != nil {
		t.Fatal(err)
	}

	if after, _ := os.ReadFile(src); !bytes.Equal(orig, after) {
		t.Fatal("backup file modified")
	}
	if _, err := os.Stat(src + "-wal"); err == nil {
		t.Fatal("backup file opened in place")
	}
	baks, _ := filepath.Glob(db + ".bak-*")
	if len(baks) != 3 {
		t.Fatalf("bak files: %v", baks)
	}
	for _, s := range []string{"-wal", "-shm"} {
		matches, _ := filepath.Glob(db + ".bak-*" + s)
		if len(matches) != 1 {
			t.Fatalf("missing bak%s: %v", s, baks)
		}
		if data, _ := os.ReadFile(matches[0]); string(data) != "old"+s {
			t.Fatalf("bak%s content %q", s, data)
		}
	}
	if left, _ := filepath.Glob(db + ".restore*"); len(left) != 0 {
		t.Fatalf("temp files left: %v", left)
	}
	restored, err := store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if _, err := restored.FirstAdmin(); err != nil {
		t.Fatal(err)
	}
}
