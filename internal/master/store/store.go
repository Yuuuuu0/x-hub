// Package store 封装主控的 SQLite 持久化访问。
package store

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

// ErrNotFound 表示目标记录不存在。
var ErrNotFound = errors.New("not found")

// Store 是主控数据库的访问入口。
type Store struct {
	db *sql.DB
}

// Open 打开（必要时创建）SQLite 数据库并执行迁移。
func Open(path string) (*Store, error) {
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// 个人规模下单连接足够，且能避免 SQLite 写锁竞争。
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

// nextID 在事务内分配不复用的新 ID：取高水位与当前最大 ID 中较大者加一，并推进高水位。
func nextID(tx *sql.Tx, table string) (int64, error) {
	var last, cur int64
	if err := tx.QueryRow("SELECT last FROM id_seq WHERE name = ?", table).Scan(&last); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	if err := tx.QueryRow("SELECT COALESCE(MAX(id), 0) FROM " + table).Scan(&cur); err != nil {
		return 0, err
	}
	id := max(last, cur) + 1
	_, err := tx.Exec("INSERT INTO id_seq (name, last) VALUES (?, ?) ON CONFLICT (name) DO UPDATE SET last = excluded.last", table, id)
	return id, err
}

// insertWithID 在事务内分配新 ID 并执行插入，query 的第一个参数位为 id。
func (s *Store) insertWithID(table, query string, args ...any) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	id, err := nextID(tx, table)
	if err != nil {
		return 0, err
	}
	if _, err := tx.Exec(query, append([]any{id}, args...)...); err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

// reorder 按 ids 的先后写入 table 的 sort 列。
func (s *Store) reorder(table string, ids []int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i, id := range ids {
		if _, err := tx.Exec("UPDATE "+table+" SET sort = ? WHERE id = ?", i, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Close 关闭数据库。
func (s *Store) Close() error { return s.db.Close() }

// migrate 按文件名序号执行尚未应用的迁移，版本记录在 PRAGMA user_version。
func (s *Store) migrate() error {
	var current int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&current); err != nil {
		return err
	}
	entries, err := fs.ReadDir(migrations, "migrations")
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, e := range entries {
		n, err := strconv.Atoi(strings.SplitN(e.Name(), "_", 2)[0])
		if err != nil {
			return fmt.Errorf("bad migration name %s", e.Name())
		}
		if n <= current {
			continue
		}
		body, err := migrations.ReadFile("migrations/" + e.Name())
		if err != nil {
			return err
		}
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("%s: %w", e.Name(), err)
		}
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", n)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}
