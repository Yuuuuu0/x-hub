package store

import (
	"testing"
	"time"
)

// TestRollSubscriptionPeriod 条件重置：同一周期只重置一次，旧快照重复调用不会清掉新周期已入库的流量。
func TestRollSubscriptionPeriod(t *testing.T) {
	st := openTest(t)
	nodeID, _ := st.CreateNode("n", "", nil)
	subID, _ := st.CreateSubscription(&Subscription{Name: "s", Token: "t", Secret: "x", UUID: "u", Enabled: true, AllInbounds: true})
	hour := time.Now().Truncate(time.Hour).Unix()
	st.ResetSubscriptionUsage(subID, hour-40*86400)
	st.IngestTraffic(nodeID, []TrafficBatch{{Seq: 1, Hour: hour, Rows: []TrafficRow{{SubID: subID, InboundID: 1, Up: 100}}}})

	start := hour - 3600
	if ok, err := st.RollSubscriptionPeriod(subID, start); err != nil || !ok {
		t.Fatalf("first roll: %v %v", ok, err)
	}
	st.IngestTraffic(nodeID, []TrafficBatch{{Seq: 2, Hour: hour, Rows: []TrafficRow{{SubID: subID, InboundID: 1, Up: 7}}}})
	if ok, err := st.RollSubscriptionPeriod(subID, start); err != nil || ok {
		t.Fatalf("second roll should be no-op: %v %v", ok, err)
	}
	if s, _ := st.GetSubscription(subID); s.UsedUp != 7 || s.PeriodStart != start {
		t.Fatalf("used=%d period=%d", s.UsedUp, s.PeriodStart)
	}
}
