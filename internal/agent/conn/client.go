// Package conn 实现被控端到主控的长连接：接入、握手、心跳与指令处理，断线后按退避重连。
package conn

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/Yuuuuu0/x-hub/internal/agent/certs"
	"github.com/Yuuuuu0/x-hub/internal/agent/config"
	"github.com/Yuuuuu0/x-hub/internal/agent/ledger"
	"github.com/Yuuuuu0/x-hub/internal/agent/metrics"
	"github.com/Yuuuuu0/x-hub/internal/agent/probe"
	"github.com/Yuuuuu0/x-hub/internal/agent/xray"
	"github.com/Yuuuuu0/x-hub/internal/shared/protocol"
	"github.com/Yuuuuu0/x-hub/internal/shared/tlspin"
	"github.com/Yuuuuu0/x-hub/internal/version"
)

const (
	heartbeatEvery  = 15 * time.Second
	trafficEvery    = 30 * time.Second
	maxBatches      = 200     // 单条上报消息的批次上限
	maxTrafficBytes = 4 << 20 // 单条上报消息的序列化大小上限，低于主控 8MB 读取上限并留足余量
	pingEvery       = 30 * time.Second
	dialTimeout     = 20 * time.Second
	writeTimeout    = 10 * time.Second
	maxBackoff      = time.Minute
	unauthBackoff   = 5 * time.Minute
	stableSession   = 30 * time.Second // 会话存活超过该时长才重置重连退避，避免握手后立即断开时热循环重连
)

var errUnauthorized = errors.New("主控拒绝了节点凭据，请在面板重新生成安装命令并重装")

// Client 维护与主控的连接。
type Client struct {
	cfg    *config.Config
	xray   *xray.Manager
	certs  *certs.Manager
	ledger *ledger.Ledger
	log    *slog.Logger
	host   protocol.HostInfo
	// applyMu 串行化配置应用。指令在独立 goroutine 中执行且不随断线取消，重连后主控可能重发，需保持逐个按序应用。
	applyMu sync.Mutex
}

// New 创建客户端。
func New(cfg *config.Config, xm *xray.Manager, cm *certs.Manager, l *ledger.Ledger, log *slog.Logger) *Client {
	return &Client{cfg: cfg, xray: xm, certs: cm, ledger: l, log: log, host: metrics.HostInfo()}
}

// Run 依次尝试候选地址连接主控，直到 ctx 结束。ctx 同时作为耗时指令（安装 Xray、申请证书等）的上下文，
// 使其不随单次连接断开而取消，只在进程退出时取消。
func (c *Client) Run(ctx context.Context) {
	backoff := time.Second
	for ctx.Err() == nil {
		var lastErr error
		connected := false
		var alive time.Duration
		for i, ep := range c.cfg.EndpointList() {
			start := time.Now()
			ok, err := c.session(ctx, ep, i)
			if ok {
				connected, alive = true, time.Since(start)
				break
			}
			lastErr = err
			c.log.Warn("connect master failed", "url", ep.URL, "err", err)
		}
		var wait time.Duration
		wait, backoff = nextBackoff(backoff, connected, alive, lastErr)
		wait += time.Duration(rand.Int64N(int64(wait/2) + 1))
		select {
		case <-ctx.Done():
		case <-time.After(wait):
		}
	}
}

// nextBackoff 根据本轮连接结果计算等待时长与新的退避值（抖动由调用方叠加）。
// 会话存活超过 stableSession 才重置退避；握手成功但很快断开与连接失败一样按指数退避；凭据被拒时固定长等待。
func nextBackoff(backoff time.Duration, connected bool, alive time.Duration, lastErr error) (wait, next time.Duration) {
	switch {
	case connected && alive >= stableSession:
		return time.Second, time.Second
	case !connected && errors.Is(lastErr, errUnauthorized):
		return unauthBackoff, backoff
	default:
		return backoff, min(backoff*2, maxBackoff)
	}
}

