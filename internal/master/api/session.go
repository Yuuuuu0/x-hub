package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Yuuuuu0/x-hub/internal/master/auth"
	"github.com/Yuuuuu0/x-hub/internal/master/store"
	"github.com/Yuuuuu0/x-hub/internal/version"
)

const sessionCookie = "xhub_session"

type ctxKey struct{}

// authed 包装需要登录的接口，把会话放入请求上下文。
func (h *Handler) authed(fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s := h.currentSession(r)
		if s == nil {
			writeError(w, http.StatusUnauthorized, "未登录或会话已过期")
			return
		}
		fn(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, s)))
	}
}

func (h *Handler) currentSession(r *http.Request) *store.Session {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return nil
	}
	s, err := h.Store.SessionByTokenHash(auth.HashToken(c.Value))
	if err != nil {
		return nil
	}
	return s
}

func sessionFrom(r *http.Request) *store.Session {
	s, _ := r.Context().Value(ctxKey{}).(*store.Session)
	return s
}

// getSession 返回登录状态与基础元信息，未登录时也可访问。
func (h *Handler) getSession(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{"authenticated": false, "version": version.Version}
	if s := h.currentSession(r); s != nil {
		out["authenticated"] = true
		out["username"] = s.Username
		out["publicUrlSet"] = h.Settings.Access().PublicURL != ""
	}
	writeJSON(w, http.StatusOK, out)
}

// login 校验用户名密码并签发会话 Cookie，按来源 IP 限制失败次数。
func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	ip := h.clientIP(r)
	if !h.limiter.Allowed(ip) {
		writeError(w, http.StatusTooManyRequests, "登录失败次数过多，请 15 分钟后再试")
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	admin, err := h.Store.AdminByUsername(req.Username)
	if err != nil || !auth.VerifyPassword(req.Password, admin.PasswordHash) {
		h.limiter.Fail(ip)
		h.Log.Warn("login failed", "ip", ip, "username", req.Username)
		writeError(w, http.StatusUnauthorized, "用户名或密码错误")
		return
	}
	h.limiter.Reset(ip)
	token := auth.RandomToken()
	ttl := time.Duration(h.Settings.Security().SessionHours) * time.Hour
	if err := h.Store.CreateSession(admin.ID, auth.HashToken(token), ip, truncate(r.UserAgent(), 256), ttl); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/", MaxAge: int(ttl.Seconds()),
		HttpOnly: true, Secure: h.isHTTPS(r), SameSite: http.SameSiteLaxMode,
	})
	writeJSON(w, http.StatusOK, map[string]any{"username": admin.Username})
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	h.Store.DeleteSession(sessionFrom(r).ID)
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
	w.WriteHeader(http.StatusNoContent)
}

// putAccount 修改管理员用户名与密码，需提供当前密码；成功后其他会话全部失效。
func (h *Handler) putAccount(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CurrentPassword string `json:"currentPassword"`
		Username        string `json:"username"`
		NewPassword     string `json:"newPassword"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s := sessionFrom(r)
	admin, err := h.Store.AdminByID(s.AdminID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !auth.VerifyPassword(req.CurrentPassword, admin.PasswordHash) {
		writeError(w, http.StatusBadRequest, "当前密码错误")
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if n := utf8.RuneCountInString(req.Username); n < 3 || n > 32 {
		writeError(w, http.StatusBadRequest, "用户名长度需在 3 到 32 之间")
		return
	}
	hash := admin.PasswordHash
	if req.NewPassword != "" {
		if utf8.RuneCountInString(req.NewPassword) < 8 {
			writeError(w, http.StatusBadRequest, "新密码至少 8 位")
			return
		}
		hash = auth.HashPassword(req.NewPassword)
	}
	if err := h.Store.UpdateAdminCredentials(admin.ID, req.Username, hash, s.ID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "管理员不存在")
			return
		}
		writeError(w, http.StatusBadRequest, "保存失败："+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"username": req.Username})
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
