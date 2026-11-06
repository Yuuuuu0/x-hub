package auth

import (
	"sync"
	"sync/atomic"
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
	if !l.Acquire("1.1.1.1") || !l.Acquire("1.1.1.1") {
		t.Fatal("attempts within max should be allowed")
	}
	if l.Acquire("1.1.1.1") {
		t.Fatal("should be locked after max attempts")
	}
	if !l.Acquire("2.2.2.2") {
		t.Fatal("other IPs unaffected")
	}
	l.Release("1.1.1.1")
	if !l.Acquire("1.1.1.1") {
		t.Fatal("released attempt should be available again")
	}
	l.Reset("1.1.1.1")
	if !l.Acquire("1.1.1.1") {
		t.Fatal("reset should unlock")
	}
}

// TestLoginLimiterConcurrent 并发预占时成功次数不超过上限。
func TestLoginLimiterConcurrent(t *testing.T) {
	l := NewLoginLimiter(5, time.Minute)
	var ok atomic.Int32
	var wg sync.WaitGroup
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if l.Acquire("1.1.1.1") {
				ok.Add(1)
			}
		}()
	}
	wg.Wait()
	if n := ok.Load(); n != 5 {
		t.Fatalf("acquired %d, want 5", n)
	}
}

// TestLoginLimiterPrune 过期记录被周期清理移除，未过期的保留。
func TestLoginLimiterPrune(t *testing.T) {
	l := NewLoginLimiter(1, time.Minute)
	l.Acquire("old")
	l.Acquire("new")
	l.entries["old"].first = time.Now().Add(-2 * time.Minute)
	l.Prune()
	if _, ok := l.entries["old"]; ok {
		t.Fatal("expired entry should be pruned")
	}
	if _, ok := l.entries["new"]; !ok {
		t.Fatal("active entry should be kept")
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
