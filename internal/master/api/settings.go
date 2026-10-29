package api

import (
	"net"
	"net/http"
	"time"

	"github.com/Yuuuuu0/x-hub/internal/master/settings"
	"github.com/Yuuuuu0/x-hub/internal/shared/protocol"
)

func (h *Handler) getSettings(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{
		"access":       h.Settings.Access(),
		"xray":         h.Settings.Xray(),
		"security":     h.Settings.Security(),
		"subscription": h.Settings.Subscription(),
		"stats":        h.Settings.Stats(),
	}
	if fp, err := h.Certs.SelfSigned(); err == nil {
		out["selfSignedFingerprint"] = fp
	}
	writeJSON(w, http.StatusOK, out)
}

// putAccess 保存访问设置：先预检证书与新端口，再落库生效；监听地址变化时平滑换绑，
// 并把新的主控地址推送给在线被控端，避免切换后被控端失联。
func (h *Handler) putAccess(w http.ResponseWriter, r *http.Request) {
	var a settings.Access
	if err := readJSON(r, &a); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := settings.ValidateAccess(&a); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	switch a.TLSMode {
	case settings.TLSFile:
		if err := h.Certs.CheckFile(a.CertFile, a.KeyFile); err != nil {
			writeError(w, http.StatusBadRequest, "证书加载失败："+err.Error())
			return
		}
	case settings.TLSSelfSigned:
		if _, err := h.Certs.SelfSigned(); err != nil {
			writeError(w, http.StatusInternalServerError, "自签证书生成失败："+err.Error())
			return
		}
	}
	var ln net.Listener
	if a.Listen != h.Server.Addr() {
		var err error
		if ln, err = h.Server.Listen(a.Listen); err != nil {
			writeError(w, http.StatusBadRequest, "监听地址无法绑定："+err.Error())
			return
		}
	}
	before := h.Endpoint()
	if err := h.Settings.SaveAccess(a); err != nil {
		if ln != nil {
			ln.Close()
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if after := h.Endpoint(); after != before && after.URL != "" {
		h.Hub.Broadcast(protocol.TypeEndpoint, after)
	}
	if ln != nil {
		h.Server.SwitchTo(a.Listen, ln, 2*time.Second)
	}
	h.Log.Info("access settings updated", "listen", a.Listen, "tls", a.TLSMode, "publicUrl", a.PublicURL)
	writeJSON(w, http.StatusOK, h.Settings.Access())
}

func (h *Handler) putXray(w http.ResponseWriter, r *http.Request) {
	var x settings.Xray
	if err := readJSON(r, &x); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.Settings.SaveXray(x); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, h.Settings.Xray())
}

func (h *Handler) putSecurity(w http.ResponseWriter, r *http.Request) {
	var s settings.Security
	if err := readJSON(r, &s); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.Settings.SaveSecurity(s); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, h.Settings.Security())
}

func (h *Handler) putSubscriptionSettings(w http.ResponseWriter, r *http.Request) {
	var s settings.Subscription
	if err := readJSON(r, &s); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.Settings.SaveSubscription(s); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, h.Settings.Subscription())
}

func (h *Handler) putStatsSettings(w http.ResponseWriter, r *http.Request) {
	var s settings.Stats
	if err := readJSON(r, &s); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.Settings.SaveStats(s); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, h.Settings.Stats())
}
