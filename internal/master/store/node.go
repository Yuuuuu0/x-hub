package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/Yuuuuu0/x-hub/internal/shared/protocol"
)

// Node 为一台被控服务器。
type Node struct {
	ID           int64             `json:"id"`
	Name         string            `json:"name"`
	Remark       string            `json:"remark"`
	Sort         int               `json:"sort"`
	PublicHost   string            `json:"publicHost"`
	Enrolled     bool              `json:"enrolled"`
	AgentVersion string            `json:"agentVersion"`
	XrayVersion  string            `json:"xrayVersion"`
	Caps         []string          `json:"caps"`
	Host         protocol.HostInfo `json:"host"`
	LastIP       string            `json:"lastIp"`
	LastSeenAt   int64             `json:"lastSeenAt"`
	CreatedAt    int64             `json:"createdAt"`

	secretHash string
}

// SecretHash 返回节点凭据哈希。
func (n *Node) SecretHash() string { return n.secretHash }

const nodeColumns = `id, name, remark, sort, public_host, secret_hash, agent_version, xray_version,
	capabilities_json, host_info_json, last_ip, last_seen_at, created_at`

type rowScanner interface{ Scan(dest ...any) error }

func scanNode(r rowScanner) (*Node, error) {
	var n Node
	var caps, host string
	if err := r.Scan(&n.ID, &n.Name, &n.Remark, &n.Sort, &n.PublicHost, &n.secretHash, &n.AgentVersion,
		&n.XrayVersion, &caps, &host, &n.LastIP, &n.LastSeenAt, &n.CreatedAt); err != nil {
		return nil, err
	}
	n.Enrolled = n.secretHash != ""
	_ = json.Unmarshal([]byte(caps), &n.Caps)
	_ = json.Unmarshal([]byte(host), &n.Host)
	if n.Caps == nil {
		n.Caps = []string{}
	}
	return &n, nil
}

// ListNodes 按排序与 ID 返回全部服务器。
func (s *Store) ListNodes() ([]*Node, error) {
	rows, err := s.db.Query("SELECT " + nodeColumns + " FROM nodes ORDER BY sort, id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Node
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// GetNode 按 ID 查询服务器。
func (s *Store) GetNode(id int64) (*Node, error) {
	n, err := scanNode(s.db.QueryRow("SELECT "+nodeColumns+" FROM nodes WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return n, err
}

// CreateNode 新建服务器记录，返回新 ID。
func (s *Store) CreateNode(name, remark, publicHost string) (int64, error) {
	res, err := s.db.Exec("INSERT INTO nodes (name, remark, public_host, created_at) VALUES (?, ?, ?, ?)",
		name, remark, publicHost, time.Now().Unix())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateNode 更新服务器的可编辑字段。
func (s *Store) UpdateNode(id int64, name, remark, publicHost string) error {
	return s.execOne("UPDATE nodes SET name = ?, remark = ?, public_host = ? WHERE id = ?", name, remark, publicHost, id)
}

// DeleteNode 删除服务器。
func (s *Store) DeleteNode(id int64) error {
	return s.execOne("DELETE FROM nodes WHERE id = ?", id)
}

// UpdateNodeRuntime 记录被控端上报的运行时信息。publicHost 为空时用连接来源 IP 补全。
func (s *Store) UpdateNodeRuntime(id int64, agentVersion, xrayVersion string, caps []string, host protocol.HostInfo, ip string) error {
	capsJSON, _ := json.Marshal(caps)
	hostJSON, _ := json.Marshal(host)
	_, err := s.db.Exec(`UPDATE nodes SET agent_version = ?, xray_version = ?, capabilities_json = ?, host_info_json = ?,
		last_ip = ?, last_seen_at = ?, public_host = CASE WHEN public_host = '' THEN ? ELSE public_host END WHERE id = ?`,
		agentVersion, xrayVersion, string(capsJSON), string(hostJSON), ip, time.Now().Unix(), ip, id)
	return err
}

// UpdateNodeXray 更新 Xray 版本与能力，在心跳中发现变化时调用。
func (s *Store) UpdateNodeXray(id int64, xrayVersion string, caps []string) error {
	capsJSON, _ := json.Marshal(caps)
	_, err := s.db.Exec("UPDATE nodes SET xray_version = ?, capabilities_json = ?, last_seen_at = ? WHERE id = ?",
		xrayVersion, string(capsJSON), time.Now().Unix(), id)
	return err
}

// TouchNode 刷新最后在线时间。
func (s *Store) TouchNode(id int64) error {
	_, err := s.db.Exec("UPDATE nodes SET last_seen_at = ? WHERE id = ?", time.Now().Unix(), id)
	return err
}

// CreateEnrollToken 为服务器生成一次性接入令牌，同时作废其此前未使用的令牌。
func (s *Store) CreateEnrollToken(nodeID int64, tokenHash string, ttl time.Duration) (int64, error) {
	expire := time.Now().Add(ttl).Unix()
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec("DELETE FROM enroll_tokens WHERE node_id = ? AND used_at IS NULL", nodeID); err != nil {
		return 0, err
	}
	if _, err := tx.Exec("INSERT INTO enroll_tokens (node_id, token_hash, expire_at) VALUES (?, ?, ?)", nodeID, tokenHash, expire); err != nil {
		return 0, err
	}
	return expire, tx.Commit()
}

// ConsumeEnrollToken 校验并消费接入令牌，把新的节点凭据哈希写入对应服务器，返回服务器 ID。
func (s *Store) ConsumeEnrollToken(tokenHash, secretHash string) (int64, error) {
	now := time.Now().Unix()
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var id, nodeID int64
	err = tx.QueryRow("SELECT id, node_id FROM enroll_tokens WHERE token_hash = ? AND used_at IS NULL AND expire_at > ?",
		tokenHash, now).Scan(&id, &nodeID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	if _, err := tx.Exec("UPDATE enroll_tokens SET used_at = ? WHERE id = ?", now, id); err != nil {
		return 0, err
	}
	if _, err := tx.Exec("UPDATE nodes SET secret_hash = ? WHERE id = ?", secretHash, nodeID); err != nil {
		return 0, err
	}
	return nodeID, tx.Commit()
}

func (s *Store) execOne(query string, args ...any) error {
	res, err := s.db.Exec(query, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
