package store

import (
	"path/filepath"
	"testing"
	"time"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestIngestDedupAndRollup(t *testing.T) {
	st := openTest(t)
	nodeID, _ := st.CreateNode("n", "", "")
	subID, _ := st.CreateSubscription(&Subscription{Name: "s", Token: "t", Secret: "x", UUID: "u", Enabled: true, AllInbounds: true})
	now := time.Now().Truncate(time.Hour).Unix()
	b1 := TrafficBatch{Seq: 1, Hour: now, Rows: []TrafficRow{{SubID: subID, InboundID: 7, Up: 10, Down: 100}}, NICRx: 1000, NICTx: 50}
	b2 := TrafficBatch{Seq: 2, Hour: now, Rows: []TrafficRow{{SubID: subID, InboundID: 7, Up: 1, Down: 1}}}

	last, touched, err := st.IngestTraffic(nodeID, []TrafficBatch{b2, b1})
	if err != nil || last != 2 || len(touched) != 1 {
		t.Fatalf("ingest: %d %v %v", last, touched, err)
	}
	// 重传同样的批次不会重复累计。
	if last, _, _ = st.IngestTraffic(nodeID, []TrafficBatch{b1, b2}); last != 2 {
		t.Fatalf("dedup last=%d", last)
	}
	s, _ := st.GetSubscription(subID)
	if s.UsedUp != 11 || s.UsedDown != 101 {
		t.Fatalf("used: %d %d", s.UsedUp, s.UsedDown)
	}

	// 周期起点之后才开始的订阅不计入早于周期的迟到流量。
	st.ResetSubscriptionUsage(subID, now+7200)
	st.IngestTraffic(nodeID, []TrafficBatch{{Seq: 3, Hour: now, Rows: []TrafficRow{{SubID: subID, InboundID: 7, Up: 5}}}})
	if s, _ = st.GetSubscription(subID); s.UsedUp != 0 {
		t.Fatalf("late traffic counted into new period: %d", s.UsedUp)
	}

	rx, tx, _ := st.NodeTrafficSince(nodeID, now)
	if rx != 1000 || tx != 50 {
		t.Fatalf("node traffic %d %d", rx, tx)
	}

	loc, _ := time.LoadLocation("Asia/Shanghai")
	if n, err := st.RollupTraffic(now+3600, loc); err != nil || n != 2 {
		t.Fatalf("rollup n=%d err=%v", n, err)
	}
	series, _ := st.SubTrafficSeries(subID, 0)
	var up int64
	for _, p := range series {
		up += p.Up
	}
	if up != 16 {
		t.Fatalf("series after rollup up=%d", up)
	}
	if rx, _, _ = st.NodeTrafficSince(nodeID, 0); rx != 1000 {
		t.Fatalf("node traffic after rollup %d", rx)
	}
}

// TestIDNeverReused 删除最大 ID 的记录后，新记录不得复用该 ID，否则历史流量会错配。
func TestIDNeverReused(t *testing.T) {
	st := openTest(t)
	n1, _ := st.CreateNode("a", "", "")
	n2, _ := st.CreateNode("b", "", "")
	st.DeleteNode(n2)
	if n3, _ := st.CreateNode("c", "", ""); n3 == n2 || n3 <= n1 {
		t.Fatalf("node id reused: %d after deleting %d", n3, n2)
	}
	in := &Inbound{NodeID: n1, Port: 1, Protocol: "vless", Spec: []byte("{}")}
	i1, _ := st.CreateInbound(in)
	st.DeleteInbound(i1)
	in.Port = 2
	if i2, _ := st.CreateInbound(in); i2 == i1 {
		t.Fatalf("inbound id reused: %d", i2)
	}
	s1, _ := st.CreateSubscription(&Subscription{Name: "s", Token: "t1", Secret: "x", UUID: "u"})
	st.DeleteSubscription(s1)
	if s2, _ := st.CreateSubscription(&Subscription{Name: "s", Token: "t2", Secret: "x", UUID: "u"}); s2 == s1 {
		t.Fatalf("subscription id reused: %d", s2)
	}
}
