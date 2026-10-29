package model

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

// GenerateX25519 生成 Reality 使用的 X25519 密钥对（base64 RawURL 编码，与 xray x25519 一致）。
func GenerateX25519() (privateKey, publicKey string) {
	priv := make([]byte, 32)
	rand.Read(priv)
	// 与 Xray 一致的 clamping，保证生成的私钥与官方工具输出形式相同。
	priv[0] &= 248
	priv[31] &= 127
	priv[31] |= 64
	privateKey = base64.RawURLEncoding.EncodeToString(priv)
	publicKey, _ = PublicKeyFromPrivate(privateKey)
	return privateKey, publicKey
}

// PublicKeyFromPrivate 由私钥派生公钥。
func PublicKeyFromPrivate(privateKey string) (string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(privateKey)
	if err != nil || len(raw) != 32 {
		return "", fmt.Errorf("invalid private key")
	}
	key, err := ecdh.X25519().NewPrivateKey(raw)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()), nil
}

// RandomShortID 生成 n 字节（2n 位十六进制）的 ShortID，n 取 1~8。
func RandomShortID(n int) string {
	n = max(1, min(n, 8))
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// RandomUUID 生成 RFC 4122 v4 UUID。
func RandomUUID() string {
	b := make([]byte, 16)
	rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func randomBase64(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return base64.StdEncoding.EncodeToString(b)
}
