package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Yuuuuu0/x-hub/internal/master/accounting"
	"github.com/Yuuuuu0/x-hub/internal/master/auth"
	"github.com/Yuuuuu0/x-hub/internal/master/hub"
	"github.com/Yuuuuu0/x-hub/internal/master/store"
	"github.com/Yuuuuu0/x-hub/internal/shared/protocol"
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
