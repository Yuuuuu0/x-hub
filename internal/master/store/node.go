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
	DesiredRev   int64             `json:"desiredRev"`
	AppliedRev   int64             `json:"appliedRev"`
	AppliedHash  string            `json:"appliedHash"`
	SyncError    string            `json:"syncError"`
	LastSeq      int64             `json:"-"`
	NetResetDay  int               `json:"netResetDay"`       // 服务器流量计费周期的重置日（1-28）
	NetQuota     int64             `json:"netQuotaBytes"`     // 服务器每周期流量配额，0 表示不限
	XrayPinned   string            `json:"xrayPinnedVersion"` // 锁定的 Xray 版本，批量升级时跳过
	Country      string            `json:"country"`           // 手动指定的国家或地区代码，空表示使用自动检测结果
	Region       string            `json:"region"`            // 自定义地区名称，空表示按国家或地区代码显示
	ExpireAt     int64             `json:"expireAt"`          // VPS 到期时间，0 表示未设置
	NetCountMode string            `json:"netCountMode"`      // 流量计费方式：both / tx / max

	secretHash string
}

// SecretHash 返回节点凭据哈希。
func (n *Node) SecretHash() string { return n.secretHash }

const nodeColumns = `id, name, remark, sort, public_host, secret_hash, agent_version, xray_version,
	capabilities_json, host_info_json, last_ip, last_seen_at, created_at, desired_rev, applied_rev, applied_hash, sync_error,
	last_seq, net_reset_day, net_quota_bytes, xray_pinned_version, country, region, expire_at, net_count_mode`

type rowScanner interface{ Scan(dest ...any) error }

func scanNode(r rowScanner) (*Node, error) {
	var n Node
	var caps, host string
	if err := r.Scan(&n.ID, &n.Name, &n.Remark, &n.Sort, &n.PublicHost, &n.secretHash, &n.AgentVersion,
		&n.XrayVersion, &caps, &host, &n.LastIP, &n.LastSeenAt, &n.CreatedAt, &n.DesiredRev, &n.AppliedRev,
		&n.AppliedHash, &n.SyncError, &n.LastSeq, &n.NetResetDay, &n.NetQuota, &n.XrayPinned, &n.Country, &n.Region,
		&n.ExpireAt, &n.NetCountMode); err != nil {
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
	return s.insertWithID("nodes", `INSERT INTO nodes (id, name, remark, sort, public_host, created_at)
		VALUES (?, ?, ?, (SELECT COALESCE(MAX(sort), -1) + 1 FROM nodes), ?, ?)`,
		name, remark, publicHost, time.Now().Unix())
}

// ReorderNodes 按 ids 的先后设置服务器顺序。
func (s *Store) ReorderNodes(ids []int64) error { return s.reorder("nodes", ids) }

// NodeSettings 为服务器的可编辑字段。
type NodeSettings struct {
	Name         string `json:"name"`
	Remark       string `json:"remark"`
	PublicHost   string `json:"publicHost"`
	NetResetDay  int    `json:"netResetDay"`
	NetQuota     int64  `json:"netQuotaBytes"`
	NetCountMode string `json:"netCountMode"`
	Country      string `json:"country"`
	Region       string `json:"region"`
	ExpireAt     int64  `json:"expireAt"`
}

// UpdateNode 更新服务器的可编辑字段。
func (s *Store) UpdateNode(id int64, in *NodeSettings) error {
	return s.execOne(`UPDATE nodes SET name = ?, remark = ?, public_host = ?, net_reset_day = ?, net_quota_bytes = ?, net_count_mode = ?,
		country = ?, region = ?, expire_at = ? WHERE id = ?`,
		in.Name, in.Remark, in.PublicHost, in.NetResetDay, in.NetQuota, in.NetCountMode, in.Country, in.Region, in.ExpireAt, id)
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

// SetNodeXrayPin 锁定或解除锁定（空字符串）服务器的 Xray 版本。
func (s *Store) SetNodeXrayPin(id int64, version string) error {
	return s.execOne("UPDATE nodes SET xray_pinned_version = ? WHERE id = ?", version, id)
}

// NextDesiredRev 递增并返回服务器的期望配置版本号。
func (s *Store) NextDesiredRev(id int64) (int64, error) {
	var rev int64
	err := s.db.QueryRow("UPDATE nodes SET desired_rev = desired_rev + 1 WHERE id = ? RETURNING desired_rev", id).Scan(&rev)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return rev, err
}

// SetNodeApplied 记录被控端已应用的配置版本与哈希，并清除同步错误。
// 期望版本号至少追平已应用版本，避免主控数据回退后下发的版本被被控端当作过期配置忽略。
func (s *Store) SetNodeApplied(id, rev int64, hash string) error {
	_, err := s.db.Exec(`UPDATE nodes SET applied_rev = ?, applied_hash = ?, sync_error = '',
		desired_rev = MAX(desired_rev, ?) WHERE id = ?`, rev, hash, rev, id)
	return err
}

// SetNodeSyncError 记录最近一次下发失败的原因。
func (s *Store) SetNodeSyncError(id int64, msg string) error {
	_, err := s.db.Exec("UPDATE nodes SET sync_error = ? WHERE id = ?", msg, id)
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
