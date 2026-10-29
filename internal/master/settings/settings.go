// Package settings 管理可在 Web 设置页修改的系统设置，持久化于数据库并在内存中缓存。
package settings

import (
	"fmt"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"sync"

	"github.com/Yuuuuu0/x-hub/internal/master/store"
	"github.com/Yuuuuu0/x-hub/internal/shared/xraycap"
)

// TLS 模式。
const (
	TLSProxy      = "proxy"      // 前置反代 / CDN 终结 TLS，主控仅提供 HTTP
	TLSFile       = "file"       // 读取外部维护的证书文件
	TLSSelfSigned = "selfsigned" // 自动生成自签证书，被控端以公钥指纹锁定
)

const (
	keyAccess   = "access"
	keyXray     = "xray"
	keySecurity = "security"
)

// DefaultListen 为默认监听地址。2096 位于 Cloudflare 支持代理的 HTTPS 端口列表中。
const DefaultListen = ":2096"

// Access 为访问入口相关设置。
type Access struct {
	Listen         string   `json:"listen"`
	PanelPath      string   `json:"panelPath"`
	PublicURL      string   `json:"publicUrl"`
	TLSMode        string   `json:"tlsMode"`
	CertFile       string   `json:"certFile"`
	KeyFile        string   `json:"keyFile"`
	TrustedProxies []string `json:"trustedProxies"`
}

// Xray 为 Xray 版本管理设置。空值表示不限制 / 使用最新稳定版。
type Xray struct {
	DefaultVersion string `json:"defaultVersion"`
	MaxVersion     string `json:"maxVersion"`
}

// Security 为登录安全设置。
type Security struct {
	SessionHours int `json:"sessionHours"`
}

// Service 为设置的读写入口，读操作走内存缓存。
type Service struct {
	store *store.Store

	mu       sync.RWMutex
	access   Access
	trusted  []netip.Prefix
	xray     Xray
	security Security
}

// Load 从数据库加载全部设置，缺失项写入默认值。firstRun 表示访问设置此前不存在。
func Load(st *store.Store, newPanelPath func() string) (svc *Service, firstRun bool, err error) {
	svc = &Service{store: st}
	ok, err := st.GetSetting(keyAccess, &svc.access)
	if err != nil {
		return nil, false, err
	}
	if !ok {
		firstRun = true
		svc.access = Access{Listen: DefaultListen, PanelPath: newPanelPath(), TLSMode: TLSSelfSigned}
		if err := st.PutSetting(keyAccess, svc.access); err != nil {
			return nil, false, err
		}
	}
	svc.trusted = parsePrefixes(svc.access.TrustedProxies)
	if _, err := st.GetSetting(keyXray, &svc.xray); err != nil {
		return nil, false, err
	}
	svc.security = Security{SessionHours: 24 * 7}
	if _, err := st.GetSetting(keySecurity, &svc.security); err != nil {
		return nil, false, err
	}
	return svc, firstRun, nil
}

// Access 返回访问设置副本。
func (s *Service) Access() Access {
	s.mu.RLock()
	defer s.mu.RUnlock()
	a := s.access
	a.TrustedProxies = append([]string(nil), a.TrustedProxies...)
	return a
}

// TrustedProxies 返回已解析的可信代理网段。
func (s *Service) TrustedProxies() []netip.Prefix {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.trusted
}

// SaveAccess 校验并保存访问设置。证书可用性与端口可绑定性由调用方另行检查。
func (s *Service) SaveAccess(a Access) error {
	if err := ValidateAccess(&a); err != nil {
		return err
	}
	if err := s.store.PutSetting(keyAccess, a); err != nil {
		return err
	}
	s.mu.Lock()
	s.access = a
	s.trusted = parsePrefixes(a.TrustedProxies)
	s.mu.Unlock()
	return nil
}

// Xray 返回 Xray 设置。
func (s *Service) Xray() Xray {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.xray
}

