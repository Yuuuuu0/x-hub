package api

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Yuuuuu0/x-hub/internal/master/auth"
	"github.com/Yuuuuu0/x-hub/internal/master/model"
	"github.com/Yuuuuu0/x-hub/internal/master/store"
	"github.com/Yuuuuu0/x-hub/internal/master/sub"
)

// CreateSubscription 新建覆盖全部入站的订阅，并生成链接令牌与凭据。
func CreateSubscription(st *store.Store, name, note string) (int64, error) {
	return st.CreateSubscription(&store.Subscription{
		Name: name, Note: note, Token: auth.RandomToken(), Secret: auth.RandomToken(),
		UUID: model.RandomUUID(), Enabled: true, AllInbounds: true,
	})
}

// subView 为订阅列表项：附带订阅链接与单独授权的入站。
type subView struct {
	*store.Subscription
	URL        string  `json:"url"`
	InboundIDs []int64 `json:"inboundIds"`
}

// subscriptionURL 返回订阅链接，未设置公开访问地址时为空。
func (h *Handler) subscriptionURL(token string) string {
	base := h.Settings.Access().PublicURL
	if base == "" {
		return ""
	}
	return base + "/" + h.Settings.Subscription().Path + "/" + token
}

func (h *Handler) viewSubscription(s *store.Subscription, ids map[int64][]int64) subView {
	v := subView{Subscription: s, URL: h.subscriptionURL(s.Token), InboundIDs: ids[s.ID]}
	if v.InboundIDs == nil {
		v.InboundIDs = []int64{}
	}
	return v
}

func (h *Handler) listSubscriptions(w http.ResponseWriter, r *http.Request) {
	list, err := h.Store.ListSubscriptions()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	ids, err := h.Store.SubscriptionInboundIDs()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]subView, 0, len(list))
	for _, s := range list {
		out = append(out, h.viewSubscription(s, ids))
	}
	writeJSON(w, http.StatusOK, map[string]any{"subscriptions": out})
}

// writeSubscription 返回单个订阅的最新视图。
func (h *Handler) writeSubscription(w http.ResponseWriter, status int, id int64) {
	s, err := h.Store.GetSubscription(id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	ids, _ := h.Store.SubscriptionInboundIDs()
	writeJSON(w, status, h.viewSubscription(s, ids))
}

// subInput 为订阅编辑输入。AllInbounds 为空表示不修改入站范围（如仅切换启用状态）。
type subInput struct {
	Name        string  `json:"name"`
	Note        string  `json:"note"`
	Enabled     bool    `json:"enabled"`
	AllInbounds *bool   `json:"allInbounds"`
	InboundIDs  []int64 `json:"inboundIds"`
}

func (in *subInput) validate() error {
	in.Name = strings.TrimSpace(in.Name)
	in.Note = strings.TrimSpace(in.Note)
	if n := utf8.RuneCountInString(in.Name); n < 1 || n > 32 {
		return errors.New("名称长度需在 1 到 32 之间")
	}
	if utf8.RuneCountInString(in.Note) > 200 {
		return errors.New("备注不能超过 200 字")
	}
	return nil
}

func (h *Handler) createSubscription(w http.ResponseWriter, r *http.Request) {
	var in subInput
	if err := readJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := in.validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	id, err := CreateSubscription(h.Store, in.Name, in.Note)
	if err == nil && in.AllInbounds != nil && !*in.AllInbounds {
		err = h.Store.SetSubscriptionScope(id, false, in.InboundIDs)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.Syncer.TriggerAll()
	h.writeSubscription(w, http.StatusCreated, id)
}

// updateSubscription 更新名称、启用状态与入站范围。
func (h *Handler) updateSubscription(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in subInput
	if err := readJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := in.validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.Store.UpdateSubscription(id, in.Name, in.Note, in.Enabled); err != nil {
		writeStoreError(w, err)
		return
	}
	if in.AllInbounds != nil {
		if err := h.Store.SetSubscriptionScope(id, *in.AllInbounds, in.InboundIDs); err != nil {
			writeStoreError(w, err)
			return
		}
	}
	h.Syncer.TriggerAll()
	h.writeSubscription(w, http.StatusOK, id)
}

func (h *Handler) deleteSubscription(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := h.Store.DeleteSubscription(id); err != nil {
		writeStoreError(w, err)
		return
	}
	h.Syncer.TriggerAll()
	w.WriteHeader(http.StatusNoContent)
}

// resetSubscriptionCredentials 更换订阅凭据，已导入的旧配置随之失效。
func (h *Handler) resetSubscriptionCredentials(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := h.Store.ResetSubscriptionCredentials(id, auth.RandomToken(), model.RandomUUID()); err != nil {
		writeStoreError(w, err)
		return
	}
	h.Syncer.TriggerAll()
	h.writeSubscription(w, http.StatusOK, id)
}

// resetSubscriptionToken 更换订阅链接，旧链接失效，已导入客户端的连接不受影响。
func (h *Handler) resetSubscriptionToken(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := h.Store.ResetSubscriptionToken(id, auth.RandomToken()); err != nil {
		writeStoreError(w, err)
		return
	}
	h.writeSubscription(w, http.StatusOK, id)
}

// serveSubscription 输出订阅内容。令牌无效或订阅停用时一律 404，不暴露原因。
func (h *Handler) serveSubscription(w http.ResponseWriter, r *http.Request, token string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.NotFound(w, r)
		return
	}
	s, err := h.Store.SubscriptionByToken(token)
	if err != nil || !s.Enabled {
		http.NotFound(w, r)
		return
	}
	cfg := h.Settings.Subscription()
	nodes, err := sub.Collect(h.Store, s, cfg.NameTemplate)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	format := sub.DetectFormat(r.UserAgent(), r.URL.Query().Get("format"))
	body, ctype, err := sub.Render(format, nodes)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	hdr := w.Header()
	hdr.Set("Content-Type", ctype)
	hdr.Set("Cache-Control", "no-store")
	hdr.Set("Subscription-Userinfo", sub.UserInfo(s))
	hdr.Set("Profile-Update-Interval", strconv.Itoa(cfg.UpdateHours))
	hdr.Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape("x-hub-"+s.Name))
	h.Log.Info("subscription fetched", "sub", s.ID, "format", format, "nodes", len(nodes), "ip", h.clientIP(r))
	w.Write(body)
}
