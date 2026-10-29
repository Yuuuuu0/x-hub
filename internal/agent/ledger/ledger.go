// Package ledger 是被控端的本地流量账本：采集增量按小时累计，上报前封存为带序号的批次，
// 收到主控确认后才删除，从而在断网或主控不可用期间不丢流量。
package ledger

import (
	"encoding/json"
	"errors"
	"os"
	"sort"
	"sync"

	"github.com/Yuuuuu0/x-hub/internal/shared/protocol"
)

// maxPending 为待确认批次上限，超出时丢弃最旧的批次。离线期间每小时只产生一个批次，上限约为 90 天。
const maxPending = 24 * 90

// bucket 为某小时尚未封存的累计量。
type bucket struct {
	Users map[string][2]int64 `json:"users"`
	NICRx int64               `json:"nicRx"`
	NICTx int64               `json:"nicTx"`
}

type state struct {
	NextSeq int64                   `json:"nextSeq"`
	Open    map[int64]*bucket       `json:"open"`
	Pending []protocol.TrafficBatch `json:"pending"`
	// LastNICRx / LastNICTx 为上次采集的网卡累计值，用于计算增量。
	LastNICRx uint64 `json:"lastNicRx"`
	LastNICTx uint64 `json:"lastNicTx"`
}

// Ledger 为线程安全的持久化账本。
type Ledger struct {
	path string
	mu   sync.Mutex
	st   state
}

// Open 加载账本文件，不存在时创建空账本。
func Open(path string) (*Ledger, error) {
	l := &Ledger{path: path, st: state{NextSeq: 1, Open: map[int64]*bucket{}}}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return l, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &l.st); err != nil {
		return nil, err
	}
	if l.st.Open == nil {
		l.st.Open = map[int64]*bucket{}
	}
	if l.st.NextSeq < 1 {
		l.st.NextSeq = 1
	}
	return l, nil
}

// NICDelta 根据网卡累计值计算增量；计数器变小（机器重启）时以当前值为增量。
func (l *Ledger) NICDelta(rx, tx uint64) (int64, int64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delta := func(cur, last uint64) int64 {
		if last == 0 {
			return 0 // 首次采集只建立基线
		}
		if cur < last {
			return int64(cur)
		}
		return int64(cur - last)
	}
	drx, dtx := delta(rx, l.st.LastNICRx), delta(tx, l.st.LastNICTx)
	l.st.LastNICRx, l.st.LastNICTx = rx, tx
	return drx, dtx
}

// Add 把一次采集的增量累计到对应小时并持久化。
func (l *Ledger) Add(hour int64, users map[string][2]int64, nicRx, nicTx int64) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.st.Open[hour]
	if b == nil {
		b = &bucket{Users: map[string][2]int64{}}
		l.st.Open[hour] = b
	}
	for email, v := range users {
		cur := b.Users[email]
		b.Users[email] = [2]int64{cur[0] + v[0], cur[1] + v[1]}
	}
	b.NICRx += nicRx
	b.NICTx += nicTx
	return l.saveLocked()
}

// Seal 把所有未封存的小时累计量封存为新批次，返回全部待确认批次（按序号递增）。
func (l *Ledger) Seal() ([]protocol.TrafficBatch, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.st.Open) > 0 {
		hours := make([]int64, 0, len(l.st.Open))
		for h := range l.st.Open {
			hours = append(hours, h)
		}
		sort.Slice(hours, func(i, j int) bool { return hours[i] < hours[j] })
		for _, h := range hours {
			b := l.st.Open[h]
			if len(b.Users) > 0 || b.NICRx > 0 || b.NICTx > 0 {
				l.st.Pending = append(l.st.Pending, protocol.TrafficBatch{
					Seq: l.st.NextSeq, Hour: h, Users: b.Users, NICRx: b.NICRx, NICTx: b.NICTx,
				})
				l.st.NextSeq++
			}
			delete(l.st.Open, h)
		}
		if n := len(l.st.Pending) - maxPending; n > 0 {
			l.st.Pending = l.st.Pending[n:]
		}
		if err := l.saveLocked(); err != nil {
			return nil, err
		}
	}
	return append([]protocol.TrafficBatch(nil), l.st.Pending...), nil
}

// Ack 删除序号不大于 seq 的批次，并保证之后的序号大于 seq（主控数据保留而被控端账本被清空时）。
func (l *Ledger) Ack(seq int64) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	i := 0
	for i < len(l.st.Pending) && l.st.Pending[i].Seq <= seq {
		i++
	}
	changed := i > 0
	l.st.Pending = l.st.Pending[i:]
	if l.st.NextSeq <= seq {
		l.st.NextSeq = seq + 1
		changed = true
	}
	if !changed {
		return nil
	}
	return l.saveLocked()
}

func (l *Ledger) saveLocked() error {
	b, err := json.Marshal(l.st)
	if err != nil {
		return err
	}
	tmp := l.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, l.path)
}
