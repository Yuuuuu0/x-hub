package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// ErrPortConflict 表示同一服务器上端口重复。
var ErrPortConflict = errors.New("该服务器上已有入站使用此端口")

// Inbound 为一台服务器上的一个 Xray 入站。Spec 为按协议组合定义的有类型结构的 JSON。
type Inbound struct {
	ID        int64           `json:"id"`
	NodeID    int64           `json:"nodeId"`
	Remark    string          `json:"remark"`
	Sort      int             `json:"sort"`
	Enabled   bool            `json:"enabled"`
	Listen    string          `json:"listen"`
	Port      int             `json:"port"`
	Protocol  string          `json:"protocol"`
	Spec      json.RawMessage `json:"spec"`
	LinkHost  string          `json:"linkHost"`
	LinkPort  int             `json:"linkPort"`
	CreatedAt int64           `json:"createdAt"`
	UpdatedAt int64           `json:"updatedAt"`
}

const inboundColumns = `id, node_id, remark, sort, enabled, listen, port, protocol, spec_json, link_host, link_port, created_at, updated_at`

func scanInbound(r rowScanner) (*Inbound, error) {
	var in Inbound
	var spec string
	if err := r.Scan(&in.ID, &in.NodeID, &in.Remark, &in.Sort, &in.Enabled, &in.Listen, &in.Port, &in.Protocol,
		&spec, &in.LinkHost, &in.LinkPort, &in.CreatedAt, &in.UpdatedAt); err != nil {
		return nil, err
	}
	in.Spec = json.RawMessage(spec)
	return &in, nil
}

func (s *Store) queryInbounds(clause string, args ...any) ([]*Inbound, error) {
	rows, err := s.db.Query("SELECT "+inboundColumns+" FROM inbounds "+clause, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Inbound
	for rows.Next() {
		in, err := scanInbound(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, in)
	}
	return out, rows.Err()
}

// ListInbounds 按面板设定的顺序返回全部入站，订阅按此顺序输出节点。
func (s *Store) ListInbounds() ([]*Inbound, error) {
	return s.queryInbounds("ORDER BY sort, id")
}

// ListNodeInbounds 按 ID 返回某台服务器的入站，用于编译配置：顺序不随展示顺序变化，
// 否则调整顺序会改变配置哈希，导致重新下发并重启 Xray。
func (s *Store) ListNodeInbounds(nodeID int64) ([]*Inbound, error) {
	return s.queryInbounds("WHERE node_id = ? ORDER BY id", nodeID)
}

// ReorderInbounds 按 ids 的先后设置入站顺序。
func (s *Store) ReorderInbounds(ids []int64) error { return s.reorder("inbounds", ids) }

// GetInbound 按 ID 查询入站。
func (s *Store) GetInbound(id int64) (*Inbound, error) {
	in, err := scanInbound(s.db.QueryRow("SELECT "+inboundColumns+" FROM inbounds WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return in, err
}

// CreateInbound 新建入站并排在最后，返回新 ID。
func (s *Store) CreateInbound(in *Inbound) (int64, error) {
	now := time.Now().Unix()
	id, err := s.insertWithID("inbounds", `INSERT INTO inbounds (id, node_id, remark, sort, enabled, listen, port, protocol, spec_json, link_host, link_port, created_at, updated_at)
		VALUES (?, ?, ?, (SELECT COALESCE(MAX(sort), -1) + 1 FROM inbounds), ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		in.NodeID, in.Remark, in.Enabled, in.Listen, in.Port, in.Protocol, string(in.Spec), in.LinkHost, in.LinkPort, now, now)
	if err != nil {
		return 0, mapConstraint(err)
	}
	return id, nil
}

// UpdateInbound 更新入站的可编辑字段，所属服务器不可变。
func (s *Store) UpdateInbound(in *Inbound) error {
	res, err := s.db.Exec(`UPDATE inbounds SET remark = ?, enabled = ?, listen = ?, port = ?, protocol = ?, spec_json = ?,
		link_host = ?, link_port = ?, updated_at = ? WHERE id = ?`,
		in.Remark, in.Enabled, in.Listen, in.Port, in.Protocol, string(in.Spec), in.LinkHost, in.LinkPort, time.Now().Unix(), in.ID)
	if err != nil {
		return mapConstraint(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteInbound 删除入站。
func (s *Store) DeleteInbound(id int64) error {
	return s.execOne("DELETE FROM inbounds WHERE id = ?", id)
}

func mapConstraint(err error) error {
	if strings.Contains(err.Error(), "UNIQUE constraint failed: inbounds.node_id, inbounds.port") {
		return ErrPortConflict
	}
	return err
}
