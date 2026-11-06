package xray

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"

	"github.com/Yuuuuu0/x-hub/internal/agent/fsutil"
	"github.com/Yuuuuu0/x-hub/internal/shared/protocol"
)

// Applied 返回已应用的主控配置版本与哈希。
func (m *Manager) Applied() (int64, string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.st.AppliedRev, m.st.AppliedHash
}

// Apply 应用主控下发的配置：注入自管段、校验、原子替换，并在 Xray 期望运行时在线增删用户或重启生效。
// 重启后在 startCheck 内退出（如端口被占用）视为失败：还原上一份配置并重启，返回错误且不记录版本；
// 新配置确认生效后才记录已应用版本并保存。
// 版本号低于已应用版本的配置视为过期并忽略。commit 为 false 时（配置被部分裁剪）不记录为已应用版本。
func (m *Manager) Apply(rev int64, hash string, cfg []byte, commit bool) error {
	m.ops.Lock()
	defer m.ops.Unlock()
	if cur, _ := m.Applied(); rev < cur {
		return nil
	}
	full, err := m.withManagement(cfg)
	if err != nil {
		return err
	}
	// Xray 按扩展名识别配置格式，临时文件需保留 .json 后缀。
	tmp := filepath.Join(m.dir, "config.pending.json")
	if err := os.WriteFile(tmp, full, 0o600); err != nil {
		return err
	}
	defer os.Remove(tmp)
	// 未安装 Xray 时无法校验，安装后首次启动会再次校验。
	if m.Status().Installed {
		if out, err := m.command("run", "-test", "-c", tmp).CombinedOutput(); err != nil {
			return fmt.Errorf("配置校验失败：%s", lastLine(string(out)))
		}
	}
	prevMaster, _ := os.ReadFile(m.masterConfigPath())
	prevFull, prevErr := os.ReadFile(m.configPath())
	if err := fsutil.WriteFile(m.configPath(), full, 0o600); err != nil {
		return err
	}
	if err := m.activate(prevMaster, cfg); err != nil {
		m.log.Warn("apply config failed, restoring previous config", "rev", rev, "err", err)
		m.rollback(prevFull, prevErr == nil)
		return fmt.Errorf("新配置启动失败，已还原上一份配置：%w", err)
	}
	if err := fsutil.WriteFile(m.masterConfigPath(), cfg, 0o600); err != nil {
		return err
	}
	m.mu.Lock()
	if commit {
		m.st.AppliedRev, m.st.AppliedHash = rev, hash
	}
	err = m.saveState()
	m.mu.Unlock()
	if err != nil {
		return err
	}
	m.notify()
	return nil
}

// activate 让已写入本地的新配置生效，调用方需持有 ops。Xray 未期望运行或未安装时无需操作；
// 只有用户变化时在线增删，不中断其他连接，失败则回退为重启；重启后等待进程稳定，期间退出返回错误。
// prevMaster 为上一次应用的主控原始配置，cfg 为本次主控原始配置。
func (m *Manager) activate(prevMaster, cfg []byte) error {
	m.mu.Lock()
	enabled := m.st.Enabled
	m.mu.Unlock()
	if !enabled || !m.Status().Installed {
		return nil
	}
	if m.running() && prevMaster != nil {
		if ops, ok := usersOnlyDiff(prevMaster, cfg); ok {
			if len(ops) == 0 {
				return nil
			}
			err := m.alterUsers(ops)
			if err == nil {
				m.log.Info("xray users updated via api", "ops", len(ops))
				return nil
			}
			m.log.Warn("alter users failed, restarting xray", "err", err)
		}
	}
	m.stopProc()
	if err := m.startProc(); err != nil {
		return err
	}
	return m.waitStable(startCheck)
}

// rollback 把 Xray 配置还原为 prev 并重启，调用方需持有 ops。had 为 false 表示此前没有配置文件，
// 此时删除配置，由启动时 ensureConfig 写入基础配置。重启失败交由 Supervise 按退避重试。
func (m *Manager) rollback(prev []byte, had bool) {
	m.stopProc()
	var err error
	if had {
		err = fsutil.WriteFile(m.configPath(), prev, 0o600)
	} else {
		err = os.Remove(m.configPath())
	}
	if err != nil {
		m.log.Error("restore xray config", "err", err)
	}
	if err := m.startProc(); err != nil {
		m.log.Warn("restart xray with previous config failed", "err", err)
	}
}

// masterConfigPath 为最近一次应用的主控原始配置，用于判断下次变更是否只涉及用户。
func (m *Manager) masterConfigPath() string { return filepath.Join(m.dir, "master-config.json") }

// withManagement 向主控配置注入被控端自管的 api / stats / policy 段。
func (m *Manager) withManagement(cfg []byte) ([]byte, error) {
	var c map[string]any
	if err := json.Unmarshal(cfg, &c); err != nil {
		return nil, fmt.Errorf("配置格式错误：%w", err)
	}
	c["api"] = map[string]any{
		"tag":      "api",
		"listen":   fmt.Sprintf("127.0.0.1:%d", m.st.APIPort),
		"services": []string{"HandlerService", "StatsService"},
	}
	c["stats"] = map[string]any{}
	c["policy"] = map[string]any{
		"levels": map[string]any{"0": map[string]any{
			"statsUserUplink": true, "statsUserDownlink": true, "statsUserOnline": true,
		}},
	}
	return json.MarshalIndent(c, "", "  ")
}

// PortStatus 检查端口是否可被新入站使用：空闲，或已被当前配置中的入站占用。
func (m *Manager) PortStatus(port int) protocol.ProbePortResult {
	if ln, err := net.Listen("tcp", ":"+strconv.Itoa(port)); err == nil {
		ln.Close()
		return protocol.ProbePortResult{Free: true}
	}
	var c struct {
		Inbounds []struct {
			Port json.RawMessage `json:"port"`
		} `json:"inbounds"`
	}
	if b, err := os.ReadFile(m.configPath()); err == nil && json.Unmarshal(b, &c) == nil {
		for _, in := range c.Inbounds {
			if string(in.Port) == strconv.Itoa(port) {
				return protocol.ProbePortResult{Owned: true}
			}
		}
	}
	return protocol.ProbePortResult{}
}
