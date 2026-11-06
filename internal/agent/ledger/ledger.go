// Package ledger 是被控端的本地流量账本：采集增量按小时累计，上报前封存为带序号的批次，
// 收到主控确认后才删除，从而在断网或主控不可用期间不丢流量。
package ledger

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/Yuuuuu0/x-hub/internal/agent/fsutil"
	"github.com/Yuuuuu0/x-hub/internal/shared/protocol"
)

// maxPending 为待确认批次上限，超出时丢弃最旧的批次。
// 存在待确认批次时不再封存新批次，未封存的流量按小时累计，因此每小时最多产生一个批次（超大批次拆分除外），上限约为 90 天。
const maxPending = 24 * 90

// maxBatchBytes 为单个批次序列化后的大小上限，超出时按用户拆分为同一小时的多个批次，保证每条上报消息可容纳至少一个批次。
var maxBatchBytes = 1 << 20

// ErrCorrupt 表示账本文件内容无法解析。
var ErrCorrupt = errors.New("流量账本文件已损坏")

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
	// NICs 为各物理网卡上次采集的累计值 [收, 发]，用于按网卡计算增量。
	NICs map[string][2]uint64 `json:"nics,omitempty"`
	// LastNICRx / LastNICTx 为旧版本记录的网卡累计总量，仅用于升级后首次采集的平滑过渡，过渡后清零。
	LastNICRx uint64 `json:"lastNicRx,omitempty"`
	LastNICTx uint64 `json:"lastNicTx,omitempty"`
}

// Ledger 为线程安全的持久化账本。
type Ledger struct {
	path string
	mu   sync.Mutex
	st   state
}

// Open 加载账本文件，不存在时创建空账本；内容无法解析时返回包装了 ErrCorrupt 的错误。
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
		return nil, fmt.Errorf("%w：%v", ErrCorrupt, err)
	}
	if l.st.Open == nil {
		l.st.Open = map[int64]*bucket{}
	}
	if l.st.NextSeq < 1 {
		l.st.NextSeq = 1
	}
	return l, nil
}

// Quarantine 把损坏的账本文件改名为 <path>.corrupt-<时间戳> 留存，返回新文件名，之后可用 Open 创建新账本。
func Quarantine(path string) (string, error) {
	dst := fmt.Sprintf("%s.corrupt-%s", path, time.Now().Format("20060102-150405"))
	return dst, os.Rename(path, dst)
}

// NICDelta 按网卡分别计算与上次采集的增量并汇总，counters 为各物理网卡的累计值 [收, 发]。
// 新出现的网卡首次只建立基线；某块网卡计数变小（重启或网卡重建）时以其当前值为增量。
// 读取失败（counters 为空）时不更新基线。
func (l *Ledger) NICDelta(counters map[string][2]uint64) (int64, int64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(counters) == 0 {
		return 0, 0
	}
	var rx, tx int64
	if l.st.NICs == nil {
		rx, tx = l.legacyDelta(counters)
	} else {
		for name, cur := range counters {
			last, ok := l.st.NICs[name]
			if !ok {
				continue // 新网卡首次只建立基线
			}
			rx += counterDelta(cur[0], last[0])
			tx += counterDelta(cur[1], last[1])
		}
	}
	l.st.NICs = counters
	return rx, tx
}

// legacyDelta 处理旧账本只记录了总量时的首次采集：总量未变小时按总量差值计入，
// 变小时无法区分是哪块网卡重置，只建立基线以免多记。全新账本（无旧总量）同样只建立基线。
func (l *Ledger) legacyDelta(counters map[string][2]uint64) (int64, int64) {
	var sumRx, sumTx uint64
	for _, v := range counters {
		sumRx += v[0]
		sumTx += v[1]
	}
	var rx, tx int64
	if l.st.LastNICRx > 0 && sumRx >= l.st.LastNICRx {
		rx = int64(sumRx - l.st.LastNICRx)
	}
	if l.st.LastNICTx > 0 && sumTx >= l.st.LastNICTx {
		tx = int64(sumTx - l.st.LastNICTx)
	}
	l.st.LastNICRx, l.st.LastNICTx = 0, 0
	return rx, tx
}

// counterDelta 计算单个计数器的增量，计数器变小视为重置，以当前值为增量。
func counterDelta(cur, last uint64) int64 {
	if cur < last {
		return int64(cur)
	}
	return int64(cur - last)
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

// Seal 返回全部待确认批次（按序号递增）。仅当没有待确认批次时，才把未封存的小时累计量封存为新批次；
// 否则新增流量继续按小时累计，等待主控确认后再封存，避免主控迟迟不确认时每个上报周期都新建批次而很快触及上限。
func (l *Ledger) Seal() ([]protocol.TrafficBatch, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.st.Pending) == 0 && len(l.st.Open) > 0 {
		hours := make([]int64, 0, len(l.st.Open))
		for h := range l.st.Open {
			hours = append(hours, h)
		}
		sort.Slice(hours, func(i, j int) bool { return hours[i] < hours[j] })
		for _, h := range hours {
			b := l.st.Open[h]
			if len(b.Users) > 0 || b.NICRx > 0 || b.NICTx > 0 {
				for _, batch := range splitBucket(h, b) {
					batch.Seq = l.st.NextSeq
					l.st.Pending = append(l.st.Pending, batch)
					l.st.NextSeq++
				}
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

// splitBucket 把一个小时的累计量转为批次（序号由调用方填写）。序列化后超过 maxBatchBytes 时按用户拆分为多个批次，
// 网卡流量记在第一个批次。
func splitBucket(hour int64, b *bucket) []protocol.TrafficBatch {
	first := protocol.TrafficBatch{Hour: hour, Users: b.Users, NICRx: b.NICRx, NICTx: b.NICTx}
	if raw, err := json.Marshal(first); err == nil && len(raw) <= maxBatchBytes {
		return []protocol.TrafficBatch{first}
	}
	emails := make([]string, 0, len(b.Users))
	for e := range b.Users {
		emails = append(emails, e)
	}
	sort.Strings(emails)
	const baseSize, entryOverhead = 128, 48 // 批次固定字段与每个用户条目（引号、两个 int64 等）的保守估算
	var out []protocol.TrafficBatch
	cur := protocol.TrafficBatch{Hour: hour, Users: map[string][2]int64{}, NICRx: b.NICRx, NICTx: b.NICTx}
	size := baseSize
	for _, e := range emails {
		entry := len(e) + entryOverhead
		if len(cur.Users) > 0 && size+entry > maxBatchBytes {
			out = append(out, cur)
			cur = protocol.TrafficBatch{Hour: hour, Users: map[string][2]int64{}}
			size = baseSize
		}
		cur.Users[e] = b.Users[e]
		size += entry
	}
	return append(out, cur)
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

// saveLocked 原子写入账本文件，调用方需持有 mu。
func (l *Ledger) saveLocked() error {
	b, err := json.Marshal(l.st)
	if err != nil {
		return err
	}
	return fsutil.WriteFile(l.path, b, 0o600)
}
