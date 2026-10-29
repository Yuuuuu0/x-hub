package api

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Yuuuuu0/x-hub/internal/master/accounting"
	"github.com/Yuuuuu0/x-hub/internal/master/auth"
	"github.com/Yuuuuu0/x-hub/internal/master/hub"
	"github.com/Yuuuuu0/x-hub/internal/master/store"
	"github.com/Yuuuuu0/x-hub/internal/shared/protocol"
	"github.com/Yuuuuu0/x-hub/internal/shared/tlspin"
	"github.com/Yuuuuu0/x-hub/internal/shared/xraycap"
	"github.com/Yuuuuu0/x-hub/internal/version"
)

const enrollTokenTTL = time.Hour

// nodeView 为服务器列表项：持久化字段 + 内存运行时状态 + 配置同步状态 + 当前计费周期流量。
type nodeView struct {
	*store.Node
	Runtime   hub.RuntimeState `json:"runtime"`
	SyncState string           `json:"syncState"`
	NetPeriod netPeriod        `json:"netPeriod"`
}

type netPeriod struct {
	Start int64 `json:"start"`
	Rx    int64 `json:"rx"`
	Tx    int64 `json:"tx"`
}

func (h *Handler) listNodes(w http.ResponseWriter, r *http.Request) {
	nodes, err := h.Store.ListNodes()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]nodeView, 0, len(nodes))
	now, loc := time.Now(), h.Settings.Location()
	for _, n := range nodes {
		v := nodeView{Node: n, Runtime: h.Hub.State(n.ID), SyncState: h.Syncer.State(n)}
		v.NetPeriod.Start = accounting.PeriodStart(n.NetResetDay, now, loc).Unix()
		v.NetPeriod.Rx, v.NetPeriod.Tx, _ = h.Store.NodeTrafficSince(n.ID, v.NetPeriod.Start)
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, map[string]any{"nodes": out, "masterVersion": version.Version})
}

type nodeInput struct {
	Name        string `json:"name"`
	Remark      string `json:"remark"`
	PublicHost  string `json:"publicHost"`
	NetResetDay int    `json:"netResetDay"`
	NetQuota    int64  `json:"netQuotaBytes"`
}

func (in *nodeInput) validate() error {
	in.Name = strings.TrimSpace(in.Name)
	in.Remark = strings.TrimSpace(in.Remark)
	in.PublicHost = strings.TrimSpace(in.PublicHost)
	if n := utf8.RuneCountInString(in.Name); n < 1 || n > 32 {
		return errors.New("名称长度需在 1 到 32 之间")
	}
	if utf8.RuneCountInString(in.Remark) > 200 {
		return errors.New("备注不能超过 200 字")
	}
	if in.NetResetDay == 0 {
		in.NetResetDay = 1
	}
	if in.NetResetDay < 1 || in.NetResetDay > 28 {
		return errors.New("流量重置日需在 1 到 28 之间")
	}
	if in.NetQuota < 0 {
		return errors.New("流量配额不能为负数")
	}
	// 恰好一个冒号意味着带了端口；IPv6 地址含多个冒号，允许。
	if strings.ContainsAny(in.PublicHost, "/@ []") || strings.Count(in.PublicHost, ":") == 1 {
		return errors.New("公网地址只填域名或 IP，不含协议与端口")
	}
	return nil
}

