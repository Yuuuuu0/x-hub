package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// ErrNameConflict 表示名称已被占用。
var ErrNameConflict = errors.New("名称已存在")

// Profile 为一个分流方案，Spec 为 routing.Profile 的 JSON。
type Profile struct {
	ID        int64           `json:"id"`
	Name      string          `json:"name"`
	Note      string          `json:"note"`
	Spec      json.RawMessage `json:"spec"`
	CreatedAt int64           `json:"createdAt"`
	UpdatedAt int64           `json:"updatedAt"`
}

const profileColumns = "id, name, note, spec_json, created_at, updated_at"

func scanProfile(r rowScanner) (*Profile, error) {
	var p Profile
	var spec string
	if err := r.Scan(&p.ID, &p.Name, &p.Note, &spec, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return nil, err
	}
	p.Spec = json.RawMessage(spec)
	return &p, nil
}

// ListProfiles 返回全部分流方案。
func (s *Store) ListProfiles() ([]*Profile, error) {
	rows, err := s.db.Query("SELECT " + profileColumns + " FROM profiles ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Profile{}
	for rows.Next() {
		p, err := scanProfile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetProfile 按 ID 查询分流方案。
func (s *Store) GetProfile(id int64) (*Profile, error) {
	p, err := scanProfile(s.db.QueryRow("SELECT "+profileColumns+" FROM profiles WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return p, err
}

// CreateProfile 新建分流方案。
func (s *Store) CreateProfile(name, note string, spec []byte) (int64, error) {
	now := time.Now().Unix()
	return s.insertWithID("profiles", "INSERT INTO profiles (id, name, note, spec_json, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
		name, note, string(spec), now, now)
}

// UpdateProfile 更新分流方案。
func (s *Store) UpdateProfile(id int64, name, note string, spec []byte) error {
	return s.execOne("UPDATE profiles SET name = ?, note = ?, spec_json = ?, updated_at = ? WHERE id = ?",
		name, note, string(spec), time.Now().Unix(), id)
}

// DeleteProfile 删除分流方案，返回仍在使用它的订阅名称（非空时不删除）。
func (s *Store) DeleteProfile(id int64) ([]string, error) {
	users, err := s.ProfileUsers(id)
	if err != nil || len(users) > 0 {
		return users, err
	}
	return nil, s.execOne("DELETE FROM profiles WHERE id = ?", id)
}

// ProfileUsers 返回使用该方案的订阅名称。
func (s *Store) ProfileUsers(id int64) ([]string, error) {
	rows, err := s.db.Query("SELECT name FROM subscriptions WHERE profile_id = ? ORDER BY id", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// SetSubscriptionProfile 设置订阅使用的分流方案，0 为内置方案。
func (s *Store) SetSubscriptionProfile(subID, profileID int64) error {
	return s.execOne("UPDATE subscriptions SET profile_id = ? WHERE id = ?", profileID, subID)
}

// RuleSet 为规则集库中的一项。Source 为 hosted（托管 Content）或 url（外部链接）。
type RuleSet struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Behavior  string `json:"behavior"`
	Source    string `json:"source"`
	URL       string `json:"url"`
	Format    string `json:"format"`
	Interval  int    `json:"interval"`
	Content   string `json:"content"`
	Note      string `json:"note"`
	UpdatedAt int64  `json:"updatedAt"`
}

const ruleSetColumns = "id, name, behavior, source, url, format, interval, content, note, updated_at"

func scanRuleSet(r rowScanner) (*RuleSet, error) {
	var rs RuleSet
	err := r.Scan(&rs.ID, &rs.Name, &rs.Behavior, &rs.Source, &rs.URL, &rs.Format, &rs.Interval, &rs.Content, &rs.Note, &rs.UpdatedAt)
	return &rs, err
}

// ListRuleSets 返回规则集库。
func (s *Store) ListRuleSets() ([]*RuleSet, error) {
	rows, err := s.db.Query("SELECT " + ruleSetColumns + " FROM rule_sets ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*RuleSet{}
	for rows.Next() {
		rs, err := scanRuleSet(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rs)
	}
	return out, rows.Err()
}

// GetRuleSet 按 ID 查询规则集。
func (s *Store) GetRuleSet(id int64) (*RuleSet, error) {
	rs, err := scanRuleSet(s.db.QueryRow("SELECT "+ruleSetColumns+" FROM rule_sets WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return rs, err
}

// RuleSetByName 按名称查询规则集。
func (s *Store) RuleSetByName(name string) (*RuleSet, error) {
	rs, err := scanRuleSet(s.db.QueryRow("SELECT "+ruleSetColumns+" FROM rule_sets WHERE name = ?", name))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return rs, err
}

// CreateRuleSet 新建规则集。
func (s *Store) CreateRuleSet(rs *RuleSet) (int64, error) {
	id, err := s.insertWithID("rule_sets", `INSERT INTO rule_sets (id, name, behavior, source, url, format, interval, content, note, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rs.Name, rs.Behavior, rs.Source, rs.URL, rs.Format, rs.Interval, rs.Content, rs.Note, time.Now().Unix())
	return id, mapNameConflict(err)
}

// UpdateRuleSet 更新规则集。名称变化时由调用方同步更新引用它的方案（见 RenameRuleSetRefs）。
func (s *Store) UpdateRuleSet(rs *RuleSet) error {
	err := s.execOne(`UPDATE rule_sets SET name = ?, behavior = ?, source = ?, url = ?, format = ?, interval = ?, content = ?, note = ?,
		updated_at = ? WHERE id = ?`,
		rs.Name, rs.Behavior, rs.Source, rs.URL, rs.Format, rs.Interval, rs.Content, rs.Note, time.Now().Unix(), rs.ID)
	return mapNameConflict(err)
}

// DeleteRuleSet 删除规则集。
func (s *Store) DeleteRuleSet(id int64) error {
	return s.execOne("DELETE FROM rule_sets WHERE id = ?", id)
}

func mapNameConflict(err error) error {
	if err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed") {
		return ErrNameConflict
	}
	return err
}
