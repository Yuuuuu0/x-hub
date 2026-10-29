package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

var totpEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewTOTPSecret 生成 160 位 TOTP 密钥（base32，无填充）。
func NewTOTPSecret() string {
	b := make([]byte, 20)
	rand.Read(b)
	return totpEncoding.EncodeToString(b)
}

// TOTPURI 返回验证器 App 可扫描的 otpauth 地址。
func TOTPURI(secret, account string) string {
	return fmt.Sprintf("otpauth://totp/%s?secret=%s&issuer=x-hub&algorithm=SHA1&digits=6&period=30",
		url.PathEscape("x-hub:"+account), secret)
}

// totpCode 按 RFC 6238 计算某一时间步的 6 位验证码。
func totpCode(key []byte, step int64) string {
	mac := hmac.New(sha1.New, key)
	binary.Write(mac, binary.BigEndian, step)
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	v := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", v%1000000)
}

// VerifyTOTP 校验验证码，允许前后各一个时间步的时钟偏差。
func VerifyTOTP(secret, code string, now time.Time) bool {
	key, err := totpEncoding.DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	code = strings.TrimSpace(code)
	if err != nil || len(code) != 6 {
		return false
	}
	step := now.Unix() / 30
	for d := int64(-1); d <= 1; d++ {
		if subtle.ConstantTimeCompare([]byte(totpCode(key, step+d)), []byte(code)) == 1 {
			return true
		}
	}
	return false
}
