package api

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Yuuuuu0/x-hub/internal/master/auth"
	"github.com/Yuuuuu0/x-hub/internal/master/store"
	"github.com/Yuuuuu0/x-hub/internal/master/xrayrel"
	"github.com/Yuuuuu0/x-hub/internal/shared/protocol"
)

func (h *Handler) installScript(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(h.InstallScript)
}

// downloadAgent 提供被控端二进制，保证与主控版本一致。
func (h *Handler) downloadAgent(w http.ResponseWriter, r *http.Request) {
	arch := r.PathValue("arch")
	if !xrayrel.SupportedArch(arch) {
		http.NotFound(w, r)
		return
	}
	path := filepath.Join(h.AgentDir, "xhub-agent-linux-"+arch)
	if _, err := os.Stat(path); err != nil {
		writeError(w, http.StatusNotFound, "主控未打包该架构的被控端程序")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeFile(w, r, path)
}

// downloadXray 中转 Xray 安装包，仅已接入的被控端可下载，响应头附带 SHA-256 供校验。
func (h *Handler) downloadXray(w http.ResponseWriter, r *http.Request) {
	if _, err := h.nodeFromBearer(r); err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	path, sum, err := h.Releases.Ensure(r.Context(), r.PathValue("tag"), r.PathValue("arch"))
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	w.Header().Set("X-Content-Sha256", sum)
	w.Header().Set("Content-Type", "application/zip")
	http.ServeFile(w, r, path)
}

// enroll 用一次性令牌换取节点长期凭据。
func (h *Handler) enroll(w http.ResponseWriter, r *http.Request) {
	var req protocol.EnrollRequest
	if err := readJSON(r, &req); err != nil || req.Token == "" {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	secret := auth.RandomToken()
	id, err := h.Store.ConsumeEnrollToken(auth.HashToken(req.Token), auth.HashToken(secret))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusUnauthorized, "接入令牌无效或已过期，请在面板重新生成安装命令")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.Hub.Kick(id) // 重装场景下旧凭据已失效，断开旧连接
	h.Log.Info("agent enrolled", "node", id, "hostname", req.Hostname, "ip", h.clientIP(r))
	writeJSON(w, http.StatusOK, protocol.EnrollResponse{NodeID: id, Secret: secret})
}

func (h *Handler) agentWS(w http.ResponseWriter, r *http.Request) {
	n, err := h.nodeFromBearer(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	h.Hub.Serve(w, r, n.ID, h.clientIP(r))
}

var errBadCredential = errors.New("节点凭据无效")

// nodeFromBearer 校验 "Authorization: Bearer <nodeID>.<secret>"。
func (h *Handler) nodeFromBearer(r *http.Request) (*store.Node, error) {
	raw, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		return nil, errBadCredential
	}
	idStr, secret, ok := strings.Cut(raw, ".")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if !ok || err != nil || secret == "" {
		return nil, errBadCredential
	}
	n, err := h.Store.GetNode(id)
	if err != nil || n.SecretHash() == "" {
		return nil, errBadCredential
	}
	if subtle.ConstantTimeCompare([]byte(auth.HashToken(secret)), []byte(n.SecretHash())) != 1 {
		return nil, errBadCredential
	}
	return n, nil
}
