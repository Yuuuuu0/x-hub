// Package auth 提供密码哈希、随机凭据生成与登录限流。
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"
)

const (
	argonTime    = 2
	argonMemory  = 64 * 1024
	argonThreads = 2
	argonKeyLen  = 32
)

// HashPassword 使用 argon2id 生成 PHC 格式的密码哈希。
func HashPassword(password string) string {
	salt := make([]byte, 16)
	rand.Read(salt)
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	b64 := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, argonMemory, argonTime, argonThreads,
		b64.EncodeToString(salt), b64.EncodeToString(key))
}

// VerifyPassword 校验密码是否与 argon2id 哈希匹配。
func VerifyPassword(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var m uint32
	var t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return false
	}
	b64 := base64.RawStdEncoding
	salt, err1 := b64.DecodeString(parts[4])
	want, err2 := b64.DecodeString(parts[5])
	if err1 != nil || err2 != nil {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, t, m, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

const alnum = "abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// RandomString 生成去除易混淆字符的随机字母数字串。
func RandomString(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	for i := range b {
		b[i] = alnum[int(b[i])%len(alnum)]
	}
	return string(b)
}

// RandomToken 生成 32 字节随机令牌（base64url，无填充）。
func RandomToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// HashToken 计算令牌的 SHA-256 十六进制摘要，用于数据库存储。
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// LoginLimiter 按来源 IP 限制登录失败次数：窗口内失败达到上限后锁定。
type LoginLimiter struct {
	mu      sync.Mutex
	entries map[string]*limitEntry
	max     int
	window  time.Duration
}

type limitEntry struct {
	fails int
	first time.Time
}

// NewLoginLimiter 创建限流器：window 内失败 max 次即锁定至窗口结束。
func NewLoginLimiter(max int, window time.Duration) *LoginLimiter {
	return &LoginLimiter{entries: map[string]*limitEntry{}, max: max, window: window}
}

// Allowed 返回该 IP 当前是否允许尝试登录。
func (l *LoginLimiter) Allowed(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	e := l.entries[ip]
	if e == nil {
		return true
	}
	if time.Since(e.first) > l.window {
		delete(l.entries, ip)
		return true
	}
	return e.fails < l.max
}

// Fail 记录一次失败。
func (l *LoginLimiter) Fail(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	e := l.entries[ip]
	if e == nil || time.Since(e.first) > l.window {
		e = &limitEntry{first: time.Now()}
		l.entries[ip] = e
	}
	e.fails++
}

// Reset 登录成功后清除该 IP 的失败记录。
func (l *LoginLimiter) Reset(ip string) {
	l.mu.Lock()
	delete(l.entries, ip)
	l.mu.Unlock()
}
