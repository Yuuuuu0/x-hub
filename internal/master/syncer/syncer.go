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

	mu      sync.Mutex
	kicks   map[int64]chan struct{}
	desired map[int64]string // 最近一次编译出的配置哈希
}

// New 创建同步器。
func New(st *store.Store, h *hub.Hub, log *slog.Logger) *Syncer {
	return &Syncer{store: st, hub: h, log: log, kicks: map[int64]chan struct{}{}, desired: map[int64]string{}}
}

// SetACME 设置 ACME 凭据来源。
func (s *Syncer) SetACME(fn func() (email, token string)) { s.acme = fn }

// Trigger 请求同步某台服务器，短时间内的多次触发会合并。
func (s *Syncer) Trigger(nodeID int64) {
	s.mu.Lock()
	ch := s.kicks[nodeID]
	if ch == nil {
		ch = make(chan struct{}, 1)
		s.kicks[nodeID] = ch
		go s.worker(nodeID, ch)
	}
	s.mu.Unlock()
	select {
	case ch <- struct{}{}:
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

// Forget 在服务器删除后清理其同步状态。
func (s *Syncer) Forget(nodeID int64) {
	s.mu.Lock()
	delete(s.desired, nodeID)
	s.mu.Unlock()
}

func (s *Syncer) worker(nodeID int64, ch chan struct{}) {
	for range ch {
		time.Sleep(debounce)
		select { // 合并防抖期间的新触发
		case <-ch:
		default:
		}
		if err := s.syncOnce(nodeID); err != nil && !errors.Is(err, store.ErrNotFound) {
			s.log.Warn("sync node", "node", nodeID, "err", err)
		}
	}
}

// syncOnce 编译期望配置，与已应用哈希不一致且服务器在线时下发。
func (s *Syncer) syncOnce(nodeID int64) error {
	res, err := compiler.Node(s.store, nodeID)
	if err != nil {
		s.store.SetNodeSyncError(nodeID, "配置编译失败："+err.Error())
		return err
	}
	hash := res.Hash
	s.mu.Lock()
	s.desired[nodeID] = hash
	s.mu.Unlock()

	n, err := s.store.GetNode(nodeID)
	if err != nil {
		return err
	}
	if n.AppliedHash == hash || !s.hub.State(nodeID).Online {
		return nil
	}
	rev, err := s.store.NextDesiredRev(nodeID)
	if err != nil {
		return err
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
		return err
	}
	s.log.Info("config applied", "node", nodeID, "rev", applied.Rev, "hash", applied.Hash)
	return s.store.SetNodeApplied(nodeID, applied.Rev, applied.Hash)
}
