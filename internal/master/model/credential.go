package model

import (
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strconv"
)

// Credential 为订阅在某入站上的连接凭据：UUID 用于 VLESS / VMess，Password 用于 Trojan / SS2022。
type Credential struct {
	UUID     string
	Password string
}

// CredentialFor 由订阅的 UUID 与根密钥确定性派生入站凭据，重置订阅凭据即可全部轮换。
func CredentialFor(protocol string, s *Spec, subUUID, subSecret string, inboundID int64) Credential {
	switch protocol {
	case ProtoTrojan:
		mac := hmac.New(sha256.New, []byte(subSecret))
		mac.Write([]byte("trojan"))
		return Credential{Password: hex.EncodeToString(mac.Sum(nil))[:32]}
	case ProtoSS:
		return Credential{Password: deriveKey(subSecret, "ss2022:"+strconv.FormatInt(inboundID, 10), SS2022Methods[s.SS.Method])}
	default:
		return Credential{UUID: subUUID}
	}
}

// deriveKey 用 HKDF-SHA256 派生 n 字节密钥并以 base64 编码。
func deriveKey(secret, info string, n int) string {
	key, _ := hkdf.Key(sha256.New, []byte(secret), nil, info, n)
	return base64.StdEncoding.EncodeToString(key)
}

// Client 为写入服务端配置的一个客户端。
type Client struct {
	Email string
	Cred  Credential
}

// ssPlaceholderEmail 为 SS2022 入站没有订阅时的占位用户。Xray 在无用户时会退化为单用户模式，
// 仅凭服务端密钥即可连接；保留一个占位用户可维持多用户模式，避免被移除的订阅继续使用。
const ssPlaceholderEmail = "x-hub-placeholder"

// ssPlaceholder 返回由服务端密钥确定性派生的占位用户，保证配置哈希稳定。
func ssPlaceholder(s *Spec) Client {
	return Client{Email: ssPlaceholderEmail, Cred: Credential{Password: deriveKey(s.SS.ServerKey, "placeholder", SS2022Methods[s.SS.Method])}}
}

// GenerateSSKey 生成指定加密方式的随机 SS2022 密钥。
func GenerateSSKey(method string) (string, bool) {
	n, ok := SS2022Methods[method]
	if !ok {
		return "", false
	}
	return randomBase64(n), true
}
