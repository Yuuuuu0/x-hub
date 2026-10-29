package store

import (
	"sort"
	"time"
)

// TrafficRow 为某订阅在某入站上的一段流量增量。
type TrafficRow struct {
	SubID     int64
	InboundID int64
	Up        int64
	Down      int64
}

// TrafficBatch 为被控端上报的一个批次。
type TrafficBatch struct {
	Seq   int64
	Hour  int64
	Rows  []TrafficRow
	NICRx int64
	NICTx int64
}

// IngestTraffic 在单个事务内入库流量批次：跳过序号不大于已确认值的批次（重传去重），
// 累加小时明细、服务器网卡流量与订阅当前周期用量，并推进已确认序号。
// 返回入库后的已确认序号与用量发生变化的订阅。
func (s *Store) IngestTraffic(nodeID int64, batches []TrafficBatch) (lastSeq int64, touched []int64, err error) {
	sort.Slice(batches, func(i, j int) bool { return batches[i].Seq < batches[j].Seq })
	tx, err := s.db.Begin()
	if err != nil {
		return 0, nil, err
	}
	defer tx.Rollback()
	if err := tx.QueryRow("SELECT last_seq FROM nodes WHERE id = ?", nodeID).Scan(&lastSeq); err != nil {
		return 0, nil, err
	}
	seen := map[int64]bool{}
	for _, b := range batches {
		if b.Seq <= lastSeq {
			continue
		}
		for _, r := range b.Rows {
			if _, err := tx.Exec(`INSERT INTO traffic_hourly (hour, sub_id, inbound_id, node_id, up, down) VALUES (?, ?, ?, ?, ?, ?)
				ON CONFLICT (hour, sub_id, inbound_id) DO UPDATE SET up = up + excluded.up, down = down + excluded.down`,
				b.Hour, r.SubID, r.InboundID, nodeID, r.Up, r.Down); err != nil {
				return 0, nil, err
			}
			// 只累计到与当前周期有重叠的小时，避免周期重置后迟到的旧流量计入新周期。
			res, err := tx.Exec("UPDATE subscriptions SET used_up = used_up + ?, used_down = used_down + ? WHERE id = ? AND period_start_at < ?",
				r.Up, r.Down, r.SubID, b.Hour+3600)
			if err != nil {
				return 0, nil, err
			}
			if n, _ := res.RowsAffected(); n > 0 && !seen[r.SubID] {
				seen[r.SubID] = true
				touched = append(touched, r.SubID)
			}
		}
		if b.NICRx > 0 || b.NICTx > 0 {
			if _, err := tx.Exec(`INSERT INTO node_traffic_hourly (hour, node_id, rx, tx) VALUES (?, ?, ?, ?)
				ON CONFLICT (hour, node_id) DO UPDATE SET rx = rx + excluded.rx, tx = tx + excluded.tx`,
				b.Hour, nodeID, b.NICRx, b.NICTx); err != nil {
				return 0, nil, err
			}
		}
		lastSeq = b.Seq
	}
	if _, err := tx.Exec("UPDATE nodes SET last_seq = ? WHERE id = ?", lastSeq, nodeID); err != nil {
		return 0, nil, err
	}
	return lastSeq, touched, tx.Commit()
}

// TrafficPoint 为某时间点（小时或天的起点）的流量。
type TrafficPoint struct {
	At   int64
	Up   int64
	Down int64
}

