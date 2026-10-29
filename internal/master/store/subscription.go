package store

import (
	"database/sql"
	"errors"
	"time"
)

// 订阅状态，由流量核算维护；管理员停用使用 Enabled 字段。
const (
	SubActive   = "active"
	SubExceeded = "exceeded"
	SubExpired  = "expired"
)

// Subscription 为一个成员/使用者，持有订阅链接令牌与派生凭据的根密钥。
type Subscription struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Note        string `json:"note"`
	Sort        int    `json:"sort"`
	Token       string `json:"token"`
	Secret      string `json:"-"`
	UUID        string `json:"uuid"`
	Enabled     bool   `json:"enabled"`
	State       string `json:"state"`
	AllInbounds bool   `json:"allInbounds"`
	CreatedAt   int64  `json:"createdAt"`
}

// Active 表示订阅当前是否应下发到节点。
func (s *Subscription) Active() bool { return s.Enabled && s.State == SubActive }

const subColumns = `id, name, note, sort, token, secret, uuid, enabled, state, all_inbounds, created_at`

func scanSub(r rowScanner) (*Subscription, error) {
	var s Subscription
	err := r.Scan(&s.ID, &s.Name, &s.Note, &s.Sort, &s.Token, &s.Secret, &s.UUID, &s.Enabled, &s.State, &s.AllInbounds, &s.CreatedAt)
	return &s, err
}

// ListSubscriptions 返回全部订阅。
func (s *Store) ListSubscriptions() ([]*Subscription, error) {
	rows, err := s.db.Query("SELECT " + subColumns + " FROM subscriptions ORDER BY sort, id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Subscription
	for rows.Next() {
		sub, err := scanSub(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sub)
	}
	return out, rows.Err()
}

// GetSubscription 按 ID 查询订阅。
func (s *Store) GetSubscription(id int64) (*Subscription, error) {
	sub, err := scanSub(s.db.QueryRow("SELECT "+subColumns+" FROM subscriptions WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return sub, err
}

// CountSubscriptions 返回订阅数量。
func (s *Store) CountSubscriptions() (int, error) {
	var n int
	err := s.db.QueryRow("SELECT COUNT(*) FROM subscriptions").Scan(&n)
	return n, err
}

// CreateSubscription 新建订阅，返回新 ID。
func (s *Store) CreateSubscription(sub *Subscription) (int64, error) {
	res, err := s.db.Exec(`INSERT INTO subscriptions (name, note, token, secret, uuid, enabled, state, all_inbounds, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sub.Name, sub.Note, sub.Token, sub.Secret, sub.UUID, sub.Enabled, SubActive, sub.AllInbounds, time.Now().Unix())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateSubscription 更新名称、备注与启用状态。
func (s *Store) UpdateSubscription(id int64, name, note string, enabled bool) error {
	return s.execOne("UPDATE subscriptions SET name = ?, note = ?, enabled = ? WHERE id = ?", name, note, enabled, id)
}

// ResetSubscriptionCredentials 更换凭据根密钥与 UUID，旧凭据随之失效。
func (s *Store) ResetSubscriptionCredentials(id int64, secret, uuid string) error {
	return s.execOne("UPDATE subscriptions SET secret = ?, uuid = ? WHERE id = ?", secret, uuid, id)
}

// DeleteSubscription 删除订阅。
func (s *Store) DeleteSubscription(id int64) error {
	return s.execOne("DELETE FROM subscriptions WHERE id = ?", id)
}

// InboundSubscriptions 返回可使用该入站的有效订阅：已启用、状态正常，且覆盖全部入站或被单独授权。
func (s *Store) InboundSubscriptions(inboundID int64) ([]*Subscription, error) {
	rows, err := s.db.Query(`SELECT `+subColumns+` FROM subscriptions s
		WHERE s.enabled = 1 AND s.state = ? AND (s.all_inbounds = 1 OR EXISTS
			(SELECT 1 FROM subscription_inbounds si WHERE si.sub_id = s.id AND si.inbound_id = ?))
		ORDER BY s.id`, SubActive, inboundID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Subscription
	for rows.Next() {
		sub, err := scanSub(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sub)
	}
	return out, rows.Err()
}