// SaveXray 校验并保存 Xray 设置。
func (s *Service) SaveXray(x Xray) error {
	for _, v := range []string{x.DefaultVersion, x.MaxVersion} {
		if v != "" && !xraycap.IsValidTag(v) {
			return fmt.Errorf("版本号格式应为 v26.7.28：%s", v)
		}
	}
	if x.DefaultVersion != "" && x.MaxVersion != "" {
		d, _ := xraycap.ParseVersion(x.DefaultVersion)
		m, _ := xraycap.ParseVersion(x.MaxVersion)
		if d.Compare(m) > 0 {
			return fmt.Errorf("默认安装版本不能高于版本上限")
		}
	}
	if err := s.store.PutSetting(keyXray, x); err != nil {
		return err
	}
	s.mu.Lock()
	s.xray = x
	s.mu.Unlock()
	return nil
}

// Security 返回安全设置。
func (s *Service) Security() Security {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.security
}

// SaveSecurity 校验并保存安全设置。
func (s *Service) SaveSecurity(sec Security) error {
	if sec.SessionHours < 1 || sec.SessionHours > 24*90 {
		return fmt.Errorf("会话时长需在 1 到 2160 小时之间")
	}
	if err := s.store.PutSetting(keySecurity, sec); err != nil {
		return err
	}
	s.mu.Lock()
	s.security = sec
	s.mu.Unlock()
	return nil
}

var panelPathRe = regexp.MustCompile(`^[A-Za-z0-9_-]{4,64}$`)

// reservedPaths 为主控自身占用的一级路径，面板路径不能与之冲突。
var reservedPaths = map[string]bool{"agent": true}

// ValidateAccess 校验并规范化访问设置。
func ValidateAccess(a *Access) error {
	a.Listen = strings.TrimSpace(a.Listen)
	if _, port, err := splitListen(a.Listen); err != nil || port == "" {
		return fmt.Errorf("监听地址格式应为 :2096 或 127.0.0.1:2096")
	}
	a.PanelPath = strings.Trim(strings.TrimSpace(a.PanelPath), "/")
	if !panelPathRe.MatchString(a.PanelPath) || reservedPaths[a.PanelPath] {
		return fmt.Errorf("面板路径需为 4 到 64 位字母、数字、- 或 _，且不能为保留路径")
	}
	if a.PublicURL != "" {
		u, err := url.Parse(strings.TrimSpace(a.PublicURL))
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || (u.Path != "" && u.Path != "/") || u.RawQuery != "" {
			return fmt.Errorf("公开访问地址应形如 https://hub.example.com:2096，不含路径")
		}
		a.PublicURL = u.Scheme + "://" + u.Host
	}
	switch a.TLSMode {
	case TLSProxy, TLSSelfSigned:
	case TLSFile:
		if a.CertFile == "" || a.KeyFile == "" {
			return fmt.Errorf("证书文件模式需要填写证书与私钥路径")
		}
	default:
		return fmt.Errorf("未知的 TLS 模式：%s", a.TLSMode)
	}
	cleaned := a.TrustedProxies[:0]
	for _, p := range a.TrustedProxies {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, err := parsePrefix(p); err != nil {
			return fmt.Errorf("可信代理格式错误：%s", p)
		}
		cleaned = append(cleaned, p)
	}
	a.TrustedProxies = cleaned
	return nil
}

func splitListen(s string) (host, port string, err error) {
	i := strings.LastIndex(s, ":")
	if i < 0 {
		return "", "", fmt.Errorf("missing port")
	}
	host, port = s[:i], s[i+1:]
	var n int
	if _, err := fmt.Sscanf(port, "%d", &n); err != nil || n < 1 || n > 65535 {
		return "", "", fmt.Errorf("bad port")
	}
	return host, port, nil
}

// parsePrefix 解析 CIDR 或单个 IP。
func parsePrefix(s string) (netip.Prefix, error) {
	if strings.Contains(s, "/") {
		return netip.ParsePrefix(s)
	}
	addr, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Prefix{}, err
	}
	return netip.PrefixFrom(addr, addr.BitLen()), nil
}

func parsePrefixes(list []string) []netip.Prefix {
	out := make([]netip.Prefix, 0, len(list))
	for _, s := range list {
		if p, err := parsePrefix(s); err == nil {
			out = append(out, p.Masked())
		}
	}
	return out
}
