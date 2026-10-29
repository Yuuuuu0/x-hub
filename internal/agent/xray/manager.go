// Package xray 管理被控端本地的 Xray：以子进程托管、崩溃自动拉起、版本安装与失败回滚、能力探测。
package xray

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Yuuuuu0/x-hub/internal/shared/protocol"
)

const (
	logLines     = 1000
	stopTimeout  = 5 * time.Second
	stableUptime = 30 * time.Second // 运行超过该时长的退出不计入崩溃退避
	maxBackoff   = time.Minute
)

var versionRe = regexp.MustCompile(`Xray (\d+\.\d+\.\d+)`)

// state 为需要跨被控端重启保留的 Xray 状态。
type state struct {
	Enabled     bool   `json:"enabled"`     // 期望运行
	APIPort     int    `json:"apiPort"`     // Xray gRPC API 本地端口
	AppliedRev  int64  `json:"appliedRev"`  // 已应用的主控配置版本
	AppliedHash string `json:"appliedHash"` // 已应用的主控配置哈希
}

// Manager 托管一个 Xray 子进程。ops 串行化启停与安装操作，mu 保护运行时字段。
type Manager struct {
	dir  string
	log  *slog.Logger
	logs *ring
	ops  sync.Mutex

	changes chan struct{}

	mu        sync.Mutex
	st        state
	version   string
	proc      *exec.Cmd
	done      chan struct{}
	startedAt time.Time
	exitedAt  time.Time
	crashes   int
	lastErr   string
	stopping  bool
	caps      []string
}

// New 创建管理器，dir 为被控端数据目录。
func New(dir string, log *slog.Logger) (*Manager, error) {
	m := &Manager{dir: dir, log: log, logs: newRing(logLines), caps: []string{}, changes: make(chan struct{}, 1)}
	if err := os.MkdirAll(m.binDir(), 0o750); err != nil {
		return nil, err
	}
	if b, err := os.ReadFile(m.statePath()); err == nil {
		json.Unmarshal(b, &m.st)
	}
	if m.st.APIPort == 0 {
		port, err := freePort()
		if err != nil {
			return nil, err
		}
		m.st.APIPort = port
		if err := m.saveState(); err != nil {
			return nil, err
		}
	}
	m.version = detectVersion(m.binPath())
	return m, nil
}

func (m *Manager) binDir() string     { return filepath.Join(m.dir, "bin") }
func (m *Manager) binPath() string    { return filepath.Join(m.dir, "bin", "xray") }
func (m *Manager) configPath() string { return filepath.Join(m.dir, "config.json") }
func (m *Manager) statePath() string  { return filepath.Join(m.dir, "xray-state.json") }

// Status 返回当前进程状态。
func (m *Manager) Status() protocol.XrayStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := protocol.XrayStatus{
		Installed: m.version != "",
		Version:   m.version,
		Enabled:   m.st.Enabled,
		Running:   m.proc != nil,
		LastError: m.lastErr,
	}
	if m.proc != nil {
		s.StartedAt = m.startedAt.Unix()
	}
	return s
}

// Caps 返回最近一次探测到的能力列表。
func (m *Manager) Caps() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.caps
}

// Changes 在进程状态或能力发生变化时收到通知，用于立即上报心跳。
func (m *Manager) Changes() <-chan struct{} { return m.changes }

func (m *Manager) notify() {
	select {
	case m.changes <- struct{}{}:
	default:
	}
}

// Logs 返回最近 n 行 Xray 输出。
func (m *Manager) Logs(n int) []string { return m.logs.Tail(n) }

// Start 标记为期望运行并启动进程。
func (m *Manager) Start() error {
	m.ops.Lock()
	defer m.ops.Unlock()
	if err := m.setEnabled(true); err != nil {
		return err
	}
	if m.running() {
		return nil
	}
	return m.startProc()
}

// Stop 标记为不运行并停止进程。
func (m *Manager) Stop() error {
	m.ops.Lock()
	defer m.ops.Unlock()
	if err := m.setEnabled(false); err != nil {
		return err
	}
	m.stopProc()
	return nil
}

// Restart 重启进程并标记为期望运行。
func (m *Manager) Restart() error {
	m.ops.Lock()
	defer m.ops.Unlock()
	if err := m.setEnabled(true); err != nil {
		return err
	}
	m.stopProc()
	return m.startProc()
}

