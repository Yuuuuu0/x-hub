// Package certs 管理被控端的 TLS 证书：检测节点上已有证书文件能否读取，
// 以及通过 ACME（Let's Encrypt + Cloudflare DNS 验证）自动申请与续签证书。
package certs

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-acme/lego/v4/certcrypto"
	"github.com/go-acme/lego/v4/certificate"
	"github.com/go-acme/lego/v4/lego"
	"github.com/go-acme/lego/v4/providers/dns/cloudflare"
	"github.com/go-acme/lego/v4/registration"

	"github.com/Yuuuuu0/x-hub/internal/shared/protocol"
)

const (
	renewBefore = 30 * 24 * time.Hour // 剩余有效期不足 30 天时续签
	renewEvery  = 12 * time.Hour
)

// Manager 管理 ACME 证书，证书写入 <dir>/certs/<域名>/，与主控编译配置中的相对路径一致。
type Manager struct {
	dir string
	log *slog.Logger
	mu  sync.Mutex
}

// New 创建证书管理器，dir 为被控端数据目录。
func New(dir string, log *slog.Logger) *Manager {
	return &Manager{dir: filepath.Join(dir, "certs"), log: log}
}

func (m *Manager) paths(domain string) (cert, key string) {
	return filepath.Join(m.dir, domain, "fullchain.pem"), filepath.Join(m.dir, domain, "privkey.pem")
}

// CheckFiles 检查证书与私钥能否被当前用户读取并配对，返回证书域名与到期时间。
func CheckFiles(certFile, keyFile string) protocol.ProbeCertResult {
	pair, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			return protocol.ProbeCertResult{Error: "被控端（xhub 用户）无权读取证书文件，请调整文件权限或在续签钩子中复制一份给 xhub"}
		}
		return protocol.ProbeCertResult{Error: err.Error()}
	}
	return protocol.ProbeCertResult{OK: true, Names: pair.Leaf.DNSNames, NotAfter: pair.Leaf.NotAfter.Unix()}
}

// Ensure 确保各域名都有有效证书：缺失或即将到期时申请，并保存 ACME 设置供后台续签。
func (m *Manager) Ensure(ctx context.Context, a protocol.ACME) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := os.MkdirAll(m.dir, 0o700); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(m.dir, "acme.json"), a); err != nil {
		return err
	}
	var pending []string
	for _, d := range a.Domains {
		if !m.valid(d) {
			pending = append(pending, d)
		}
	}
	if len(pending) == 0 {
		return nil
	}
	if a.Email == "" || a.CloudflareToken == "" {
		return errors.New("自动申请证书需要先在主控「设置 → 证书」中填写 ACME 邮箱与 Cloudflare API Token")
	}
	client, err := m.client(a)
	if err != nil {
		return err
	}
	for _, d := range pending {
		if err := ctx.Err(); err != nil {
			return err
		}
		m.log.Info("obtaining certificate", "domain", d)
		res, err := client.Certificate.Obtain(certificate.ObtainRequest{Domains: []string{d}, Bundle: true})
		if err != nil {
			return fmt.Errorf("申请 %s 证书失败：%w", d, err)
		}
		cert, key := m.paths(d)
		if err := os.MkdirAll(filepath.Dir(cert), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(key, res.PrivateKey, 0o600); err != nil {
			return err
		}
		if err := os.WriteFile(cert, res.Certificate, 0o600); err != nil {
			return err
		}
		m.log.Info("certificate obtained", "domain", d)
	}
	return nil
}

// RenewLoop 定期续签即将到期的证书。Xray 每小时重新读取证书文件，续签后无需重启。
func (m *Manager) RenewLoop(ctx context.Context) {
	t := time.NewTicker(renewEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		var a protocol.ACME
		if b, err := os.ReadFile(filepath.Join(m.dir, "acme.json")); err != nil || json.Unmarshal(b, &a) != nil || len(a.Domains) == 0 {
			continue
		}
		if err := m.Ensure(ctx, a); err != nil {
			m.log.Warn("renew certificates", "err", err)
		}
	}
}

