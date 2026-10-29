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
	LimitBytes  int64  `json:"limitBytes"`
	ExpireAt    int64  `json:"expireAt"`
	UsedUp      int64  `json:"usedUp"`
	UsedDown    int64  `json:"usedDown"`
	ResetDay    int    `json:"resetDay"`      // 每月流量重置日（1-28），0 表示不自动重置
	PeriodStart int64  `json:"periodStartAt"` // 当前计量周期起点
	CreatedAt   int64  `json:"createdAt"`
}

// Active 表示订阅当前是否应下发到节点。
func (s *Subscription) Active() bool { return s.Enabled && s.State == SubActive }

const subColumns = `id, name, note, sort, token, secret, uuid, enabled, state, all_inbounds, limit_bytes, expire_at,
	used_up, used_down, reset_day, period_start_at, created_at`

func scanSub(r rowScanner) (*Subscription, error) {
	var s Subscription
	err := r.Scan(&s.ID, &s.Name, &s.Note, &s.Sort, &s.Token, &s.Secret, &s.UUID, &s.Enabled, &s.State, &s.AllInbounds,
		&s.LimitBytes, &s.ExpireAt, &s.UsedUp, &s.UsedDown, &s.ResetDay, &s.PeriodStart, &s.CreatedAt)
	return &s, err
}

// SubscriptionByToken 按订阅链接令牌查询。
func (s *Store) SubscriptionByToken(token string) (*Subscription, error) {
	sub, err := scanSub(s.db.QueryRow("SELECT "+subColumns+" FROM subscriptions WHERE token = ?", token))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return sub, err
}

// ResetSubscriptionToken 更换订阅链接令牌，旧链接随之失效，凭据不变。
func (s *Store) ResetSubscriptionToken(id int64, token string) error {
	return s.execOne("UPDATE subscriptions SET token = ? WHERE id = ?", token, id)
}

// SubscriptionInboundIDs 返回各订阅被单独授权的入站 ID。
func (s *Store) SubscriptionInboundIDs() (map[int64][]int64, error) {
	rows, err := s.db.Query("SELECT sub_id, inbound_id FROM subscription_inbounds ORDER BY sub_id, inbound_id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][]int64{}
	for rows.Next() {
		var sid, iid int64
		if err := rows.Scan(&sid, &iid); err != nil {
			return nil, err
		}
		out[sid] = append(out[sid], iid)
	}
	return out, rows.Err()
}

// SetSubscriptionScope 设置订阅的入站范围：覆盖全部入站，或仅限 inboundIDs。
func (s *Store) SetSubscriptionScope(id int64, all bool, inboundIDs []int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec("UPDATE subscriptions SET all_inbounds = ? WHERE id = ?", all, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if _, err := tx.Exec("DELETE FROM subscription_inbounds WHERE sub_id = ?", id); err != nil {
		return err
	}
	if !all {
		for _, iid := range inboundIDs {
			if _, err := tx.Exec("INSERT OR IGNORE INTO subscription_inbounds (sub_id, inbound_id) SELECT ?, id FROM inbounds WHERE id = ?", id, iid); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
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
	now := time.Now().Unix()
	return s.insertWithID("subscriptions", `INSERT INTO subscriptions (id, name, note, token, secret, uuid, enabled, state, all_inbounds, period_start_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sub.Name, sub.Note, sub.Token, sub.Secret, sub.UUID, sub.Enabled, SubActive, sub.AllInbounds, now, now)
}

// UpdateSubscription 更新名称、备注与启用状态。
func (s *Store) UpdateSubscription(id int64, name, note string, enabled bool) error {
	return s.execOne("UPDATE subscriptions SET name = ?, note = ?, enabled = ? WHERE id = ?", name, note, enabled, id)
}

// UpdateSubscriptionQuota 更新流量上限、到期时间与每月重置日。重置日变化时以当前时刻开始新周期但保留已用量。
func (s *Store) UpdateSubscriptionQuota(id, limitBytes, expireAt int64, resetDay int) error {
	return s.execOne(`UPDATE subscriptions SET limit_bytes = ?, expire_at = ?,
		period_start_at = CASE WHEN reset_day != ? THEN ? ELSE period_start_at END, reset_day = ? WHERE id = ?`,
		limitBytes, expireAt, resetDay, time.Now().Unix(), resetDay, id)
}

// ResetSubscriptionUsage 清零当前周期用量，并以 start 作为新周期起点。
func (s *Store) ResetSubscriptionUsage(id, start int64) error {
	return s.execOne("UPDATE subscriptions SET used_up = 0, used_down = 0, period_start_at = ? WHERE id = ?", start, id)
}

// SetSubscriptionState 更新订阅状态。
func (s *Store) SetSubscriptionState(id int64, state string) error {
	return s.execOne("UPDATE subscriptions SET state = ? WHERE id = ?", state, id)
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
