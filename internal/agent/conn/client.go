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
	"time"

	"github.com/coder/websocket"

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
	heartbeatEvery = 15 * time.Second
	trafficEvery   = 30 * time.Second
	maxBatches     = 200 // 单条上报消息的批次上限
	pingEvery      = 30 * time.Second
	dialTimeout    = 20 * time.Second
	writeTimeout   = 10 * time.Second
	maxBackoff     = time.Minute
	unauthBackoff  = 5 * time.Minute
)

var errUnauthorized = errors.New("主控拒绝了节点凭据，请在面板重新生成安装命令并重装")

// Client 维护与主控的连接。
type Client struct {
	cfg    *config.Config
	xray   *xray.Manager
	ledger *ledger.Ledger
	log    *slog.Logger
	host   protocol.HostInfo
}

// New 创建客户端。
func New(cfg *config.Config, xm *xray.Manager, l *ledger.Ledger, log *slog.Logger) *Client {
	return &Client{cfg: cfg, xray: xm, ledger: l, log: log, host: metrics.HostInfo()}
}

// Run 依次尝试候选地址连接主控，直到 ctx 结束。
func (c *Client) Run(ctx context.Context) {
	backoff := time.Second
	for ctx.Err() == nil {
		var lastErr error
		connected := false
		for i, ep := range c.cfg.EndpointList() {
			ok, err := c.session(ctx, ep, i)
			if ok {
				connected = true
				break
			}
			lastErr = err
			c.log.Warn("connect master failed", "url", ep.URL, "err", err)
		}
		wait := backoff
		switch {
		case connected:
			backoff, wait = time.Second, time.Second
		case errors.Is(lastErr, errUnauthorized):
			wait = unauthBackoff
		default:
			backoff = min(backoff*2, maxBackoff)
		}
		wait += time.Duration(rand.Int64N(int64(wait/2) + 1))
		select {
		case <-ctx.Done():
		case <-time.After(wait):
		}
	}
}

// session 建立一次连接并运行到断开。connected 表示是否曾成功握手。
func (c *Client) session(ctx context.Context, ep protocol.Endpoint, idx int) (connected bool, err error) {
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
	err = c.readLoop(sctx, ws)
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
	// sendTraffic 上报全部待确认批次，未确认的会在下个周期重发，主控按序号去重。
	sendTraffic := func() bool {
		batches, err := c.ledger.Seal()
		if err != nil {
			c.log.Warn("seal ledger", "err", err)
			return true
		}
		if len(batches) == 0 {
			return true
		}
		if len(batches) > maxBatches {
			batches = batches[:maxBatches]
		}
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

func (c *Client) readLoop(ctx context.Context, ws *websocket.Conn) error {
	for {
		_, data, err := ws.Read(ctx)
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
			go c.reply(ctx, ws, env.ID, c.handleXray(ctx, env.D))
		case protocol.TypeApply:
			go c.reply(ctx, ws, env.ID, c.handleApply(env.D))
		case protocol.TypeProbe:
			go c.reply(ctx, ws, env.ID, c.handleProbe(ctx, env.D))
		case protocol.TypeLogs:
			var req protocol.LogsRequest
			json.Unmarshal(env.D, &req)
			c.reply(ctx, ws, env.ID, okResult(protocol.LogsResponse{Lines: c.xray.Logs(req.Lines)}))
		default:
			if env.ID != "" {
				c.reply(ctx, ws, env.ID, protocol.Result{Error: "被控端不支持该指令：" + env.T})
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
func (c *Client) handleApply(raw json.RawMessage) protocol.Result {
	var a protocol.Apply
	if err := json.Unmarshal(raw, &a); err != nil {
		return protocol.Result{Error: err.Error()}
	}
	if err := c.xray.Apply(a.Rev, a.Hash, a.Config); err != nil {
		return protocol.Result{Error: err.Error()}
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
