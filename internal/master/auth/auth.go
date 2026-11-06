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

// argonSlots 限制同时进行的 argon2 计算数：每次计算约占 64MB 内存，并发登录时防止内存耗尽。
var argonSlots = make(chan struct{}, 2)

// argonKey 在全局并发上限内计算 argon2id 密钥。
func argonKey(password, salt []byte, t, m uint32, p uint8, keyLen uint32) []byte {
	argonSlots <- struct{}{}
	defer func() { <-argonSlots }()
	return argon2.IDKey(password, salt, t, m, p, keyLen)
}

// HashPassword 使用 argon2id 生成 PHC 格式的密码哈希。
func HashPassword(password string) string {
	salt := make([]byte, 16)
	rand.Read(salt)
	key := argonKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
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
	got := argonKey([]byte(password), salt, t, m, p, uint32(len(want)))
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

// LoginLimiter 按来源 IP 限制登录尝试次数：窗口内尝试（未成功）达到上限后锁定。
// 尝试在校验前原子预占，避免并发请求同时通过检查而绕过上限。
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

// Acquire 原子地检查并预占该 IP 的一次尝试，已达上限时返回 false。
// 预占的尝试按失败计数，登录成功后由 Reset 清零，非失败的中途返回（如参数错误）由 Release 归还。
func (l *LoginLimiter) Acquire(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	e := l.entries[ip]
	if e == nil || time.Since(e.first) > l.window {
		e = &limitEntry{first: time.Now()}
		l.entries[ip] = e
	}
	if e.fails >= l.max {
		return false
	}
	e.fails++
	return true
}

// Release 归还一次预占但不应计为失败的尝试。
func (l *LoginLimiter) Release(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if e := l.entries[ip]; e != nil && e.fails > 0 {
		e.fails--
	}
}

// Reset 登录成功后清除该 IP 的失败记录。
func (l *LoginLimiter) Reset(ip string) {
	l.mu.Lock()
	delete(l.entries, ip)
	l.mu.Unlock()
}

// Prune 清理已过窗口期的记录，供周期任务调用，避免不再访问的 IP 长期占用内存。
func (l *LoginLimiter) Prune() {
	l.mu.Lock()
	defer l.mu.Unlock()
	for ip, e := range l.entries {
		if time.Since(e.first) > l.window {
			delete(l.entries, ip)
		}
	}
}