// session 建立一次连接并运行到断开。connected 表示是否曾成功握手。
func (c *Client) session(ctx context.Context, ep protocol.Endpoint, idx int) (connected bool, err error) {
	if c.host.Country == "" {
		// 每次连接前补测，启动时网络未就绪导致的失败可在重连时恢复。
		gctx, gcancel := context.WithTimeout(ctx, 6*time.Second)
		c.host.Country = metrics.DetectCountry(gctx)
		gcancel()
	}
	dctx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()
	ws, resp, err := websocket.Dial(dctx, wsURL(ep.URL), &websocket.DialOptions{
		HTTPClient: tlspin.HTTPClient(ep.FP, 0),
		HTTPHeader: http.Header{"Authorization": {c.bearer()}},
	})
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusUnauthorized {
			return false, errUnauthorized
		}
		return false, err
	}
	defer ws.CloseNow()
	ws.SetReadLimit(8 << 20)

	hello := protocol.Hello{AgentVersion: version.Version, Host: c.host, Xray: c.xray.Status(), Caps: c.xray.Caps()}
	hello.AppliedRev, hello.AppliedHash = c.xray.Applied()
	if err := c.write(ctx, ws, protocol.TypeHello, "", hello); err != nil {
		return false, err
	}
	welcome, err := readWelcome(dctx, ws)
	if err != nil {
		return false, err
	}
	c.log.Info("connected to master", "url", ep.URL)
	c.syncEndpoint(ep, idx, welcome.Endpoint)
	if err := c.ledger.Ack(welcome.AckSeq); err != nil {
		c.log.Warn("ack ledger", "err", err)
	}

	sctx, scancel := context.WithCancel(ctx)
	defer scancel()
	go c.heartbeatLoop(sctx, scancel, ws)
	err = c.readLoop(sctx, ctx, ws)
	c.log.Warn("disconnected from master", "err", err)
	return true, nil
}

// syncEndpoint 根据主控推荐地址调整本地候选列表。
func (c *Client) syncEndpoint(used protocol.Endpoint, idx int, recommended protocol.Endpoint) {
	var err error
	switch {
	case recommended.URL == "":
	case recommended == used && idx == 0:
		err = c.cfg.KeepOnly(used)
	default:
		err = c.cfg.Promote(recommended)
	}
	if err != nil {
		c.log.Warn("save endpoint", "err", err)
	}
}

func readWelcome(ctx context.Context, ws *websocket.Conn) (protocol.Welcome, error) {
	var w protocol.Welcome
	_, data, err := ws.Read(ctx)
	if err != nil {
		return w, err
	}
	var env protocol.Envelope
	if err := json.Unmarshal(data, &env); err != nil || env.T != protocol.TypeWelcome {
		return w, errors.New("unexpected handshake reply")
	}
	return w, json.Unmarshal(env.D, &w)
}

func (c *Client) heartbeatLoop(ctx context.Context, cancel context.CancelFunc, ws *websocket.Conn) {
	hb := time.NewTicker(heartbeatEvery)
	ping := time.NewTicker(pingEvery)
	traffic := time.NewTicker(trafficEvery)
	defer hb.Stop()
	defer ping.Stop()
	defer traffic.Stop()
	send := func() bool {
		s := metrics.Sample()
		s.Xray, s.Caps = c.xray.Status(), c.xray.Caps()
		s.Online = c.xray.OnlineIPs()
		return c.write(ctx, ws, protocol.TypeHeartbeat, "", s) == nil
	}
	// sendTraffic 上报待确认批次中不超过消息大小上限的前一段，未确认的会在下个周期重发，主控按序号去重。
	sendTraffic := func() bool {
		batches, err := c.ledger.Seal()
		if err != nil {
			c.log.Warn("seal ledger", "err", err)
			return true
		}
		if len(batches) == 0 {
			return true
		}
		batches = firstChunk(batches, maxTrafficBytes, maxBatches)
		return c.write(ctx, ws, protocol.TypeTraffic, "", protocol.Traffic{Batches: batches}) == nil
	}
	// 连接建立后立即补报断线期间积压的流量，不等第一个上报周期。
	if !send() || !sendTraffic() {
		cancel()
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-hb.C:
			if !send() {
				cancel()
				return
			}
		case <-traffic.C:
			if !sendTraffic() {
				cancel()
				return
			}
		case <-c.xray.Changes():
			if !send() {
				cancel()
				return
			}
		case <-ping.C:
			pctx, pcancel := context.WithTimeout(ctx, writeTimeout)
			err := ws.Ping(pctx)
			pcancel()
			if err != nil {
				cancel()
				return
			}
		}
	}
}

