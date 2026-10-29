package accounting

import (
	"testing"
	"time"

	"github.com/Yuuuuu0/x-hub/internal/master/store"
)

func TestPeriodStart(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	at := func(s string) time.Time { v, _ := time.ParseInLocation("2006-01-02 15:04", s, loc); return v }
	cases := []struct {
		day       int
		now, want string
	}{
		{1, "2026-10-29 12:00", "2026-10-01 00:00"},
		{29, "2026-10-29 12:00", "2026-10-29 00:00"},
		{30, "2026-10-29 12:00", "2026-09-30 00:00"},
		{15, "2026-01-10 08:00", "2025-12-15 00:00"},
	}
	for _, c := range cases {
		if got := PeriodStart(c.day, at(c.now), loc); !got.Equal(at(c.want)) {
			t.Errorf("day=%d now=%s got %s want %s", c.day, c.now, got, c.want)
		}
	}
}

func TestState(t *testing.T) {
	now := time.Now()
	s := &store.Subscription{LimitBytes: 100, UsedUp: 40, UsedDown: 60}
	if State(s, now) != store.SubExceeded {
		t.Fatal("limit reached should be exceeded")
	}
	s.ExpireAt = now.Add(-time.Minute).Unix()
	if State(s, now) != store.SubExpired {
		t.Fatal("expiry takes precedence")
	}
	s.ExpireAt, s.LimitBytes = now.Add(time.Hour).Unix(), 0
	if State(s, now) != store.SubActive {
		t.Fatal("unlimited and not expired should be active")
	}
}
