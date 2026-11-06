// Package syncer 负责把主控期望的 Xray 配置收敛到各被控端：变更时防抖触发、离线时等待重连后补发。
package syncer

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/Yuuuuu0/x-hub/internal/master/compiler"
	"github.com/Yuuuuu0/x-hub/internal/master/hub"
	"github.com/Yuuuuu0/x-hub/internal/master/store"
	"github.com/Yuuuuu0/x-hub/internal/shared/protocol"
)

const (
	debounce = 300 * time.Millisecond
	// applyTimeout 需覆盖首次 ACME 申请证书（DNS 验证传播）的耗时。
	applyTimeout = 5 * time.Minute
)

// 在线服务器下发失败后的默认重试退避：从 defaultRetryBase 起指数增长，上限 defaultRetryMax。
const (
	defaultRetryBase = 30 * time.Second
	defaultRetryMax  = 5 * time.Minute
)

// 同步状态。
const (
	StateSynced  = "synced"
	StatePending = "pending"
	StateFailed  = "failed"
)

// Syncer 为每台服务器维护一个串行的同步工作协程。
type Syncer struct {
	store *store.Store
	hub   *hub.Hub
	log   *slog.Logger

	acme func() (email, token string) // 读取 ACME 账户邮箱与 Cloudflare Token

	retryBase, retryMax time.Duration // 下发失败重试退避的起始值与上限

	mu      sync.Mutex
	workers map[int64]*worker
	desired map[int64]string // 最近一次编译出的配置哈希
}

// worker 为单台服务器的同步协程句柄：kick 投递同步请求，done 关闭时协程退出。
type worker struct {
	kick chan struct{}
	done chan struct{}
}

// New 创建同步器。
func New(st *store.Store, h *hub.Hub, log *slog.Logger) *Syncer {
	return &Syncer{store: st, hub: h, log: log, workers: map[int64]*worker{}, desired: map[int64]string{},
		retryBase: defaultRetryBase, retryMax: defaultRetryMax}
}

// SetACME 设置 ACME 凭据来源。
func (s *Syncer) SetACME(fn func() (email, token string)) { s.acme = fn }

// Trigger 请求同步某台服务器，短时间内的多次触发会合并。
func (s *Syncer) Trigger(nodeID int64) {
	s.mu.Lock()
	w := s.workers[nodeID]
	if w == nil {
		w = &worker{kick: make(chan struct{}, 1), done: make(chan struct{})}
		s.workers[nodeID] = w
		go s.run(nodeID, w)
	}
	s.mu.Unlock()
	select {
	case w.kick <- struct{}{}:
	default:
	}
}

// TriggerAll 请求同步全部服务器，用于订阅变更等影响所有节点的操作。
func (s *Syncer) TriggerAll() {
	nodes, err := s.store.ListNodes()
	if err != nil {
		s.log.Warn("list nodes for sync", "err", err)
		return
	}
	for _, n := range nodes {
		s.Trigger(n.ID)
	}
}

// OnHello 记录被控端上报的已应用版本并触发一次同步校验。
func (s *Syncer) OnHello(nodeID int64, h protocol.Hello) {
	if err := s.store.SetNodeApplied(nodeID, h.AppliedRev, h.AppliedHash); err != nil {
		s.log.Warn("record applied", "node", nodeID, "err", err)
	}
	s.Trigger(nodeID)
}

// State 返回服务器的同步状态。
func (s *Syncer) State(n *store.Node) string {
	s.mu.Lock()
	want, ok := s.desired[n.ID]
	s.mu.Unlock()
	switch {
	case ok && want == n.AppliedHash:
		return StateSynced
	case n.SyncError != "":
		return StateFailed
	case !ok && n.AppliedHash != "":
		// 尚未编译过（如主控刚启动），以已应用状态为准，避免误报
		return StateSynced
	default:
		return StatePending
	}
}

// Forget 在服务器删除后清理其同步状态，并让其同步协程退出。
func (s *Syncer) Forget(nodeID int64) {
	s.mu.Lock()
	delete(s.desired, nodeID)
	if w := s.workers[nodeID]; w != nil {
		delete(s.workers, nodeID)
		close(w.done)
	}
	s.mu.Unlock()
}