func (h *Handler) createNode(w http.ResponseWriter, r *http.Request) {
	var in nodeInput
	if err := readJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := in.validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	id, err := h.Store.CreateNode(in.Name, in.Remark, in.PublicHost)
	if err == nil {
		err = h.Store.UpdateNode(id, in.Name, in.Remark, in.PublicHost, in.NetResetDay, in.NetQuota)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	n, _ := h.Store.GetNode(id)
	writeJSON(w, http.StatusCreated, n)
}

func (h *Handler) updateNode(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in nodeInput
	if err := readJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := in.validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.Store.UpdateNode(id, in.Name, in.Remark, in.PublicHost, in.NetResetDay, in.NetQuota); err != nil {
		writeStoreError(w, err)
		return
	}
	n, _ := h.Store.GetNode(id)
	writeJSON(w, http.StatusOK, n)
}

func (h *Handler) deleteNode(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := h.Store.DeleteNode(id); err != nil {
		writeStoreError(w, err)
		return
	}
	h.Hub.Kick(id)
	h.Syncer.Forget(id)
	w.WriteHeader(http.StatusNoContent)
}

// installCommand 为服务器生成一次性接入令牌与对应的一行安装命令。
func (h *Handler) installCommand(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if _, err := h.Store.GetNode(id); err != nil {
		writeStoreError(w, err)
		return
	}
	ep := h.Endpoint()
	if ep.URL == "" {
		writeError(w, http.StatusBadRequest, "请先在「设置 → 访问」中填写公开访问地址")
		return
	}
	if err := verifySelfSignedPin(r.Context(), ep); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	token := auth.RandomToken()
	expire, err := h.Store.CreateEnrollToken(id, auth.HashToken(token), enrollTokenTTL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	curl := "curl -fsSL"
	fpArg := ""
	if ep.FP != "" {
		curl += fmt.Sprintf(" -k --pinnedpubkey 'sha256//%s'", ep.FP)
		fpArg = fmt.Sprintf(" --fp '%s'", ep.FP)
	}
	cmd := fmt.Sprintf("%s '%s/agent/install.sh' | bash -s -- --master '%s' --token '%s'%s", curl, ep.URL, ep.URL, token, fpArg)
	writeJSON(w, http.StatusOK, map[string]any{"command": cmd, "expireAt": expire})
}

// xrayAction 控制服务器上的 Xray：启动、停止、重启或安装指定版本。
func (h *Handler) xrayAction(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var cmd protocol.XrayCommand
	if err := readJSON(r, &cmd); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	timeout := 30 * time.Second
	switch cmd.Action {
	case protocol.XrayStart, protocol.XrayStop, protocol.XrayRestart:
		cmd.Version = ""
	case protocol.XrayInstall:
		timeout = 6 * time.Minute
		if err := h.checkInstallVersion(r.Context(), &cmd); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	default:
		writeError(w, http.StatusBadRequest, "未知操作")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	var st protocol.XrayStatus
	if err := h.Hub.Request(ctx, id, protocol.TypeXray, cmd, &st); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// checkInstallVersion 补全默认版本，并拒绝超过版本上限的安装请求。
func (h *Handler) checkInstallVersion(ctx context.Context, cmd *protocol.XrayCommand) error {
	if cmd.Version == "" {
		v, err := h.defaultXrayVersion(ctx)
		if err != nil {
			return err
		}
		cmd.Version = v
	}
	v, ok := xraycap.ParseVersion(cmd.Version)
	if !ok || !xraycap.IsValidTag(cmd.Version) {
		return errors.New("版本号格式错误")
	}
	if max, ok := xraycap.ParseVersion(h.Settings.Xray().MaxVersion); ok && v.Compare(max) > 0 {
		return fmt.Errorf("版本 %s 超过了设置的上限 %s", cmd.Version, max)
	}
	return nil
}

func (h *Handler) nodeLogs(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	lines, _ := strconv.Atoi(r.URL.Query().Get("lines"))
	if lines <= 0 || lines > 2000 {
		lines = 300
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	var resp protocol.LogsResponse
	if err := h.Hub.Request(ctx, id, protocol.TypeLogs, protocol.LogsRequest{Lines: lines}, &resp); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// xrayReleases 返回 Xray 版本列表，并标注是否超过版本上限。
func (h *Handler) xrayReleases(w http.ResponseWriter, r *http.Request) {
	list, err := h.Releases.List(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	max, hasMax := xraycap.ParseVersion(h.Settings.Xray().MaxVersion)
	type item struct {
		Tag         string    `json:"tag"`
		Prerelease  bool      `json:"prerelease"`
		PublishedAt time.Time `json:"publishedAt"`
		Allowed     bool      `json:"allowed"`
	}
	out := make([]item, 0, len(list))
	for _, rel := range list {
		v, _ := xraycap.ParseVersion(rel.Tag)
		out = append(out, item{rel.Tag, rel.Prerelease, rel.PublishedAt, !hasMax || v.Compare(max) <= 0})
	}
	def, _ := h.defaultXrayVersion(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{"releases": out, "default": def})
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "ID 无效")
		return 0, false
	}
	return id, true
}

func writeStoreError(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "记录不存在")
		return
	}
	writeError(w, http.StatusInternalServerError, err.Error())
}

// pinXray 锁定或解除锁定服务器的 Xray 版本（空字符串为解除）。
func (h *Handler) pinXray(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req struct {
		Version string `json:"version"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Version != "" {
		cmd := protocol.XrayCommand{Version: req.Version}
		if err := h.checkInstallVersion(r.Context(), &cmd); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if err := h.Store.SetNodeXrayPin(id, req.Version); err != nil {
		writeStoreError(w, err)
		return
	}
	n, _ := h.Store.GetNode(id)
	writeJSON(w, http.StatusOK, n)
}

// batchInstall 把所有在线服务器的 Xray 切换到目标版本（锁定版本的服务器切换到其锁定版本），并发执行。
func (h *Handler) batchInstall(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Version string `json:"version"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	cmd := protocol.XrayCommand{Action: protocol.XrayInstall, Version: req.Version}
	if err := h.checkInstallVersion(r.Context(), &cmd); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	nodes, err := h.Store.ListNodes()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	type result struct {
		NodeID  int64  `json:"nodeId"`
		Name    string `json:"name"`
		Version string `json:"version"`
		Status  string `json:"status"` // done / skipped / failed / offline
		Error   string `json:"error,omitempty"`
	}
	out := make([]result, len(nodes))
	ctx, cancel := context.WithTimeout(r.Context(), 6*time.Minute)
	defer cancel()
	var wg sync.WaitGroup
	for i, n := range nodes {
		target := cmd.Version
		if n.XrayPinned != "" {
			target = n.XrayPinned
		}
		out[i] = result{NodeID: n.ID, Name: n.Name, Version: target}
		st := h.Hub.State(n.ID)
		switch {
		case !st.Online:
			out[i].Status = "offline"
			continue
		case st.Xray.Version == target:
			out[i].Status = "skipped"
			continue
		}
		wg.Add(1)
		go func(i int, id int64, target string) {
			defer wg.Done()
			err := h.Hub.Request(ctx, id, protocol.TypeXray, protocol.XrayCommand{Action: protocol.XrayInstall, Version: target}, nil)
			if err != nil {
				out[i].Status, out[i].Error = "failed", err.Error()
			} else {
				out[i].Status = "done"
			}
		}(i, n.ID, target)
	}
	wg.Wait()
	writeJSON(w, http.StatusOK, map[string]any{"results": out})
}

// verifySelfSignedPin 在自签模式下确认公开访问地址返回的确实是主控的自签证书。
// 主控位于 Nginx / Cloudflare 之后时，公开地址返回的是代理的证书，锁定自签公钥的安装命令必然失败。
// 连接不上（如服务器无法回环访问自己的公网地址）时不阻塞，按原样生成。
func verifySelfSignedPin(ctx context.Context, ep protocol.Endpoint) error {
	if ep.FP == "" {
		return nil
	}
	u, err := url.Parse(ep.URL)
	if err != nil || u.Scheme != "https" {
		return nil
	}
	addr := u.Host
	if u.Port() == "" {
		addr = net.JoinHostPort(u.Hostname(), "443")
	}
	d := &tls.Dialer{NetDialer: &net.Dialer{Timeout: 5 * time.Second}, Config: &tls.Config{ServerName: u.Hostname(), InsecureSkipVerify: true}}
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	c, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil
	}
	defer c.Close()
	certs := c.(*tls.Conn).ConnectionState().PeerCertificates
	if len(certs) > 0 && tlspin.Fingerprint(certs[0]) != ep.FP {
		return fmt.Errorf("公开访问地址 %s 返回的不是主控的自签证书（前面可能有 Nginx / Cloudflare）。"+
			"请先把主控切换为反向代理模式（设置 → 访问，或执行 xhub-master settings proxy），再生成安装命令", ep.URL)
	}
	return nil
}