// Supervise 守护进程：期望运行但进程不在时按崩溃退避重新拉起，直到 ctx 结束。
func (m *Manager) Supervise(ctx context.Context) {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			m.ops.Lock()
			m.stopProc()
			m.ops.Unlock()
			return
		case <-t.C:
		}
		m.mu.Lock()
		want := m.st.Enabled && m.version != "" && m.proc == nil
		backoff := min(time.Duration(1<<min(m.crashes, 6))*time.Second, maxBackoff)
		due := time.Since(m.exitedAt) >= backoff
		m.mu.Unlock()
		if !want || !due {
			continue
		}
		m.ops.Lock()
		if !m.running() {
			if err := m.startProc(); err != nil {
				m.log.Warn("xray restart failed", "err", err)
			}
		}
		m.ops.Unlock()
	}
}

func (m *Manager) running() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.proc != nil
}

func (m *Manager) setEnabled(v bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.st.Enabled == v {
		return nil
	}
	m.st.Enabled = v
	return m.saveState()
}

func (m *Manager) saveState() error {
	b, _ := json.Marshal(m.st)
	return os.WriteFile(m.statePath(), b, 0o600)
}

// startProc 校验配置并启动 Xray 子进程，调用方需持有 ops。
func (m *Manager) startProc() error {
	if m.Status().Version == "" {
		return errors.New("Xray 尚未安装")
	}
	if err := m.ensureConfig(); err != nil {
		return err
	}
	if out, err := m.command("run", "-test", "-c", m.configPath()).CombinedOutput(); err != nil {
		m.setErr("配置校验失败：" + lastLine(string(out)))
		return fmt.Errorf("配置校验失败：%s", lastLine(string(out)))
	}
	cmd := m.command("run", "-c", m.configPath())
	cmd.Stdout, cmd.Stderr = m.logs, m.logs
	cmd.SysProcAttr = procAttr()
	if err := cmd.Start(); err != nil {
		m.setErr(err.Error())
		return err
	}
	done := make(chan struct{})
	m.mu.Lock()
	m.proc, m.done, m.startedAt, m.lastErr, m.stopping = cmd, done, time.Now(), "", false
	m.mu.Unlock()
	m.log.Info("xray started", "pid", cmd.Process.Pid, "version", m.version)
	m.notify()

	go func() {
		err := cmd.Wait()
		m.mu.Lock()
		if !m.stopping {
			m.lastErr = fmt.Sprintf("进程意外退出：%v", err)
			if time.Since(m.startedAt) < stableUptime {
				m.crashes++
			} else {
				m.crashes = 0
			}
			m.log.Warn("xray exited", "err", err)
		}
		m.proc, m.exitedAt = nil, time.Now()
		m.mu.Unlock()
		close(done)
		m.notify()
	}()
	go m.probe(m.st.APIPort, done)
	return nil
}

// stopProc 先 SIGTERM，超时后强杀，调用方需持有 ops。
func (m *Manager) stopProc() {
	m.mu.Lock()
	proc, done := m.proc, m.done
	m.stopping = true
	m.crashes = 0
	m.mu.Unlock()
	if proc == nil {
		return
	}
	proc.Process.Signal(syscall.SIGTERM)
	select {
	case <-done:
	case <-time.After(stopTimeout):
		proc.Process.Kill()
		<-done
	}
	m.log.Info("xray stopped")
}

func (m *Manager) command(args ...string) *exec.Cmd {
	cmd := exec.Command(m.binPath(), args...)
	cmd.Dir = m.dir
	cmd.Env = append(os.Environ(), "XRAY_LOCATION_ASSET="+m.binDir())
	return cmd
}

func (m *Manager) setErr(msg string) {
	m.mu.Lock()
	m.lastErr = msg
	m.mu.Unlock()
	m.notify()
}

// ensureConfig 在本地没有配置时写入仅含自管段的基础配置，保证 Xray 可先行启动。
func (m *Manager) ensureConfig() error {
	if _, err := os.Stat(m.configPath()); err == nil {
		return nil
	}
	b, err := m.withManagement([]byte(`{"log":{"loglevel":"warning","access":"none"},"inbounds":[],` +
		`"outbounds":[{"protocol":"freedom","tag":"direct"},{"protocol":"blackhole","tag":"block"}]}`))
	if err != nil {
		return err
	}
	return os.WriteFile(m.configPath(), b, 0o600)
}

// detectVersion 执行 xray version 解析版本号，未安装或无法执行时返回空。
func detectVersion(bin string) string {
	out, err := exec.Command(bin, "version").Output()
	if err != nil {
		return ""
	}
	if mm := versionRe.FindStringSubmatch(string(out)); mm != nil {
		return "v" + mm[1]
	}
	return ""
}

func freePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}
