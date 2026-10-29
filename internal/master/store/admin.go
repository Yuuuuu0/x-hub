package store

import (
	"database/sql"
	"errors"
	"time"
)

// Admin 为面板管理员。
type Admin struct {
	ID           int64
	Username     string
	PasswordHash string
}

// Session 为管理员登录会话。
type Session struct {
	ID       int64
	AdminID  int64
	Username string
	ExpireAt int64
}

// CountAdmins 返回管理员数量，用于判断是否首次启动。
func (s *Store) CountAdmins() (int, error) {
	var n int
	err := s.db.QueryRow("SELECT COUNT(*) FROM admins").Scan(&n)
	return n, err
}

// FirstAdmin 返回唯一的管理员（v1 只支持单管理员）。
func (s *Store) FirstAdmin() (*Admin, error) {
	return s.scanAdmin(s.db.QueryRow("SELECT id, username, password_hash FROM admins ORDER BY id LIMIT 1"))
}

// AdminByUsername 按用户名查找管理员。
func (s *Store) AdminByUsername(username string) (*Admin, error) {
	return s.scanAdmin(s.db.QueryRow("SELECT id, username, password_hash FROM admins WHERE username = ?", username))
}

// AdminByID 按 ID 查找管理员。
func (s *Store) AdminByID(id int64) (*Admin, error) {
	return s.scanAdmin(s.db.QueryRow("SELECT id, username, password_hash FROM admins WHERE id = ?", id))
}

func (s *Store) scanAdmin(row *sql.Row) (*Admin, error) {
	var a Admin
	if err := row.Scan(&a.ID, &a.Username, &a.PasswordHash); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &a, nil
}

// CreateAdmin 创建管理员。
func (s *Store) CreateAdmin(username, passwordHash string) error {
	_, err := s.db.Exec("INSERT INTO admins (username, password_hash, created_at) VALUES (?, ?, ?)",
		username, passwordHash, time.Now().Unix())
	return err
}

// UpdateAdminCredentials 更新用户名与密码哈希，并清空该管理员全部会话（keepSessionID 除外）。
func (s *Store) UpdateAdminCredentials(id int64, username, passwordHash string, keepSessionID int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec("UPDATE admins SET username = ?, password_hash = ? WHERE id = ?", username, passwordHash, id); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM sessions WHERE admin_id = ? AND id != ?", id, keepSessionID); err != nil {
		return err
	}
	return tx.Commit()
}

// CreateSession 创建登录会话。
func (s *Store) CreateSession(adminID int64, tokenHash, ip, ua string, ttl time.Duration) error {
	now := time.Now()
	_, err := s.db.Exec("INSERT INTO sessions (admin_id, token_hash, ip, ua, created_at, expire_at) VALUES (?, ?, ?, ?, ?, ?)",
		adminID, tokenHash, ip, ua, now.Unix(), now.Add(ttl).Unix())
	return err
}

// SessionByTokenHash 查找未过期的会话。
func (s *Store) SessionByTokenHash(tokenHash string) (*Session, error) {
	var ss Session
	err := s.db.QueryRow(`SELECT s.id, s.admin_id, a.username, s.expire_at FROM sessions s
		JOIN admins a ON a.id = s.admin_id WHERE s.token_hash = ? AND s.expire_at > ?`,
		tokenHash, time.Now().Unix()).Scan(&ss.ID, &ss.AdminID, &ss.Username, &ss.ExpireAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &ss, err
}

// DeleteSession 删除会话（登出）。
func (s *Store) DeleteSession(id int64) error {
	_, err := s.db.Exec("DELETE FROM sessions WHERE id = ?", id)
	return err
}

// DeleteAllSessions 删除全部会话，用于命令行重置。
func (s *Store) DeleteAllSessions() error {
	_, err := s.db.Exec("DELETE FROM sessions")
	return err
}

// DeleteExpiredSessions 清理过期会话。
func (s *Store) DeleteExpiredSessions() error {
	_, err := s.db.Exec("DELETE FROM sessions WHERE expire_at <= ?", time.Now().Unix())
	return err
}