// run 串行处理某台服务器的同步请求：防抖合并触发；在线下发失败时按指数退避定时重试，
// 下发成功或期望配置变化时重置退避；收到 Forget 后退出。
func (s *Syncer) run(nodeID int64, w *worker) {
	var (
		timer      *time.Timer
		retryC     <-chan time.Time
		backoff    time.Duration
		failedHash string // 上次下发失败时的期望配置哈希
	)
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	for {
		select {
		case <-w.done:
			return
		case <-w.kick:
		case <-retryC:
		}
		if timer != nil { // 本轮即会同步，取消尚未到期的重试
			timer.Stop()
			timer, retryC = nil, nil
		}
		select {
		case <-w.done:
			return
		case <-time.After(debounce):
		}
		select { // 合并防抖期间的新触发
		case <-w.kick:
		default:
		}
		hash, retry, err := s.syncOnce(nodeID)
		if errors.Is(err, store.ErrNotFound) { // 服务器已删除（如删除后迟到的触发），清理后退出
			s.retire(nodeID, w)
			return
		}
		if err != nil {
			s.log.Warn("sync node", "node", nodeID, "err", err)
		}
		if !retry {
			backoff, failedHash = 0, ""
			continue
		}
		backoff = s.nextBackoff(backoff, hash != failedHash)
		failedHash = hash
		timer = time.NewTimer(backoff)
		retryC = timer.C
	}
}

// retire 在同步协程自行退出时移除其登记（仅当登记的仍是该协程）。
func (s *Syncer) retire(nodeID int64, w *worker) {
	s.mu.Lock()
	if s.workers[nodeID] == w {
		delete(s.workers, nodeID)
	}
	s.mu.Unlock()
}

// nextBackoff 计算下一次重试间隔：首次失败或期望配置已变化时从 retryBase 开始，否则翻倍，不超过 retryMax。
func (s *Syncer) nextBackoff(prev time.Duration, changed bool) time.Duration {
	if changed || prev <= 0 {
		return s.retryBase
	}
	return min(prev*2, s.retryMax)
}

// syncOnce 编译期望配置，与已应用哈希不一致且服务器在线时下发。
// 返回本次的期望配置哈希，以及是否因在线下发失败而需要重试。
func (s *Syncer) syncOnce(nodeID int64) (string, bool, error) {
	res, err := compiler.Node(s.store, nodeID)
	if err != nil {
		s.store.SetNodeSyncError(nodeID, "配置编译失败："+err.Error())
		return "", false, err
	}
	hash := res.Hash
	s.mu.Lock()
	s.desired[nodeID] = hash
	s.mu.Unlock()

	n, err := s.store.GetNode(nodeID)
	if err != nil {
		return hash, false, err
	}
	if n.AppliedHash == hash || !s.hub.State(nodeID).Online {
		return hash, false, nil
	}
	rev, err := s.store.NextDesiredRev(nodeID)
	if err != nil {
		return hash, false, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), applyTimeout)
	defer cancel()
	apply := protocol.Apply{Rev: rev, Hash: hash, Config: res.Config}
	if len(res.ACMEDomains) > 0 && s.acme != nil {
		email, token := s.acme()
		apply.ACME = &protocol.ACME{Email: email, CloudflareToken: token, Domains: res.ACMEDomains}
	}
	var applied protocol.ApplyResult
	if err := s.hub.Request(ctx, nodeID, protocol.TypeApply, apply, &applied); err != nil {
		s.store.SetNodeSyncError(nodeID, err.Error())
		// 离线时等待重连握手（OnHello）再触发，无需定时重试。
		return hash, !errors.Is(err, hub.ErrOffline), err
	}
	s.log.Info("config applied", "node", nodeID, "rev", applied.Rev, "hash", applied.Hash)
	return hash, false, s.store.SetNodeApplied(nodeID, applied.Rev, applied.Hash)
}
