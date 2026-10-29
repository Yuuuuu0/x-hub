package store

import (
	"database/sql"
	"encoding/json"
	"errors"
)

// GetSetting 读取 key 对应的 JSON 设置到 out，不存在时返回 false。
func (s *Store) GetSetting(key string, out any) (bool, error) {
	var raw string
	err := s.db.QueryRow("SELECT value_json FROM settings WHERE key = ?", key).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, json.Unmarshal([]byte(raw), out)
}

// PutSetting 以 JSON 形式写入设置。
func (s *Store) PutSetting(key string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = s.db.Exec("INSERT INTO settings (key, value_json) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value_json = excluded.value_json", key, string(raw))
	return err
}
