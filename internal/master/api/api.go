// Package api 实现主控的 HTTP 入口：面板 REST API、前端静态资源与被控端接入接口。
package api

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/Yuuuuu0/x-hub/internal/master/auth"
	"github.com/Yuuuuu0/x-hub/internal/master/hub"
	"github.com/Yuuuuu0/x-hub/internal/master/server"
	"github.com/Yuuuuu0/x-hub/internal/master/settings"
	"github.com/Yuuuuu0/x-hub/internal/master/store"
	"github.com/Yuuuuu0/x-hub/internal/master/syncer"
	"github.com/Yuuuuu0/x-hub/internal/master/xrayrel"
	"github.com/Yuuuuu0/x-hub/internal/shared/protocol"
)

// Handler 是主控全部 HTTP 请求的入口。
type Handler struct {
	Store         *store.Store
	Settings      *settings.Service
	Certs         *server.Certs
	Server        *server.Server
	Hub           *hub.Hub
	Syncer        *syncer.Syncer
	Releases      *xrayrel.Service
	Log           *slog.Logger
	Web           fs.FS  // 前端构建产物根目录
	AgentDir      string // 各架构被控端二进制所在目录
	InstallScript []byte

	limiter  *auth.LoginLimiter
	apiMux   *http.ServeMux
	agentMux *http.ServeMux
}

// Init 注册路由，需在字段赋值完成后调用。
func (h *Handler) Init() {
	h.limiter = auth.NewLoginLimiter(5, 15*time.Minute)

	m := http.NewServeMux()
	m.HandleFunc("GET /api/session", h.getSession)
	m.HandleFunc("POST /api/login", h.login)
	m.HandleFunc("POST /api/logout", h.authed(h.logout))
	m.HandleFunc("PUT /api/account", h.authed(h.putAccount))
	m.HandleFunc("GET /api/nodes", h.authed(h.listNodes))
	m.HandleFunc("POST /api/nodes", h.authed(h.createNode))
	m.HandleFunc("PUT /api/nodes/{id}", h.authed(h.updateNode))
	m.HandleFunc("DELETE /api/nodes/{id}", h.authed(h.deleteNode))
	m.HandleFunc("POST /api/nodes/{id}/install-command", h.authed(h.installCommand))
	m.HandleFunc("POST /api/nodes/{id}/xray", h.authed(h.xrayAction))
	m.HandleFunc("GET /api/nodes/{id}/logs", h.authed(h.nodeLogs))
	m.HandleFunc("POST /api/nodes/{id}/probe", h.authed(h.probeNode))
	m.HandleFunc("POST /api/nodes/{id}/sync", h.authed(h.syncNode))
	m.HandleFunc("GET /api/inbounds", h.authed(h.listInbounds))
	m.HandleFunc("POST /api/inbounds", h.authed(h.createInbound))
	m.HandleFunc("PUT /api/inbounds/{id}", h.authed(h.updateInbound))
	m.HandleFunc("DELETE /api/inbounds/{id}", h.authed(h.deleteInbound))
	m.HandleFunc("GET /api/inbounds/{id}/links", h.authed(h.inboundLinks))
	m.HandleFunc("GET /api/subscriptions", h.authed(h.listSubscriptions))
	m.HandleFunc("POST /api/subscriptions", h.authed(h.createSubscription))
	m.HandleFunc("PUT /api/subscriptions/{id}", h.authed(h.updateSubscription))
	m.HandleFunc("DELETE /api/subscriptions/{id}", h.authed(h.deleteSubscription))
	m.HandleFunc("POST /api/subscriptions/{id}/reset-credentials", h.authed(h.resetSubscriptionCredentials))
	m.HandleFunc("POST /api/tools/x25519", h.authed(h.x25519))
	m.HandleFunc("GET /api/tools/short-id", h.authed(h.shortID))
	m.HandleFunc("GET /api/xray/releases", h.authed(h.xrayReleases))
	m.HandleFunc("GET /api/settings", h.authed(h.getSettings))
	m.HandleFunc("PUT /api/settings/access", h.authed(h.putAccess))
	m.HandleFunc("PUT /api/settings/xray", h.authed(h.putXray))
	m.HandleFunc("PUT /api/settings/security", h.authed(h.putSecurity))
	m.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { writeError(w, http.StatusNotFound, "接口不存在") })
	h.apiMux = m

	a := http.NewServeMux()
	a.HandleFunc("GET /agent/install.sh", h.installScript)
	a.HandleFunc("GET /agent/dl/agent/{arch}", h.downloadAgent)
	a.HandleFunc("GET /agent/dl/xray/{tag}/{arch}", h.downloadXray)
	a.HandleFunc("POST /agent/enroll", h.enroll)
	a.HandleFunc("GET /agent/ws", h.agentWS)
	h.agentMux = a
}

