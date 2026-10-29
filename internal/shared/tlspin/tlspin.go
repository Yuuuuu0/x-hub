// Package tlspin 计算并校验证书公钥指纹（SPKI SHA-256，base64），格式与 curl --pinnedpubkey 一致。
package tlspin

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"net/http"
	"time"
)

// Fingerprint 返回证书公钥的 SHA-256 base64 指纹。
func Fingerprint(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	return base64.StdEncoding.EncodeToString(sum[:])
}

// ClientConfig 返回 TLS 客户端配置：fp 非空时跳过 CA 校验并锁定叶子证书公钥，否则使用系统 CA。
func ClientConfig(fp string) *tls.Config {
	if fp == "" {
		return &tls.Config{MinVersion: tls.VersionTLS12}
	}
	return &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: true,
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 || Fingerprint(cs.PeerCertificates[0]) != fp {
				return errors.New("server public key fingerprint mismatch")
			}
			return nil
		},
	}
}

// HTTPClient 返回带指纹锁定的 HTTP 客户端，强制 HTTP/1.1 以便 WebSocket 握手。
func HTTPClient(fp string, timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			Proxy:               http.ProxyFromEnvironment,
			TLSClientConfig:     ClientConfig(fp),
			TLSHandshakeTimeout: 15 * time.Second,
			ForceAttemptHTTP2:   false,
		},
	}
}
