package ledger

import (
	"path/filepath"
	"testing"
)

func TestLedgerSealAckPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.json")
	l, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	l.Add(3600, map[string][2]int64{"s1.i1": {10, 100}}, 5, 6)
	l.Add(3600, map[string][2]int64{"s1.i1": {1, 1}, "s2.i1": {2, 2}}, 0, 0)
	l.Add(7200, map[string][2]int64{"s1.i1": {7, 7}}, 0, 0)

	batches, _ := l.Seal()
	if len(batches) != 2 || batches[0].Seq != 1 || batches[0].Hour != 3600 || batches[0].Users["s1.i1"] != [2]int64{11, 101} ||
		batches[0].NICRx != 5 || batches[1].Seq != 2 {
		t.Fatalf("seal: %+v", batches)
	}

	// 未确认的批次在重新打开后仍在，新采集生成更大的序号。
	l2, _ := Open(path)
	l2.Add(7200, map[string][2]int64{"s1.i1": {1, 1}}, 0, 0)
	batches, _ = l2.Seal()
	if len(batches) != 3 || batches[2].Seq != 3 {
		t.Fatalf("reopen: %+v", batches)
	}
	l2.Ack(2)
	if batches, _ = l2.Seal(); len(batches) != 1 || batches[0].Seq != 3 {
		t.Fatalf("ack: %+v", batches)
	}

	// 主控已确认的序号高于本地（账本被清空后重装），新批次序号需接续。
	l3, _ := Open(filepath.Join(t.TempDir(), "fresh.json"))
	l3.Ack(50)
	l3.Add(3600, map[string][2]int64{"s1.i1": {1, 1}}, 0, 0)
	if batches, _ = l3.Seal(); batches[0].Seq != 51 {
		t.Fatalf("seq continuation: %+v", batches)
	}
}

func TestNICDelta(t *testing.T) {
	l, _ := Open(filepath.Join(t.TempDir(), "l.json"))
	if rx, _ := l.NICDelta(1000, 1000); rx != 0 {
		t.Fatal("first sample is baseline")
	}
	if rx, tx := l.NICDelta(1500, 1200); rx != 500 || tx != 200 {
		t.Fatalf("delta %d %d", rx, tx)
	}
	if rx, _ := l.NICDelta(300, 1300); rx != 300 {
		t.Fatalf("counter reset should count current value, got %d", rx)
	}
}
