package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Yuuuuu0/x-hub/internal/master/model"
	"github.com/Yuuuuu0/x-hub/internal/master/store"
	"github.com/Yuuuuu0/x-hub/internal/master/sub"
	"github.com/Yuuuuu0/x-hub/internal/shared/protocol"
)

// inboundView 为入站列表项，附带所属服务器名称与分享用的公钥等派生信息。
type inboundView struct {
	*store.Inbound
	NodeName string `json:"nodeName"`
}

func (h *Handler) listInbounds(w http.ResponseWriter, r *http.Request) {
	list, err := h.Store.ListInbounds()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	nodes, err := h.Store.ListNodes()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	names := map[int64]string{}
	for _, n := range nodes {
		names[n.ID] = n.Name
	}
	out := make([]inboundView, 0, len(list))
	for _, in := range list {
		out = append(out, inboundView{Inbound: in, NodeName: names[in.NodeID]})
	}
	writeJSON(w, http.StatusOK, map[string]any{"inbounds": out})
}

type inboundInput struct {
	NodeID   int64           `json:"nodeId"`
	Remark   string          `json:"remark"`
	Enabled  bool            `json:"enabled"`
	Listen   string          `json:"listen"`
	Port     int             `json:"port"`
	Protocol string          `json:"protocol"`
	Spec     json.RawMessage `json:"spec"`
	LinkHost string          `json:"linkHost"`
	LinkPort int             `json:"linkPort"`
}

// toInbound 校验输入并转换为存储结构，Spec 以规范化后的形式保存（含派生公钥）。
func (in *inboundInput) toInbound() (*store.Inbound, error) {
	in.Remark = strings.TrimSpace(in.Remark)
	in.Listen = strings.TrimSpace(in.Listen)
	in.LinkHost = strings.TrimSpace(in.LinkHost)
	if utf8.RuneCountInString(in.Remark) > 64 {
		return nil, errors.New("备注不能超过 64 字")
	}
	if in.Listen != "" && net.ParseIP(in.Listen) == nil {
		return nil, errors.New("监听 IP 无效，留空表示监听全部地址")
	}
	if in.Port < 1 || in.Port > 65535 {
		return nil, errors.New("端口需在 1 到 65535 之间")
	}
	if in.LinkPort < 0 || in.LinkPort > 65535 {
		return nil, errors.New("链接端口需在 0 到 65535 之间")
	}
	if strings.ContainsAny(in.LinkHost, "/@ []") || strings.Count(in.LinkHost, ":") == 1 {
		return nil, errors.New("链接地址只填域名或 IP")
	}
	spec, err := model.ParseSpec(in.Protocol, in.Spec)
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(spec)
	return &store.Inbound{
		NodeID: in.NodeID, Remark: in.Remark, Enabled: in.Enabled, Listen: in.Listen, Port: in.Port,
		Protocol: in.Protocol, Spec: raw, LinkHost: in.LinkHost, LinkPort: in.LinkPort,
	}, nil
}