// valid 判断域名证书存在且距到期超过续签阈值。
func (m *Manager) valid(domain string) bool {
	cert, key := m.paths(domain)
	r := CheckFiles(cert, key)
	return r.OK && time.Until(time.Unix(r.NotAfter, 0)) > renewBefore
}

// account 为持久化的 ACME 账户，复用以避免触发注册频率限制。
type account struct {
	Email        string                 `json:"email"`
	Key          string                 `json:"key"`
	Registration *registration.Resource `json:"registration"`
	key          crypto.PrivateKey
}

func (a *account) GetEmail() string                        { return a.Email }
func (a *account) GetRegistration() *registration.Resource { return a.Registration }
func (a *account) GetPrivateKey() crypto.PrivateKey        { return a.key }

// client 加载或创建 ACME 账户并配置 Cloudflare DNS 验证。
func (m *Manager) client(a protocol.ACME) (*lego.Client, error) {
	path := filepath.Join(m.dir, "account.json")
	acc := &account{}
	if b, err := os.ReadFile(path); err == nil && json.Unmarshal(b, acc) == nil && acc.Email == a.Email {
		if blk, _ := pem.Decode([]byte(acc.Key)); blk != nil {
			acc.key, _ = x509.ParseECPrivateKey(blk.Bytes)
		}
	}
	if acc.key == nil {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, err
		}
		der, _ := x509.MarshalECPrivateKey(key)
		acc = &account{Email: a.Email, Key: string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})), key: key}
	}
	cfg := lego.NewConfig(acc)
	cfg.Certificate.KeyType = certcrypto.EC256
	client, err := lego.NewClient(cfg)
	if err != nil {
		return nil, err
	}
	cf := cloudflare.NewDefaultConfig()
	cf.AuthToken = a.CloudflareToken
	cf.PropagationTimeout = 3 * time.Minute
	provider, err := cloudflare.NewDNSProviderConfig(cf)
	if err != nil {
		return nil, err
	}
	if err := client.Challenge.SetDNS01Provider(provider); err != nil {
		return nil, err
	}
	if acc.Registration == nil {
		reg, err := client.Registration.Register(registration.RegisterOptions{TermsOfServiceAgreed: true})
		if err != nil {
			return nil, fmt.Errorf("注册 ACME 账户失败：%w", err)
		}
		acc.Registration = reg
		if err := writeJSON(path, acc); err != nil {
			return nil, err
		}
	}
	return client, nil
}

func writeJSON(path string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

// DropMissing 从配置中移除证书文件尚不存在的 ACME 入站（证书路径相对数据目录），返回过滤后的配置与被移除的 tag。
// 证书申请失败时用它让其余入站照常生效。
func DropMissing(cfg []byte, dataDir string) ([]byte, []string, error) {
	var c map[string]any
	if err := json.Unmarshal(cfg, &c); err != nil {
		return nil, nil, err
	}
	inbounds, _ := c["inbounds"].([]any)
	kept := make([]any, 0, len(inbounds))
	var dropped []string
	for _, raw := range inbounds {
		if missingACMECert(raw, dataDir) {
			in, _ := raw.(map[string]any)
			tag, _ := in["tag"].(string)
			dropped = append(dropped, tag)
			continue
		}
		kept = append(kept, raw)
	}
	c["inbounds"] = kept
	out, err := json.Marshal(c)
	return out, dropped, err
}

func missingACMECert(raw any, dataDir string) bool {
	in, _ := raw.(map[string]any)
	stream, _ := in["streamSettings"].(map[string]any)
	tlsSettings, _ := stream["tlsSettings"].(map[string]any)
	certsList, _ := tlsSettings["certificates"].([]any)
	for _, c := range certsList {
		entry, _ := c.(map[string]any)
		file, _ := entry["certificateFile"].(string)
		if file == "" || filepath.IsAbs(file) {
			continue
		}
		if _, err := os.Stat(filepath.Join(dataDir, file)); err != nil {
			return true
		}
	}
	return false
}