// firstChunk 返回 batches 中序列化后总大小不超过 maxBytes、条数不超过 maxCount 的最长前缀；
// 首个批次单独超限时仍单独返回它（账本封存时已按大小拆分，正常不会出现），保证每个周期都有进展。
func firstChunk(batches []protocol.TrafficBatch, maxBytes, maxCount int) []protocol.TrafficBatch {
	const envelopeOverhead = 256 // 消息外层 {"t":"traffic","d":{"batches":[...]}} 的保守估算
	size := envelopeOverhead
	for i, b := range batches {
		raw, err := json.Marshal(b)
		if err != nil {
			return batches[:max(i, 1)]
		}
		size += len(raw) + 1 // 逗号分隔
		if i > 0 && (size > maxBytes || i >= maxCount) {
			return batches[:i]
		}
	}
	return batches
}

// readLoop 读取主控消息直到连接断开。sctx 为本次会话上下文，用于回复；root 为进程级上下文，
// 耗时指令在独立 goroutine 中以 root 执行，避免阻塞读循环导致心跳超时，也不因连接断开而中止，回复失败可接受。
func (c *Client) readLoop(sctx, root context.Context, ws *websocket.Conn) error {
	for {
		_, data, err := ws.Read(sctx)
		if err != nil {
			return err
		}
		var env protocol.Envelope
		if err := json.Unmarshal(data, &env); err != nil {
			continue
		}
		switch env.T {
		case protocol.TypeAck:
			var a protocol.Ack
			if json.Unmarshal(env.D, &a) == nil {
				if err := c.ledger.Ack(a.Seq); err != nil {
					c.log.Warn("ack ledger", "err", err)
				}
			}
		case protocol.TypeEndpoint:
			var ep protocol.Endpoint
			if json.Unmarshal(env.D, &ep) == nil {
				if err := c.cfg.Promote(ep); err != nil {
					c.log.Warn("save endpoint", "err", err)
				}
			}
		case protocol.TypeXray:
			go func() { c.reply(sctx, ws, env.ID, c.handleXray(root, env.D)) }()
		case protocol.TypeApply:
			go func() { c.reply(sctx, ws, env.ID, c.handleApply(root, env.D)) }()
		case protocol.TypeProbe:
			go func() { c.reply(sctx, ws, env.ID, c.handleProbe(sctx, env.D)) }()
		case protocol.TypeLogs:
			var req protocol.LogsRequest
			json.Unmarshal(env.D, &req)
			c.reply(sctx, ws, env.ID, okResult(protocol.LogsResponse{Lines: c.xray.Logs(req.Lines)}))
		default:
			if env.ID != "" {
				c.reply(sctx, ws, env.ID, protocol.Result{Error: "被控端不支持该指令：" + env.T})
			}
		}
	}
}

// handleXray 执行 Xray 控制指令。
func (c *Client) handleXray(ctx context.Context, raw json.RawMessage) protocol.Result {
	var cmd protocol.XrayCommand
	if err := json.Unmarshal(raw, &cmd); err != nil {
		return protocol.Result{Error: err.Error()}
	}
	var err error
	switch cmd.Action {
	case protocol.XrayStart:
		err = c.xray.Start()
	case protocol.XrayStop:
		err = c.xray.Stop()
	case protocol.XrayRestart:
		err = c.xray.Restart()
	case protocol.XrayInstall:
		ictx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
		err = c.xray.Install(ictx, cmd.Version, c.fetchXray)
	default:
		err = fmt.Errorf("未知操作 %s", cmd.Action)
	}
	if err != nil {
		return protocol.Result{Error: err.Error()}
	}
	return okResult(c.xray.Status())
}

// handleApply 应用主控下发的 Xray 配置。
func (c *Client) handleApply(ctx context.Context, raw json.RawMessage) protocol.Result {
	var a protocol.Apply
	if err := json.Unmarshal(raw, &a); err != nil {
		return protocol.Result{Error: err.Error()}
	}
	c.applyMu.Lock()
	defer c.applyMu.Unlock()
	cfg := a.Config
	var certErr error
	var dropped []string
	if a.ACME != nil {
		cctx, cancel := context.WithTimeout(ctx, 4*time.Minute)
		certErr = c.certs.Ensure(cctx, *a.ACME)
		cancel()
		// 证书申请失败时只跳过缺证书的入站，其余配置照常生效；不记录为已应用，主控会显示失败并在下次同步时重试。
		if certErr != nil {
			var err error
			if cfg, dropped, err = certs.DropMissing(cfg, c.cfg.DataDir); err != nil {
				return protocol.Result{Error: err.Error()}
			}
		}
	}
	if err := c.xray.Apply(a.Rev, a.Hash, cfg, certErr == nil); err != nil {
		return protocol.Result{Error: err.Error()}
	}
	if certErr != nil {
		return protocol.Result{Error: fmt.Sprintf("%v（%s 暂未启用，其余入站已生效）", certErr, strings.Join(dropped, ", "))}
	}
	rev, hash := c.xray.Applied()
	return okResult(protocol.ApplyResult{Rev: rev, Hash: hash})
}

