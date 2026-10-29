// Package hub 管理全部被控端的 WebSocket 长连接：握手、心跳状态、指令下发与应答关联。
package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/Yuuuuu0/x-hub/internal/master/auth"
	"github.com/Yuuuuu0/x-hub/internal/master/store"
	"github.com/Yuuuuu0/x-hub/internal/shared/protocol"
)

const (
	helloTimeout = 15 * time.Second
	readTimeout  = 45 * time.Second // 3 个心跳周期无消息即判定离线
	writeTimeout = 10 * time.Second
	touchEvery   = time.Minute
)

// ErrOffline 表示目标服务器当前不在线。
var ErrOffline = errors.New("服务器不在线")

// RuntimeState 为在线服务器的内存态信息，不落库。
type RuntimeState struct {
	Online      bool                `json:"online"`
	ConnectedAt int64               `json:"connectedAt"`
	IP          string              `json:"ip"`
	Heartbeat   *protocol.Heartbeat `json:"heartbeat"`
	RxRate      float64             `json:"rxRate"` // 字节/秒
	TxRate      float64             `json:"txRate"`
	Xray        protocol.XrayStatus `json:"xray"`
	Caps        []string            `json:"caps"`
}

// Hub 是所有被控连接的注册中心。
type Hub struct {
	store    *store.Store
	log      *slog.Logger
	endpoint func() protocol.Endpoint
	onHello  func(nodeID int64, h protocol.Hello)

	mu    sync.RWMutex
	conns map[int64]*conn
}

// New 创建 Hub。endpoint 返回当前推荐给被控端的主控地址；onHello 在被控端完成握手后异步调用。
func New(st *store.Store, log *slog.Logger, endpoint func() protocol.Endpoint, onHello func(int64, protocol.Hello)) *Hub {
	return &Hub{store: st, log: log, endpoint: endpoint, onHello: onHello, conns: map[int64]*conn{}}
}

type conn struct {
	hub    *Hub
	nodeID int64
	ws     *websocket.Conn
	ctx    context.Context
	cancel context.CancelFunc
	send   chan []byte

	pmu     sync.Mutex
	pending map[string]chan protocol.Envelope

	smu       sync.RWMutex
	state     RuntimeState
	lastHBAt  time.Time
	lastTouch time.Time
	xrayVer   string
	caps      []string
}

// Serve 接管一个已通过认证的被控端 WebSocket 连接，直到连接断开。
func (h *Hub) Serve(w http.ResponseWriter, r *http.Request, nodeID int64, ip string) {
	ws, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	ws.SetReadLimit(8 << 20)
	ctx, cancel := context.WithCancel(context.Background())
	c := &conn{
		hub: h, nodeID: nodeID, ws: ws, ctx: ctx, cancel: cancel,
		send:    make(chan []byte, 64),
		pending: map[string]chan protocol.Envelope{},
		state:   RuntimeState{Online: true, ConnectedAt: time.Now().Unix(), IP: ip},
	}
	defer c.close()

	if err := c.handshake(); err != nil {
		h.log.Warn("agent handshake failed", "node", nodeID, "err", err)
		ws.Close(websocket.StatusPolicyViolation, "handshake failed")
		return
	}
	h.register(c)
	defer h.unregister(c)
	go c.writeLoop()
	c.readLoop()
}

func (c *conn) handshake() error {
	ctx, cancel := context.WithTimeout(c.ctx, helloTimeout)
	defer cancel()
	_, data, err := c.ws.Read(ctx)
	if err != nil {
		return err
	}
	var env protocol.Envelope
	if err := json.Unmarshal(data, &env); err != nil || env.T != protocol.TypeHello {
		return fmt.Errorf("expect hello")
	}
	var hello protocol.Hello
	if err := json.Unmarshal(env.D, &hello); err != nil {
		return err
	}
	if err := c.hub.store.UpdateNodeRuntime(c.nodeID, hello.AgentVersion, hello.Xray.Version, hello.Caps, hello.Host, c.state.IP); err != nil {
		return err
	}
	c.state.Xray, c.state.Caps = hello.Xray, hello.Caps
	c.xrayVer, c.caps = hello.Xray.Version, hello.Caps
	c.lastTouch = time.Now()
	msg, _ := protocol.Marshal(protocol.TypeWelcome, "", "", protocol.Welcome{ServerTime: time.Now().Unix(), Endpoint: c.hub.endpoint()})
	wctx, wcancel := context.WithTimeout(c.ctx, writeTimeout)
	defer wcancel()
	if err := c.ws.Write(wctx, websocket.MessageText, msg); err != nil {
		return err
	}
	c.hub.log.Info("agent connected", "node", c.nodeID, "ip", c.state.IP, "agent", hello.AgentVersion, "xray", hello.Xray.Version)
	if c.hub.onHello != nil {
		go c.hub.onHello(c.nodeID, hello)
	}
	return nil
}

func (c *conn) readLoop() {
	for {
		ctx, cancel := context.WithTimeout(c.ctx, readTimeout)
		_, data, err := c.ws.Read(ctx)
		cancel()
		if err != nil {
			return
		}
		var env protocol.Envelope
		if err := json.Unmarshal(data, &env); err != nil {
			continue
		}
		switch env.T {
		case protocol.TypeHeartbeat:
			var hb protocol.Heartbeat
			if json.Unmarshal(env.D, &hb) == nil {
				c.onHeartbeat(&hb)
			}
		case protocol.TypeResult:
			c.pmu.Lock()
			ch := c.pending[env.Re]
			delete(c.pending, env.Re)
			c.pmu.Unlock()
			if ch != nil {
				ch <- env
			}
		}
	}
}