// checkPort 在服务器在线时确认端口未被其他程序占用；离线时跳过，由下发时的配置校验兜底。
func (h *Handler) checkPort(ctx context.Context, nodeID int64, port int) error {
	if !h.Hub.State(nodeID).Online {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var res protocol.ProbePortResult
	if err := h.Hub.Request(ctx, nodeID, protocol.TypeProbe, protocol.ProbeRequest{Kind: protocol.ProbePort, Port: port}, &res); err != nil {
		return nil // 旧版被控端不支持探测时不阻塞保存
	}
	if !res.Free && !res.Owned {
		return fmt.Errorf("端口 %d 已被服务器上的其他程序占用", port)
	}
	return nil
}

func (h *Handler) createInbound(w http.ResponseWriter, r *http.Request) {
	var input inboundInput
	if err := readJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	in, err := input.toInbound()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := h.Store.GetNode(in.NodeID); err != nil {
		writeError(w, http.StatusBadRequest, "所属服务器不存在")
		return
	}
	if err := h.checkPort(r.Context(), in.NodeID, in.Port); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	id, err := h.Store.CreateInbound(in)
	if err != nil {
		writeInboundError(w, err)
		return
	}
	h.Syncer.Trigger(in.NodeID)
	saved, _ := h.Store.GetInbound(id)
	writeJSON(w, http.StatusCreated, saved)
}

func (h *Handler) updateInbound(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	old, err := h.Store.GetInbound(id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	var input inboundInput
	if err := readJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	input.NodeID = old.NodeID
	in, err := input.toInbound()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	in.ID = id
	if in.Port != old.Port {
		if err := h.checkPort(r.Context(), in.NodeID, in.Port); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if err := h.Store.UpdateInbound(in); err != nil {
		writeInboundError(w, err)
		return
	}
	h.Syncer.Trigger(in.NodeID)
	saved, _ := h.Store.GetInbound(id)
	writeJSON(w, http.StatusOK, saved)
}

func (h *Handler) deleteInbound(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	in, err := h.Store.GetInbound(id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if err := h.Store.DeleteInbound(id); err != nil {
		writeStoreError(w, err)
		return
	}
	h.Syncer.Trigger(in.NodeID)
	w.WriteHeader(http.StatusNoContent)
}

// inboundLinks 返回该入站对每个有效订阅的分享链接。
func (h *Handler) inboundLinks(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	in, err := h.Store.GetInbound(id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	node, err := h.Store.GetNode(in.NodeID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	spec, err := model.ParseSpec(in.Protocol, in.Spec)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	target := sub.Target(node, in, h.Settings.Subscription().NameTemplate)
	if target.Host == "" {
		writeError(w, http.StatusBadRequest, "服务器未设置公网地址，请先编辑服务器或在入站中填写链接地址")
		return
	}
	subs, err := h.Store.InboundSubscriptions(in.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	type link struct {
		SubscriptionID   int64  `json:"subscriptionId"`
		SubscriptionName string `json:"subscriptionName"`
		Link             string `json:"link"`
	}
	out := make([]link, 0, len(subs))
	for _, sb := range subs {
		out = append(out, link{sb.ID, sb.Name, model.ShareLink(spec, target, sb.UUID)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"links": out, "enabled": in.Enabled})
}

func writeInboundError(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrPortConflict) {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeStoreError(w, err)
}

// x25519 生成 Reality 密钥对；传入私钥时只派生公钥。
func (h *Handler) x25519(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PrivateKey string `json:"privateKey"`
	}
	readJSON(r, &req)
	if req.PrivateKey == "" {
		priv, pub := model.GenerateX25519()
		writeJSON(w, http.StatusOK, map[string]string{"privateKey": priv, "publicKey": pub})
		return
	}
	pub, err := model.PublicKeyFromPrivate(strings.TrimSpace(req.PrivateKey))
	if err != nil {
		writeError(w, http.StatusBadRequest, "私钥无效")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"privateKey": strings.TrimSpace(req.PrivateKey), "publicKey": pub})
}

func (h *Handler) shortID(w http.ResponseWriter, r *http.Request) {
	n, _ := strconv.Atoi(r.URL.Query().Get("bytes"))
	if n == 0 {
		n = 8
	}
	writeJSON(w, http.StatusOK, map[string]string{"shortId": model.RandomShortID(n)})
}

// probeNode 在服务器本机执行探测，如检测 Reality 目标网站。
func (h *Handler) probeNode(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req protocol.ProbeRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Kind != protocol.ProbeRealityTarget {
		writeError(w, http.StatusBadRequest, "不支持的探测类型")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	var res protocol.ProbeTargetResult
	if err := h.Hub.Request(ctx, id, protocol.TypeProbe, req, &res); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// syncNode 手动触发一次配置同步。
func (h *Handler) syncNode(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	h.Syncer.Trigger(id)
	w.WriteHeader(http.StatusAccepted)
}