// handleProbe 执行本机探测。
func (c *Client) handleProbe(ctx context.Context, raw json.RawMessage) protocol.Result {
	var req protocol.ProbeRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return protocol.Result{Error: err.Error()}
	}
	switch req.Kind {
	case protocol.ProbeRealityTarget:
		return okResult(probe.RealityTarget(ctx, req.Target, req.ServerName))
	case protocol.ProbePort:
		return okResult(c.xray.PortStatus(req.Port))
	case protocol.ProbeCertFiles:
		return okResult(certs.CheckFiles(req.CertFile, req.KeyFile))
	}
	return protocol.Result{Error: "未知探测类型：" + req.Kind}
}

// fetchXray 从主控中转下载 Xray 安装包，依次尝试候选地址。
func (c *Client) fetchXray(ctx context.Context, tag string) (io.ReadCloser, string, error) {
	var lastErr error
	for _, ep := range c.cfg.EndpointList() {
		url := fmt.Sprintf("%s/agent/dl/xray/%s/%s", ep.URL, tag, runtime.GOARCH)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		req.Header.Set("Authorization", c.bearer())
		resp, err := tlspin.HTTPClient(ep.FP, 0).Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
			resp.Body.Close()
			lastErr = fmt.Errorf("下载失败：%s %s", resp.Status, extractError(body))
			continue
		}
		return resp.Body, resp.Header.Get("X-Content-Sha256"), nil
	}
	return nil, "", lastErr
}

func (c *Client) reply(ctx context.Context, ws *websocket.Conn, id string, res protocol.Result) {
	if err := c.write(ctx, ws, protocol.TypeResult, id, res); err != nil {
		c.log.Warn("send result", "err", err)
	}
}

func (c *Client) write(ctx context.Context, ws *websocket.Conn, typ, re string, payload any) error {
	msg, err := protocol.Marshal(typ, "", re, payload)
	if err != nil {
		return err
	}
	wctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	return ws.Write(wctx, websocket.MessageText, msg)
}

func (c *Client) bearer() string {
	return fmt.Sprintf("Bearer %d.%s", c.cfg.NodeID, c.cfg.Secret)
}

func okResult(data any) protocol.Result {
	b, _ := json.Marshal(data)
	return protocol.Result{OK: true, Data: b}
}

func wsURL(base string) string {
	base = strings.TrimSuffix(base, "/")
	switch {
	case strings.HasPrefix(base, "https://"):
		base = "wss://" + base[len("https://"):]
	case strings.HasPrefix(base, "http://"):
		base = "ws://" + base[len("http://"):]
	}
	return base + "/agent/ws"
}

func extractError(body []byte) string {
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &e) == nil && e.Error != "" {
		return e.Error
	}
	return string(bytes.TrimSpace(body))
}

// Enroll 用一次性令牌向主控换取节点凭据并写入配置文件。
func Enroll(master, token, fp, cfgPath, dataDir string) error {
	master = strings.TrimSuffix(master, "/")
	hostname, _ := os.Hostname()
	body, _ := json.Marshal(protocol.EnrollRequest{Token: token, Hostname: hostname})
	resp, err := tlspin.HTTPClient(fp, 30*time.Second).Post(master+"/agent/enroll", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("接入失败：%s %s", resp.Status, extractError(raw))
	}
	var er protocol.EnrollResponse
	if err := json.Unmarshal(raw, &er); err != nil {
		return err
	}
	cfg := config.New(cfgPath)
	cfg.NodeID, cfg.Secret, cfg.DataDir = er.NodeID, er.Secret, dataDir
	cfg.Endpoints = []protocol.Endpoint{{URL: master, FP: fp}}
	return cfg.Save()
}
