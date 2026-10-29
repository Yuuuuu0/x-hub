package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// 外部节点来源类型。
const (
	ExternalManual = "manual" // 手动粘贴的链接或节点定义
	ExternalRemote = "remote" // 外部订阅
)

// ExternalSource 为一个外部节点来源。Nodes 为 external.Proxy 列表的 JSON。
type ExternalSource struct {
	ID         int64           `json:"id"`
	Kind       string          `json:"kind"`
	Name       string          `json:"name"`
	Enabled    bool            `json:"enabled"`
	Content    string          `json:"content"`
	UserAgent  string          `json:"userAgent"`
	Interval   int             `json:"interval"`
	Prefix     string          `json:"prefix"`
	Include    string          `json:"include"`
	Exclude    string          `json:"exclude"`
	Chain      string          `json:"chain"`
	AllSubs    bool            `json:"allSubs"`
	SubIDs     []int64         `json:"subIds"`
	Nodes      json.RawMessage `json:"-"`
	FetchedAt  int64           `json:"fetchedAt"`
	FetchError string          `json:"fetchError"`
	UserInfo   string          `json:"userInfo"`
	CreatedAt  int64           `json:"createdAt"`
	UpdatedAt  int64           `json:"updatedAt"`
}

// VisibleTo 判断来源是否对订阅可见。
func (e *ExternalSource) VisibleTo(subID int64) bool {
	if e.AllSubs {
		return true
	}
	for _, id := range e.SubIDs {
		if id == subID {
			return true
		}
	}
	return false
}

const externalColumns = `id, kind, name, enabled, content, user_agent, interval, prefix, include, exclude, chain,
	all_subs, sub_ids, nodes_json, fetched_at, fetch_error, user_info, created_at, updated_at`

func scanExternal(r rowScanner) (*ExternalSource, error) {
	var e ExternalSource
	var subIDs, nodes string
	if err := r.Scan(&e.ID, &e.Kind, &e.Name, &e.Enabled, &e.Content, &e.UserAgent, &e.Interval, &e.Prefix, &e.Include,
		&e.Exclude, &e.Chain, &e.AllSubs, &subIDs, &nodes, &e.FetchedAt, &e.FetchError, &e.UserInfo, &e.CreatedAt, &e.UpdatedAt); err != nil {
		return nil, err
	}
	if json.Unmarshal([]byte(subIDs), &e.SubIDs) != nil || e.SubIDs == nil {
		e.SubIDs = []int64{}
	}
	e.Nodes = json.RawMessage(nodes)
	return &e, nil
}

// ListExternalSources 返回全部外部节点来源。
func (s *Store) ListExternalSources() ([]*ExternalSource, error) {
	rows, err := s.db.Query("SELECT " + externalColumns + " FROM external_sources ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*ExternalSource{}
	for rows.Next() {
		e, err := scanExternal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// GetExternalSource 按 ID 查询外部节点来源。
func (s *Store) GetExternalSource(id int64) (*ExternalSource, error) {
	e, err := scanExternal(s.db.QueryRow("SELECT "+externalColumns+" FROM external_sources WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return e, err
}

// CreateExternalSource 新建外部节点来源（含解析或拉取结果）。
func (s *Store) CreateExternalSource(e *ExternalSource) (int64, error) {
	now := time.Now().Unix()
	subIDs, _ := json.Marshal(e.SubIDs)
	return s.insertWithID("external_sources", `INSERT INTO external_sources (id, kind, name, enabled, content, user_agent, interval,
		prefix, include, exclude, chain, all_subs, sub_ids, nodes_json, fetched_at, fetch_error, user_info, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.Kind, e.Name, e.Enabled, e.Content, e.UserAgent, e.Interval, e.Prefix, e.Include, e.Exclude, e.Chain, e.AllSubs,
		string(subIDs), string(e.Nodes), e.FetchedAt, e.FetchError, e.UserInfo, now, now)
}

// UpdateExternalSource 更新来源的全部字段（含节点与拉取状态）。
func (s *Store) UpdateExternalSource(e *ExternalSource) error {
	subIDs, _ := json.Marshal(e.SubIDs)
	return s.execOne(`UPDATE external_sources SET name = ?, enabled = ?, content = ?, user_agent = ?, interval = ?, prefix = ?,
		include = ?, exclude = ?, chain = ?, all_subs = ?, sub_ids = ?, nodes_json = ?, fetched_at = ?, fetch_error = ?, user_info = ?,
		updated_at = ? WHERE id = ?`,
		e.Name, e.Enabled, e.Content, e.UserAgent, e.Interval, e.Prefix, e.Include, e.Exclude, e.Chain, e.AllSubs, string(subIDs),
		string(e.Nodes), e.FetchedAt, e.FetchError, e.UserInfo, time.Now().Unix(), e.ID)
}

// SetExternalFetch 记录一次订阅拉取结果；nodes 为空表示失败，保留上次的节点。
func (s *Store) SetExternalFetch(id int64, nodes []byte, userInfo, fetchErr string) error {
	now := time.Now().Unix()
	if nodes == nil {
		return s.execOne("UPDATE external_sources SET fetched_at = ?, fetch_error = ? WHERE id = ?", now, fetchErr, id)
	}
	return s.execOne("UPDATE external_sources SET nodes_json = ?, user_info = ?, fetched_at = ?, fetch_error = '' WHERE id = ?",
		string(nodes), userInfo, now, id)
}

// DeleteExternalSource 删除外部节点来源。
func (s *Store) DeleteExternalSource(id int64) error {
	return s.execOne("DELETE FROM external_sources WHERE id = ?", id)
}
