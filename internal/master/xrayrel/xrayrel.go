// Package xrayrel 从 GitHub 获取 Xray 发布列表，并下载、校验、缓存安装包供被控端中转下载。
package xrayrel

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/Yuuuuu0/x-hub/internal/shared/xraycap"
)

const (
	releasesURL = "https://api.github.com/repos/XTLS/Xray-core/releases?per_page=50"
	downloadURL = "https://github.com/XTLS/Xray-core/releases/download/%s/%s"
	listTTL     = 30 * time.Minute
)

// assetNames 为各架构对应的官方发布包名。
var assetNames = map[string]string{
	"amd64": "Xray-linux-64.zip",
	"arm64": "Xray-linux-arm64-v8a.zip",
}

var sha256Re = regexp.MustCompile(`(?m)^SHA2-256=\s*([0-9a-f]{64})`)

// Release 为一个 Xray 发布版本。
type Release struct {
	Tag         string    `json:"tag"`
	Prerelease  bool      `json:"prerelease"`
	PublishedAt time.Time `json:"publishedAt"`
}

// Service 提供发布列表查询与安装包缓存。
type Service struct {
	dir    string
	client *http.Client

	mu        sync.Mutex
	list      []Release
	fetchedAt time.Time

	dlMu  sync.Mutex
	locks map[string]*sync.Mutex
}

// New 创建服务，安装包缓存于 dir。
func New(dir string) *Service {
	return &Service{dir: dir, client: &http.Client{Timeout: 5 * time.Minute}, locks: map[string]*sync.Mutex{}}
}

// SupportedArch 判断是否提供该架构的安装包。
func SupportedArch(arch string) bool {
	_, ok := assetNames[arch]
	return ok
}

// List 返回按发布时间倒序的版本列表，结果缓存 30 分钟；GitHub 请求失败时回退到旧缓存。
func (s *Service) List(ctx context.Context) ([]Release, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.list != nil && time.Since(s.fetchedAt) < listTTL {
		return s.list, nil
	}
	list, err := s.fetch(ctx)
	if err != nil {
		if s.list != nil {
			return s.list, nil
		}
		return nil, err
	}
	s.list, s.fetchedAt = list, time.Now()
	return list, nil
}

func (s *Service) fetch(ctx context.Context) ([]Release, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, releasesURL, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("获取 Xray 版本列表失败：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("获取 Xray 版本列表失败：GitHub 返回 %s", resp.Status)
	}
	var raw []struct {
		TagName     string    `json:"tag_name"`
		Prerelease  bool      `json:"prerelease"`
		Draft       bool      `json:"draft"`
		PublishedAt time.Time `json:"published_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	list := make([]Release, 0, len(raw))
	for _, r := range raw {
		if r.Draft || !xraycap.IsValidTag(r.TagName) {
			continue
		}
		list = append(list, Release{Tag: r.TagName, Prerelease: r.Prerelease, PublishedAt: r.PublishedAt})
	}
	return list, nil
}

// LatestStable 返回不高于 max（为空不限制）的最新稳定版。
func LatestStable(list []Release, max string) (string, bool) {
	maxV, hasMax := xraycap.ParseVersion(max)
	for _, r := range list {
		v, _ := xraycap.ParseVersion(r.Tag)
		if r.Prerelease || (hasMax && v.Compare(maxV) > 0) {
			continue
		}
		return r.Tag, true
	}
	return "", false
}

// Ensure 确保指定版本与架构的安装包已下载并通过 SHA-256 校验，返回本地路径与摘要。
func (s *Service) Ensure(ctx context.Context, tag, arch string) (path, sum string, err error) {
	asset, ok := assetNames[arch]
	if !ok || !xraycap.IsValidTag(tag) {
		return "", "", fmt.Errorf("不支持的版本或架构：%s %s", tag, arch)
	}
	path = filepath.Join(s.dir, tag, asset)

	s.dlMu.Lock()
	lock := s.locks[path]
	if lock == nil {
		lock = &sync.Mutex{}
		s.locks[path] = lock
	}
	s.dlMu.Unlock()
	lock.Lock()
	defer lock.Unlock()

	if b, err := os.ReadFile(path + ".sha256"); err == nil {
		if _, err := os.Stat(path); err == nil {
			return path, string(b), nil
		}
	}
	want, err := s.fetchDigest(ctx, tag, asset)
	if err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", "", err
	}
	got, err := s.download(ctx, fmt.Sprintf(downloadURL, tag, asset), path+".part")
	if err != nil {
		os.Remove(path + ".part")
		return "", "", err
	}
	if got != want {
		os.Remove(path + ".part")
		return "", "", fmt.Errorf("Xray 安装包校验失败：期望 %s，实际 %s", want, got)
	}
	if err := os.Rename(path+".part", path); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(path+".sha256", []byte(got), 0o644); err != nil {
		return "", "", err
	}
	return path, got, nil
}

func (s *Service) fetchDigest(ctx context.Context, tag, asset string) (string, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf(downloadURL, tag, asset+".dgst"), nil)
	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("获取校验文件失败：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("获取校验文件失败：%s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return "", err
	}
	m := sha256Re.FindSubmatch(body)
	if m == nil {
		return "", fmt.Errorf("校验文件中未找到 SHA2-256")
	}
	return string(m[1]), nil
}

func (s *Service) download(ctx context.Context, url, dst string) (string, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("下载 Xray 失败：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("下载 Xray 失败：%s", resp.Status)
	}
	f, err := os.Create(dst)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(f, h), resp.Body); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), f.Sync()
}
