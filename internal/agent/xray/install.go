package xray

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// installFiles 为从官方发布包中提取的文件。
var installFiles = map[string]os.FileMode{"xray": 0o755, "geoip.dat": 0o644, "geosite.dat": 0o644}

// Fetcher 下载指定版本安装包，返回内容流与期望的 SHA-256。
type Fetcher func(ctx context.Context, tag string) (body io.ReadCloser, sha256Hex string, err error)

// Install 下载并安装指定版本：校验摘要与版本号后替换二进制并启动；
// 若新版本启动失败则回滚到旧版本。安装视为期望运行。
func (m *Manager) Install(ctx context.Context, tag string, fetch Fetcher) error {
	staging := filepath.Join(m.dir, "staging")
	os.RemoveAll(staging)
	defer os.RemoveAll(staging)
	if err := m.download(ctx, tag, fetch, staging); err != nil {
		return err
	}
	if got := detectVersion(filepath.Join(staging, "xray")); got != tag {
		return fmt.Errorf("安装包版本不符：期望 %s，实际 %q", tag, got)
	}

	m.ops.Lock()
	defer m.ops.Unlock()
	backup := filepath.Join(m.dir, "bin.bak")
	os.RemoveAll(backup)
	m.stopProc()
	hadOld := m.Status().Version != ""
	if hadOld {
		if err := os.Rename(m.binDir(), backup); err != nil {
			return err
		}
	} else {
		os.RemoveAll(m.binDir())
	}
	if err := os.Rename(staging, m.binDir()); err != nil {
		m.restore(backup, hadOld)
		return err
	}
	m.setVersion(tag)
	if err := m.setEnabled(true); err != nil {
		return err
	}
	err := m.startProc()
	if err == nil {
		err = m.waitStable(3 * time.Second)
	}
	if err != nil {
		m.log.Warn("xray install failed, rolling back", "version", tag, "err", err)
		m.stopProc()
		m.restore(backup, hadOld)
		if hadOld {
			m.startProc()
		}
		return fmt.Errorf("新版本启动失败，已回滚：%w", err)
	}
	os.RemoveAll(backup)
	m.log.Info("xray installed", "version", tag)
	return nil
}

// restore 把备份目录还原为当前二进制目录。
func (m *Manager) restore(backup string, hadOld bool) {
	os.RemoveAll(m.binDir())
	if hadOld {
		os.Rename(backup, m.binDir())
	} else {
		os.MkdirAll(m.binDir(), 0o750)
	}
	m.setVersion(detectVersion(m.binPath()))
}

func (m *Manager) setVersion(v string) {
	m.mu.Lock()
	m.version = v
	m.mu.Unlock()
	m.notify()
}

// waitStable 等待 d 时长，期间进程退出视为启动失败。
func (m *Manager) waitStable(d time.Duration) error {
	m.mu.Lock()
	done := m.done
	m.mu.Unlock()
	select {
	case <-done:
		m.mu.Lock()
		defer m.mu.Unlock()
		return errors.New(m.lastErr)
	case <-time.After(d):
		return nil
	}
}

// download 下载安装包到临时文件、校验摘要并解压所需文件到 dst。
func (m *Manager) download(ctx context.Context, tag string, fetch Fetcher, dst string) error {
	body, want, err := fetch(ctx, tag)
	if err != nil {
		return err
	}
	defer body.Close()
	tmp, err := os.CreateTemp(m.dir, "xray-*.zip")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), body)
	if err != nil {
		return fmt.Errorf("下载中断：%w", err)
	}
	if got := hex.EncodeToString(h.Sum(nil)); want == "" || got != want {
		return fmt.Errorf("安装包校验失败：期望 %s，实际 %s", want, got)
	}
	zr, err := zip.NewReader(tmp, n)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, 0o750); err != nil {
		return err
	}
	found := 0
	for _, f := range zr.File {
		mode, ok := installFiles[f.Name]
		if !ok {
			continue
		}
		if err := extract(f, filepath.Join(dst, f.Name), mode); err != nil {
			return err
		}
		found++
	}
	if found != len(installFiles) {
		return errors.New("安装包内容不完整")
	}
	return nil
}

func extract(f *zip.File, dst string, mode os.FileMode) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, rc); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
