package conn

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/Yuuuuu0/x-hub/internal/agent/ledger"
	"github.com/Yuuuuu0/x-hub/internal/agent/metrics"
	"github.com/Yuuuuu0/x-hub/internal/agent/xray"
)

const collectEvery = 10 * time.Second

// Collector 周期性读取 Xray 用户流量与网卡流量增量，写入本地账本。
type Collector struct {
	ledger *ledger.Ledger
	xray   *xray.Manager
	log    *slog.Logger
	mu     sync.Mutex
}

// NewCollector 创建采集器，并注册为 Xray 停止前的回调，避免重启丢失尚未读取的计数。
func NewCollector(l *ledger.Ledger, xm *xray.Manager, log *slog.Logger) *Collector {
	c := &Collector{ledger: l, xray: xm, log: log}
	xm.SetBeforeStop(c.Collect)
	return c
}

// Run 每 10 秒采集一次，直到 ctx 结束。
func (c *Collector) Run(ctx context.Context) {
	t := time.NewTicker(collectEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.Collect()
		}
	}
}

// Collect 执行一次采集。用户流量读取后即清零，因此必须先落账本再返回。
func (c *Collector) Collect() {
	c.mu.Lock()
	defer c.mu.Unlock()
	users, err := c.xray.QueryUserTraffic()
	if err != nil {
		c.log.Warn("query xray traffic", "err", err)
	}
	rx, tx := c.ledger.NICDelta(metrics.NetCounters())
	if len(users) == 0 && rx == 0 && tx == 0 {
		return
	}
	hour := time.Now().Truncate(time.Hour).Unix()
	if err := c.ledger.Add(hour, users, rx, tx); err != nil {
		c.log.Error("write ledger", "err", err)
	}
}
