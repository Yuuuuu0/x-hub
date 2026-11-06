package ledger

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

	// 未确认的批次在重新打开后仍在；存在待确认批次时新流量只累计、不封存。
	l2, _ := Open(path)
	l2.Add(7200, map[string][2]int64{"s1.i1": {1, 1}}, 0, 0)
	batches, _ = l2.Seal()
	if len(batches) != 2 || batches[1].Seq != 2 {
		t.Fatalf("reopen: %+v", batches)
	}
	l2.Add(7200, map[string][2]int64{"s1.i1": {2, 2}}, 0, 0)
	// 确认后再封存，期间的累计合并为一个批次，序号接续。
	l2.Ack(2)
	if batches, _ = l2.Seal(); len(batches) != 1 || batches[0].Seq != 3 || batches[0].Users["s1.i1"] != [2]int64{3, 3} {
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

// 主控长时间在线却不确认时，每个上报周期调用 Seal 不应持续新建批次。
func TestSealDoesNotGrowWhileUnacked(t *testing.T) {
	l, _ := Open(filepath.Join(t.TempDir(), "l.json"))
	for range 100 {
		l.Add(3600, map[string][2]int64{"s1.i1": {1, 1}}, 0, 0)
		if _, err := l.Seal(); err != nil {
			t.Fatal(err)
		}
	}
	batches, _ := l.Seal()
	if len(batches) != 1 {
		t.Fatalf("pending grew to %d", len(batches))
	}
	l.Ack(batches[0].Seq)
	batches, _ = l.Seal()
	if len(batches) != 1 || batches[0].Users["s1.i1"] != [2]int64{99, 99} {
		t.Fatalf("accumulated: %+v", batches)
	}
}

func TestSealSplitsOversizedBatch(t *testing.T) {
	old := maxBatchBytes
	maxBatchBytes = 1024
	defer func() { maxBatchBytes = old }()
	l, _ := Open(filepath.Join(t.TempDir(), "l.json"))
	users := map[string][2]int64{}
	for i := range 100 {
		users[fmt.Sprintf("s%d.i1", i)] = [2]int64{1, 2}
	}
	l.Add(3600, users, 7, 8)
	batches, _ := l.Seal()
	if len(batches) < 2 {
		t.Fatalf("expected split, got %d", len(batches))
	}
	total := 0
	for i, b := range batches {
		if b.Seq != int64(i+1) || b.Hour != 3600 {
			t.Fatalf("batch %d: %+v", i, b)
		}
		if (i == 0) != (b.NICRx == 7) {
			t.Fatalf("nic should be in first batch only: %+v", b)
		}
		total += len(b.Users)
	}
	if total != 100 {
		t.Fatalf("users lost: %d", total)
	}
}

func TestNICDelta(t *testing.T) {
	l, _ := Open(filepath.Join(t.TempDir(), "l.json"))
	nic := func(kv ...any) map[string][2]uint64 {
		m := map[string][2]uint64{}
		for i := 0; i < len(kv); i += 3 {
			m[kv[i].(string)] = [2]uint64{uint64(kv[i+1].(int)), uint64(kv[i+2].(int))}
		}
		return m
	}
	if rx, _ := l.NICDelta(nic("eth0", 1000, 1000)); rx != 0 {
		t.Fatal("first sample is baseline")
	}
	if rx, tx := l.NICDelta(nic("eth0", 1500, 1200)); rx != 500 || tx != 200 {
		t.Fatalf("delta %d %d", rx, tx)
	}
	// 新网卡首次只建立基线。
	if rx, tx := l.NICDelta(nic("eth0", 1600, 1300, "eth1", 9000, 9000)); rx != 100 || tx != 100 {
		t.Fatalf("new nic delta %d %d", rx, tx)
	}
	// eth1 重建计数归零：只把 eth1 当前值计为增量，不把整个总量当增量。
	if rx, tx := l.NICDelta(nic("eth0", 1700, 1400, "eth1", 50, 60)); rx != 150 || tx != 160 {
		t.Fatalf("nic reset delta %d %d", rx, tx)
	}
	// 读取失败时不更新基线。
	if rx, _ := l.NICDelta(nil); rx != 0 {
		t.Fatal("empty counters")
	}
	if rx, _ := l.NICDelta(nic("eth0", 1800, 1400, "eth1", 50, 60)); rx != 100 {
		t.Fatalf("after empty sample %d", rx)
	}
}

func TestNICDeltaLegacyTotals(t *testing.T) {
	dir := t.TempDir()
	// 旧版本账本只记录了总量。
	path := filepath.Join(dir, "old.json")
	os.WriteFile(path, []byte(`{"nextSeq":3,"open":{},"pending":[],"lastNicRx":1000,"lastNicTx":2000}`), 0o600)
	l, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if rx, tx := l.NICDelta(map[string][2]uint64{"eth0": {700, 1500}, "eth1": {500, 600}}); rx != 200 || tx != 100 {
		t.Fatalf("legacy delta %d %d", rx, tx)
	}
	if rx, _ := l.NICDelta(map[string][2]uint64{"eth0": {800, 1500}, "eth1": {500, 600}}); rx != 100 {
		t.Fatalf("after transition %d", rx)
	}

	// 总量变小时无法判断哪块网卡重置，只建立基线，不多记。
	path2 := filepath.Join(dir, "old2.json")
	os.WriteFile(path2, []byte(`{"nextSeq":1,"lastNicRx":100000,"lastNicTx":100000}`), 0o600)
	l2, _ := Open(path2)
	if rx, tx := l2.NICDelta(map[string][2]uint64{"eth0": {10, 20}}); rx != 0 || tx != 0 {
		t.Fatalf("legacy shrink %d %d", rx, tx)
	}
	l2.Add(3600, nil, 0, 0)
	b, _ := os.ReadFile(path2)
	if strings.Contains(string(b), "lastNic") || !strings.Contains(string(b), `"nics"`) {
		t.Fatalf("persisted state not migrated: %s", b)
	}
}

func TestOpenCorruptAndQuarantine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.json")
	os.WriteFile(path, []byte(`{"nextSeq":`), 0o600)
	if _, err := Open(path); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("expected ErrCorrupt, got %v", err)
	}
	bak, err := Quarantine(path)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(bak); string(b) != `{"nextSeq":` || !strings.HasPrefix(filepath.Base(bak), "ledger.json.corrupt-") {
		t.Fatalf("backup %s: %q", bak, b)
	}
	l, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if batches, _ := l.Seal(); len(batches) != 0 {
		t.Fatalf("fresh ledger: %+v", batches)
	}
}
