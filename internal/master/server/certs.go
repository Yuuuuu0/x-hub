// Package server 管理主控的 HTTP(S) 监听：按访问设置动态切换 TLS 模式、热加载证书与平滑换端口。
package server

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Yuuuuu0/x-hub/internal/master/settings"
	"github.com/Yuuuuu0/x-hub/internal/shared/tlspin"
)

// fileCheckInterval 为证书文件 mtime 的检查间隔，避免每次握手都 stat。
const fileCheckInterval = 30 * time.Second

// Certs 根据当前访问设置提供 TLS 证书：自签模式返回本地生成的证书，文件模式按 mtime 热加载。
type Certs struct {
	dir      string
	settings *settings.Service

	mu        sync.Mutex
	self      *tls.Certificate
	selfFP    string
	file      *tls.Certificate
	filePaths [2]string
	fileMod   [2]time.Time
	checkedAt time.Time
}

// NewCerts 创建证书提供者，自签证书存放在 dir 下。
func NewCerts(dir string, s *settings.Service) *Certs {
	return &Certs{dir: dir, settings: s}
}

// GetCertificate 实现 tls.Config.GetCertificate。
func (c *Certs) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	a := c.settings.Access()
	switch a.TLSMode {
	case settings.TLSSelfSigned:
		if _, err := c.SelfSigned(); err != nil {
			return nil, err
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.self, nil
	case settings.TLSFile:
		return c.fileCert(a.CertFile, a.KeyFile)
	}
	return nil, errors.New("tls disabled")
}

// SelfSigned 确保自签证书存在并返回其公钥指纹。
func (c *Certs) SelfSigned() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.self != nil {
		return c.selfFP, nil
	}
	certPath := filepath.Join(c.dir, "selfsigned.crt")
	keyPath := filepath.Join(c.dir, "selfsigned.key")
	if _, err := os.Stat(certPath); errors.Is(err, os.ErrNotExist) {
		if err := generateSelfSigned(certPath, keyPath); err != nil {
			return "", fmt.Errorf("generate self-signed cert: %w", err)
		}
	}
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return "", err
	}
	c.self = &cert
	c.selfFP = tlspin.Fingerprint(cert.Leaf)
	return c.selfFP, nil
}

// CheckFile 校验证书文件是否可加载，用于保存设置前的预检。
func (c *Certs) CheckFile(certFile, keyFile string) error {
	_, err := tls.LoadX509KeyPair(certFile, keyFile)
	return err
}

// fileCert 返回文件模式证书，路径变化或文件 mtime 变化时重新加载；重载失败时沿用旧证书。
func (c *Certs) fileCert(certFile, keyFile string) (*tls.Certificate, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	paths := [2]string{certFile, keyFile}
	if c.file != nil && paths == c.filePaths && time.Since(c.checkedAt) < fileCheckInterval {
		return c.file, nil
	}
	c.checkedAt = time.Now()
	var mods [2]time.Time
	for i, p := range paths {
		st, err := os.Stat(p)
		if err != nil {
			if c.file != nil && paths == c.filePaths {
				return c.file, nil
			}
			return nil, err
		}
		mods[i] = st.ModTime()
	}
	if c.file != nil && paths == c.filePaths && mods == c.fileMod {
		return c.file, nil
	}
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		if c.file != nil && paths == c.filePaths {
			return c.file, nil
		}
		return nil, err
	}
	c.file, c.filePaths, c.fileMod = &cert, paths, mods
	return c.file, nil
}

func generateSelfSigned(certPath, keyPath string) error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	tpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "x-hub"},
		DNSNames:              []string{"x-hub"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(20, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		return err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(certPath), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		return err
	}
	return os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644)
}