// SubTrafficSeries 返回订阅自 since 起的流量序列，包含小时明细与已汇总的天数据。
func (s *Store) SubTrafficSeries(subID, since int64) ([]TrafficPoint, error) {
	rows, err := s.db.Query(`SELECT hour, SUM(up), SUM(down) FROM traffic_hourly WHERE sub_id = ? AND hour >= ? GROUP BY hour
		UNION ALL SELECT day, SUM(up), SUM(down) FROM traffic_daily WHERE sub_id = ? AND day >= ? GROUP BY day`,
		subID, since, subID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TrafficPoint
	for rows.Next() {
		var p TrafficPoint
		if err := rows.Scan(&p.At, &p.Up, &p.Down); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// InboundUsage 为订阅在某入站上的累计流量。
type InboundUsage struct {
	NodeID    int64 `json:"nodeId"`
	InboundID int64 `json:"inboundId"`
	Up        int64 `json:"up"`
	Down      int64 `json:"down"`
}

// SubTrafficByInbound 返回订阅自 since 起在各入站上的流量。
func (s *Store) SubTrafficByInbound(subID, since int64) ([]InboundUsage, error) {
	rows, err := s.db.Query(`SELECT node_id, inbound_id, SUM(up), SUM(down) FROM (
			SELECT node_id, inbound_id, up, down FROM traffic_hourly WHERE sub_id = ? AND hour >= ?
			UNION ALL SELECT node_id, inbound_id, up, down FROM traffic_daily WHERE sub_id = ? AND day >= ?
		) GROUP BY node_id, inbound_id ORDER BY SUM(up) + SUM(down) DESC`, subID, since, subID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []InboundUsage
	for rows.Next() {
		var u InboundUsage
		if err := rows.Scan(&u.NodeID, &u.InboundID, &u.Up, &u.Down); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// NodeTrafficSince 返回服务器自 since 起的网卡收发总量。
func (s *Store) NodeTrafficSince(nodeID, since int64) (rx, tx int64, err error) {
	err = s.db.QueryRow(`SELECT COALESCE(SUM(rx), 0), COALESCE(SUM(tx), 0) FROM (
			SELECT rx, tx FROM node_traffic_hourly WHERE node_id = ? AND hour >= ?
			UNION ALL SELECT rx, tx FROM node_traffic_daily WHERE node_id = ? AND day >= ?)`,
		nodeID, since, nodeID, since).Scan(&rx, &tx)
	return rx, tx, err
}

// RollupTraffic 把 before 之前的小时明细按 loc 时区汇总为天数据并删除明细，控制数据量。
func (s *Store) RollupTraffic(before int64, loc *time.Location) (int, error) {
	dayOf := func(h int64) int64 {
		t := time.Unix(h, 0).In(loc)
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc).Unix()
	}
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	type subKey struct{ day, sub, inbound, node int64 }
	subAgg := map[subKey][2]int64{}
	rows, err := tx.Query("SELECT hour, sub_id, inbound_id, node_id, up, down FROM traffic_hourly WHERE hour < ?", before)
	if err != nil {
		return 0, err
	}
	n := 0
	for rows.Next() {
		var h, sub, in, node, up, down int64
		if err := rows.Scan(&h, &sub, &in, &node, &up, &down); err != nil {
			rows.Close()
			return 0, err
		}
		k := subKey{dayOf(h), sub, in, node}
		v := subAgg[k]
		subAgg[k] = [2]int64{v[0] + up, v[1] + down}
		n++
	}
	rows.Close()

	type nodeKey struct{ day, node int64 }
	nodeAgg := map[nodeKey][2]int64{}
	rows, err = tx.Query("SELECT hour, node_id, rx, tx FROM node_traffic_hourly WHERE hour < ?", before)
	if err != nil {
		return 0, err
	}
	for rows.Next() {
		var h, node, rx, txb int64
		if err := rows.Scan(&h, &node, &rx, &txb); err != nil {
			rows.Close()
			return 0, err
		}
		k := nodeKey{dayOf(h), node}
		v := nodeAgg[k]
		nodeAgg[k] = [2]int64{v[0] + rx, v[1] + txb}
		n++
	}
	rows.Close()

	for k, v := range subAgg {
		if _, err := tx.Exec(`INSERT INTO traffic_daily (day, sub_id, inbound_id, node_id, up, down) VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT (day, sub_id, inbound_id) DO UPDATE SET up = up + excluded.up, down = down + excluded.down`,
			k.day, k.sub, k.inbound, k.node, v[0], v[1]); err != nil {
			return 0, err
		}
	}
	for k, v := range nodeAgg {
		if _, err := tx.Exec(`INSERT INTO node_traffic_daily (day, node_id, rx, tx) VALUES (?, ?, ?, ?)
			ON CONFLICT (day, node_id) DO UPDATE SET rx = rx + excluded.rx, tx = tx + excluded.tx`,
			k.day, k.node, v[0], v[1]); err != nil {
			return 0, err
		}
	}
	if _, err := tx.Exec("DELETE FROM traffic_hourly WHERE hour < ?", before); err != nil {
		return 0, err
	}
	if _, err := tx.Exec("DELETE FROM node_traffic_hourly WHERE hour < ?", before); err != nil {
		return 0, err
	}
	return n, tx.Commit()
}
