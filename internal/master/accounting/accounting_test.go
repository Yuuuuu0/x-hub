package accounting

import (
	"log/slog"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Yuuuuu0/x-hub/internal/master/settings"
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

type countSyncer struct{ n atomic.Int32 }

func (c *countSyncer) TriggerAll() { c.n.Add(1) }

// TestEvaluateConcurrentReset 并发核算时周期只重置一次，重置后入库的流量不会被后续核算清掉。
func TestEvaluateConcurrentReset(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	cfg, _, err := settings.Load(st, func() string { return "panel1234" })
	if err != nil {
		t.Fatal(err)
	}
	a := New(st, cfg, &countSyncer{}, slog.New(slog.DiscardHandler))
	nodeID, _ := st.CreateNode("n", "", nil)
	subID, _ := st.CreateSubscription(&store.Subscription{Name: "s", Token: "t", Secret: "x", UUID: "u", Enabled: true, AllInbounds: true})
	if err := st.UpdateSubscriptionQuota(subID, 1000, 0, 1); err != nil {
		t.Fatal(err)
	}
	hour := time.Now().Truncate(time.Hour).Unix()
	st.ResetSubscriptionUsage(subID, hour-40*86400)
	st.IngestTraffic(nodeID, []store.TrafficBatch{{Seq: 1, Hour: hour, Rows: []store.TrafficRow{{SubID: subID, InboundID: 1, Up: 2000}}}})
	a.Evaluate(subID)
	if s, _ := st.GetSubscription(subID); s.State != store.SubActive || s.UsedUp != 0 {
		t.Fatalf("after reset: state=%s used=%d", s.State, s.UsedUp)
	}

	// 周期内新流量超额后，并发核算（含全量核算）不得再次清零，状态应为超额。
	st.IngestTraffic(nodeID, []store.TrafficBatch{{Seq: 2, Hour: hour, Rows: []store.TrafficRow{{SubID: subID, InboundID: 1, Up: 1500}}}})
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if i%2 == 0 {
				a.Evaluate()
			} else {
				a.Evaluate(subID)
			}
		}()
	}
	wg.Wait()
	if s, _ := st.GetSubscription(subID); s.UsedUp != 1500 || s.State != store.SubExceeded {
		t.Fatalf("after concurrent evaluate: state=%s used=%d", s.State, s.UsedUp)
	}
}
