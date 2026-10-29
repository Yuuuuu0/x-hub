package xray

import (
	"bytes"
	"sync"
)

// ring 是按行保存最近输出的环形缓冲，实现 io.Writer 以接收 Xray 的标准输出。
type ring struct {
	mu      sync.Mutex
	lines   []string
	next    int
	full    bool
	partial []byte
}

func newRing(size int) *ring { return &ring{lines: make([]string, size)} }

func (r *ring) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	data := append(r.partial, p...)
	for {
		i := bytes.IndexByte(data, '\n')
		if i < 0 {
			break
		}
		r.push(string(bytes.TrimRight(data[:i], "\r")))
		data = data[i+1:]
	}
	if len(data) > 4096 { // 防止无换行的超长输出撑大缓冲
		r.push(string(data))
		data = nil
	}
	r.partial = append([]byte(nil), data...)
	return len(p), nil
}

func (r *ring) push(line string) {
	r.lines[r.next] = line
	r.next = (r.next + 1) % len(r.lines)
	if r.next == 0 {
		r.full = true
	}
}

// Tail 返回最近 n 行，按时间先后排列。
func (r *ring) Tail(n int) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	count := r.next
	if r.full {
		count = len(r.lines)
	}
	if n > count {
		n = count
	}
	out := make([]string, 0, n)
	for i := r.next - n; i < r.next; i++ {
		out = append(out, r.lines[(i+len(r.lines))%len(r.lines)])
	}
	return out
}
