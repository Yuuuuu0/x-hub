// Package config 管理被控端的本地配置：节点凭据、主控地址候选列表与数据目录。
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/Yuuuuu0/x-hub/internal/shared/protocol"
)

const maxEndpoints = 3

// Config 为被控端配置。Endpoints 按优先级排列，首项为主控当前推荐地址，其余为回退地址。
type Config struct {
	NodeID    int64               `json:"nodeId"`
	Secret    string              `json:"secret"`
	Endpoints []protocol.Endpoint `json:"endpoints"`
	DataDir   string              `json:"dataDir"`

	path string
	mu   sync.Mutex
}

// Load 读取配置文件。
func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	c := &Config{path: path}
	if err := json.Unmarshal(b, c); err != nil {
		return nil, err
	}
	return c, nil
}

// New 创建尚未保存的配置。
func New(path string) *Config { return &Config{path: path} }

// Save 原子写入配置文件（权限 0600）。
func (c *Config) Save() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.saveLocked()
}

func (c *Config) saveLocked() error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := c.path + ".tmp"
	if err := os.MkdirAll(filepath.Dir(c.path), 0o750); err != nil {
		return err
	}
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, c.path)
}

// EndpointList 返回地址候选列表副本。
func (c *Config) EndpointList() []protocol.Endpoint {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.Endpoints)
}

// Promote 把主控推荐的地址置为首选，原有地址保留为回退，列表有变化时保存。
func (c *Config) Promote(ep protocol.Endpoint) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if ep.URL == "" || (len(c.Endpoints) > 0 && c.Endpoints[0] == ep) {
		return nil
	}
	list := []protocol.Endpoint{ep}
	for _, e := range c.Endpoints {
		if e != ep && len(list) < maxEndpoints {
			list = append(list, e)
		}
	}
	c.Endpoints = list
	return c.saveLocked()
}

// KeepOnly 在首选地址连接成功且被主控确认后，丢弃回退地址。
func (c *Config) KeepOnly(ep protocol.Endpoint) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.Endpoints) == 1 && c.Endpoints[0] == ep {
		return nil
	}
	c.Endpoints = []protocol.Endpoint{ep}
	return c.saveLocked()
}
