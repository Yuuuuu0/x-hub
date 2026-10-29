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
