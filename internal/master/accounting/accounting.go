// Package accounting 负责流量核算：入库被控端上报的批次，并按流量上限、到期时间与每月重置日
// 维护订阅状态，状态变化时触发配置同步，使超额或到期的订阅从各节点移除。
package accounting

import (
	"context"
	"log/slog"
	"time"

	"github.com/Yuuuuu0/x-hub/internal/master/model"
	"github.com/Yuuuuu0/x-hub/internal/master/settings"
	"github.com/Yuuuuu0/x-hub/internal/master/store"
	"github.com/Yuuuuu0/x-hub/internal/shared/protocol"
)

// Syncer 为配置同步触发接口。
type Syncer interface{ TriggerAll() }

// Service 为流量核算服务。
type Service struct {
	store    *store.Store
	settings *settings.Service
	syncer   Syncer
	log      *slog.Logger
}

// New 创建核算服务。
func New(st *store.Store, s *settings.Service, syncer Syncer, log *slog.Logger) *Service {
	return &Service{store: st, settings: s, syncer: syncer, log: log}
}

// Ingest 入库流量批次并返回应确认的序号；入库失败返回 -1，被控端会在下个周期重发。
func (a *Service) Ingest(nodeID int64, t protocol.Traffic) int64 {
	batches := make([]store.TrafficBatch, 0, len(t.Batches))
	for _, b := range t.Batches {
		sb := store.TrafficBatch{Seq: b.Seq, Hour: b.Hour, NICRx: b.NICRx, NICTx: b.NICTx}
		for email, v := range b.Users {
			if sub, in, ok := model.ParseClientEmail(email); ok && (v[0] > 0 || v[1] > 0) {
				sb.Rows = append(sb.Rows, store.TrafficRow{SubID: sub, InboundID: in, Up: v[0], Down: v[1]})
			}
		}
		batches = append(batches, sb)
	}
	last, touched, err := a.store.IngestTraffic(nodeID, batches)
	if err != nil {
		a.log.Error("ingest traffic", "node", nodeID, "err", err)
		return -1
	}
	if len(touched) > 0 {
		a.Evaluate(touched...)
	}
	return last
}

// LastSeq 返回服务器已确认的批次序号，用于握手时告知被控端。
func (a *Service) LastSeq(nodeID int64) int64 {
	n, err := a.store.GetNode(nodeID)
	if err != nil {
		return 0
	}
	return n.LastSeq
}

// Evaluate 重新计算指定订阅（为空时全部订阅）的周期与状态，有变化时触发同步。
func (a *Service) Evaluate(ids ...int64) {
	var subs []*store.Subscription
	if len(ids) == 0 {
		all, err := a.store.ListSubscriptions()
		if err != nil {
			a.log.Warn("list subscriptions", "err", err)
			return
		}
		subs = all
	} else {
		for _, id := range ids {
			if s, err := a.store.GetSubscription(id); err == nil {
				subs = append(subs, s)
			}
		}
	}
	now, loc := time.Now(), a.settings.Location()
	changed := false
	for _, s := range subs {
		if s.ResetDay > 0 {
			if start := PeriodStart(s.ResetDay, now, loc); s.PeriodStart < start.Unix() {
				if err := a.store.ResetSubscriptionUsage(s.ID, start.Unix()); err != nil {
					a.log.Warn("reset usage", "sub", s.ID, "err", err)
					continue
				}
				s.UsedUp, s.UsedDown, s.PeriodStart = 0, 0, start.Unix()
				a.log.Info("subscription period reset", "sub", s.ID)
			}
		}
		if st := State(s, now); st != s.State {
			if err := a.store.SetSubscriptionState(s.ID, st); err != nil {
				a.log.Warn("set state", "sub", s.ID, "err", err)
				continue
			}
			a.log.Info("subscription state changed", "sub", s.ID, "from", s.State, "to", st)
			changed = true
		}
	}
	if changed {
		a.syncer.TriggerAll()
	}
}

// Run 每分钟检查到期与周期重置，每天把超过保留期的小时明细汇总为天，直到 ctx 结束。
func (a *Service) Run(ctx context.Context) {
	a.Evaluate()
	a.rollup()
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	lastRollup := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			a.Evaluate()
			if time.Since(lastRollup) >= 24*time.Hour {
				a.rollup()
				lastRollup = time.Now()
			}
		}
	}
}

func (a *Service) rollup() {
	st, loc := a.settings.Stats(), a.settings.Location()
	cutoff := time.Now().In(loc).AddDate(0, 0, -st.RetentionDays)
	before := time.Date(cutoff.Year(), cutoff.Month(), cutoff.Day(), 0, 0, 0, 0, loc).Unix()
	if n, err := a.store.RollupTraffic(before, loc); err != nil {
		a.log.Warn("rollup traffic", "err", err)
	} else if n > 0 {
		a.log.Info("traffic rolled up", "rows", n)
	}
}

// State 计算订阅应处的状态：到期优先于超额。
func State(s *store.Subscription, now time.Time) string {
	switch {
	case s.ExpireAt > 0 && now.Unix() >= s.ExpireAt:
		return store.SubExpired
	case s.LimitBytes > 0 && s.UsedUp+s.UsedDown >= s.LimitBytes:
		return store.SubExceeded
	default:
		return store.SubActive
	}
}

// PeriodStart 返回不晚于 now 的最近一个重置点（day 日 00:00，loc 时区）。
func PeriodStart(day int, now time.Time, loc *time.Location) time.Time {
	t := now.In(loc)
	start := time.Date(t.Year(), t.Month(), day, 0, 0, 0, 0, loc)
	if start.After(t) {
		start = start.AddDate(0, -1, 0)
	}
	return start
}