// ServeHTTP 按路径前缀分发：/agent/ 为被控端接口，/{面板路径}/ 为面板，其余一律 404。
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	if strings.HasPrefix(p, "/agent/") {
		h.agentMux.ServeHTTP(w, r)
		return
	}
	panel := "/" + h.Settings.Access().PanelPath
	if p == panel {
		http.Redirect(w, r, panel+"/", http.StatusFound)
		return
	}
	if !strings.HasPrefix(p, panel+"/") {
		http.NotFound(w, r)
		return
	}
	hdr := w.Header()
	hdr.Set("X-Frame-Options", "DENY")
	hdr.Set("X-Content-Type-Options", "nosniff")
	hdr.Set("Referrer-Policy", "no-referrer")
	rest := p[len(panel):]
	if strings.HasPrefix(rest, "/api/") {
		http.StripPrefix(panel, h.apiMux).ServeHTTP(w, r)
		return
	}
	h.serveSPA(w, r, panel+"/", strings.TrimPrefix(rest, "/"))
}

// Endpoint 返回当前推荐给被控端的主控地址。
func (h *Handler) Endpoint() protocol.Endpoint {
	a := h.Settings.Access()
	ep := protocol.Endpoint{URL: a.PublicURL}
	if a.TLSMode == settings.TLSSelfSigned {
		ep.FP, _ = h.Certs.SelfSigned()
	}
	return ep
}

// OnAgentHello 在被控端握手后调用：校验配置是否需要同步；未安装 Xray 时自动安装默认版本。
func (h *Handler) OnAgentHello(nodeID int64, hello protocol.Hello) {
	h.Syncer.OnHello(nodeID, hello)
	if hello.Xray.Installed || !xrayrel.SupportedArch(hello.Host.Arch) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	ver, err := h.defaultXrayVersion(ctx)
	if err != nil {
		h.Log.Warn("resolve default xray version", "err", err)
		return
	}
	h.Log.Info("auto installing xray", "node", nodeID, "version", ver)
	if err := h.Hub.Request(ctx, nodeID, protocol.TypeXray, protocol.XrayCommand{Action: protocol.XrayInstall, Version: ver}, nil); err != nil {
		h.Log.Warn("auto install xray", "node", nodeID, "err", err)
	}
}

// defaultXrayVersion 返回设置中的默认版本，未设置时取不超过上限的最新稳定版。
func (h *Handler) defaultXrayVersion(ctx context.Context) (string, error) {
	x := h.Settings.Xray()
	if x.DefaultVersion != "" {
		return x.DefaultVersion, nil
	}
	list, err := h.Releases.List(ctx)
	if err != nil {
		return "", err
	}
	if v, ok := xrayrel.LatestStable(list, x.MaxVersion); ok {
		return v, nil
	}
	return "", errors.New("没有可用的 Xray 稳定版本")
}

// clientIP 返回真实客户端 IP：仅当直连方属于可信代理时才采信转发头。
func (h *Handler) clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	remote, err := netip.ParseAddr(host)
	if err != nil || !h.isTrusted(remote) {
		return host
	}
	for _, name := range []string{"CF-Connecting-IP", "X-Real-IP"} {
		if v := strings.TrimSpace(r.Header.Get(name)); v != "" {
			if _, err := netip.ParseAddr(v); err == nil {
				return v
			}
		}
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		for i := len(parts) - 1; i >= 0; i-- {
			a, err := netip.ParseAddr(strings.TrimSpace(parts[i]))
			if err != nil {
				break
			}
			if !h.isTrusted(a) {
				return a.String()
			}
		}
	}
	return host
}

func (h *Handler) isTrusted(a netip.Addr) bool {
	a = a.Unmap()
	for _, p := range h.Settings.TrustedProxies() {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// isHTTPS 判断请求在浏览器侧是否为 HTTPS，用于决定 Cookie 的 Secure 属性。
func (h *Handler) isHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	if a, err := netip.ParseAddr(host); err == nil && h.isTrusted(a) {
		return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
	}
	return false
}

func readJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	if err := dec.Decode(v); err != nil {
		return errors.New("请求格式错误")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