func (c *conn) onHeartbeat(hb *protocol.Heartbeat) {
	now := time.Now()
	c.smu.Lock()
	if prev := c.state.Heartbeat; prev != nil {
		dt := now.Sub(c.lastHBAt).Seconds()
		if dt > 0 && hb.NetRx >= prev.NetRx && hb.NetTx >= prev.NetTx {
			c.state.RxRate = float64(hb.NetRx-prev.NetRx) / dt
			c.state.TxRate = float64(hb.NetTx-prev.NetTx) / dt
		}
	}
	c.state.Heartbeat, c.lastHBAt = hb, now
	c.state.Xray, c.state.Caps = hb.Xray, hb.Caps
	changed := hb.Xray.Version != c.xrayVer || !slices.Equal(hb.Caps, c.caps)
	touch := now.Sub(c.lastTouch) >= touchEvery
	if changed {
		c.xrayVer, c.caps = hb.Xray.Version, hb.Caps
	}
	if changed || touch {
		c.lastTouch = now
	}
	c.smu.Unlock()

	var err error
	if changed {
		err = c.hub.store.UpdateNodeXray(c.nodeID, hb.Xray.Version, hb.Caps)
	} else if touch {
		err = c.hub.store.TouchNode(c.nodeID)
	}
	if err != nil {
		c.hub.log.Warn("persist heartbeat", "node", c.nodeID, "err", err)
	}
}

func (c *conn) writeLoop() {
	for {
		select {
		case <-c.ctx.Done():
			return
		case msg := <-c.send:
			ctx, cancel := context.WithTimeout(c.ctx, writeTimeout)
			err := c.ws.Write(ctx, websocket.MessageText, msg)
			cancel()
			if err != nil {
				c.cancel()
				return
			}
		}
	}
}

// enqueue 非阻塞投递消息，发送队列满说明对端异常，直接断开。
func (c *conn) enqueue(msg []byte) bool {
	select {
	case c.send <- msg:
		return true
	default:
		c.cancel()
		return false
	}
}

func (c *conn) close() {
	c.cancel()
	c.ws.CloseNow()
	c.pmu.Lock()
	for id, ch := range c.pending {
		close(ch)
		delete(c.pending, id)
	}
	c.pmu.Unlock()
}

func (h *Hub) register(c *conn) {
	h.mu.Lock()
	old := h.conns[c.nodeID]
	h.conns[c.nodeID] = c
	h.mu.Unlock()
	if old != nil {
		old.ws.Close(websocket.StatusPolicyViolation, "replaced by new connection")
		old.cancel()
	}
}

func (h *Hub) unregister(c *conn) {
	h.mu.Lock()
	if h.conns[c.nodeID] == c {
		delete(h.conns, c.nodeID)
	}
	h.mu.Unlock()
	h.store.TouchNode(c.nodeID)
	h.log.Info("agent disconnected", "node", c.nodeID)
}

func (h *Hub) get(nodeID int64) *conn {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.conns[nodeID]
}

// State 返回服务器的内存态，离线时 Online 为 false。
func (h *Hub) State(nodeID int64) RuntimeState {
	c := h.get(nodeID)
	if c == nil {
		return RuntimeState{}
	}
	c.smu.RLock()
	defer c.smu.RUnlock()
	return c.state
}

// Request 向服务器发送指令并等待应答，out 非空时解码应答数据。
func (h *Hub) Request(ctx context.Context, nodeID int64, typ string, payload, out any) error {
	c := h.get(nodeID)
	if c == nil {
		return ErrOffline
	}
	id := auth.RandomString(16)
	msg, err := protocol.Marshal(typ, id, "", payload)
	if err != nil {
		return err
	}
	ch := make(chan protocol.Envelope, 1)
	c.pmu.Lock()
	c.pending[id] = ch
	c.pmu.Unlock()
	defer func() {
		c.pmu.Lock()
		delete(c.pending, id)
		c.pmu.Unlock()
	}()
	if !c.enqueue(msg) {
		return ErrOffline
	}
	select {
	case env, ok := <-ch:
		if !ok {
			return ErrOffline
		}
		var res protocol.Result
		if err := json.Unmarshal(env.D, &res); err != nil {
			return err
		}
		if !res.OK {
			return errors.New(res.Error)
		}
		if out != nil && len(res.Data) > 0 {
			return json.Unmarshal(res.Data, out)
		}
		return nil
	case <-ctx.Done():
		return fmt.Errorf("等待服务器响应超时")
	case <-c.ctx.Done():
		return ErrOffline
	}
}

// Broadcast 向所有在线服务器推送一条无需应答的消息。
func (h *Hub) Broadcast(typ string, payload any) {
	msg, err := protocol.Marshal(typ, "", "", payload)
	if err != nil {
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, c := range h.conns {
		c.enqueue(msg)
	}
}

// Kick 断开服务器连接，用于删除服务器后。
func (h *Hub) Kick(nodeID int64) {
	if c := h.get(nodeID); c != nil {
		c.ws.Close(websocket.StatusPolicyViolation, "node removed")
		c.cancel()
	}
}
