package xray

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"

	"github.com/Yuuuuu0/x-hub/internal/shared/protocol"
)

// Applied 返回已应用的主控配置版本与哈希。
func (m *Manager) Applied() (int64, string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.st.AppliedRev, m.st.AppliedHash
}

// Apply 应用主控下发的配置：注入自管段、校验、原子替换，并在 Xray 期望运行时重启生效。
// 版本号低于已应用版本的配置视为过期并忽略。
func (m *Manager) Apply(rev int64, hash string, cfg []byte) error {
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
	if err := os.Rename(tmp, m.configPath()); err != nil {
		return err
	}
	m.mu.Lock()
	m.st.AppliedRev, m.st.AppliedHash = rev, hash
	enabled := m.st.Enabled
	err = m.saveState()
	m.mu.Unlock()
	if err != nil {
		return err
	}
	m.notify()
	if !enabled || !m.Status().Installed {
		return nil
	}
	m.stopProc()
	return m.startProc()
}

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
