package auth

import (
	"testing"
	"time"
)

func TestPasswordRoundTrip(t *testing.T) {
	h := HashPassword("s3cret")
	if !VerifyPassword("s3cret", h) {
		t.Fatal("password should verify")
	}
	if VerifyPassword("wrong", h) || VerifyPassword("s3cret", "garbage") {
		t.Fatal("unexpected verification success")
	}
}

func TestLoginLimiter(t *testing.T) {
	l := NewLoginLimiter(2, time.Minute)
	l.Fail("1.1.1.1")
	if !l.Allowed("1.1.1.1") {
		t.Fatal("one failure should still be allowed")
	}
	l.Fail("1.1.1.1")
	if l.Allowed("1.1.1.1") {
		t.Fatal("should be locked after max failures")
	}
	if !l.Allowed("2.2.2.2") {
		t.Fatal("other IPs unaffected")
	}
	l.Reset("1.1.1.1")
	if !l.Allowed("1.1.1.1") {
		t.Fatal("reset should unlock")
	}
}

func TestTOTP(t *testing.T) {
	// RFC 6238 附录 B 的 SHA1 测试向量（密钥 "12345678901234567890"），取后 6 位。
	secret := totpEncoding.EncodeToString([]byte("12345678901234567890"))
	cases := map[int64]string{59: "287082", 1111111109: "081804", 1234567890: "005924", 2000000000: "279037"}
	for ts, want := range cases {
		if !VerifyTOTP(secret, want, time.Unix(ts, 0)) {
			t.Errorf("t=%d code %s should verify", ts, want)
		}
	}
	if VerifyTOTP(secret, "287082", time.Unix(59+120, 0)) {
		t.Error("code outside the window must fail")
	}
}
