package api

import (
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/Yuuuuu0/x-hub/internal/master/auth"
	"github.com/Yuuuuu0/x-hub/internal/master/model"
	"github.com/Yuuuuu0/x-hub/internal/master/store"
)

// CreateSubscription 新建覆盖全部入站的订阅，并生成链接令牌与凭据。
func CreateSubscription(st *store.Store, name, note string) (int64, error) {
	return st.CreateSubscription(&store.Subscription{
		Name: name, Note: note, Token: auth.RandomToken(), Secret: auth.RandomToken(),
		UUID: model.RandomUUID(), Enabled: true, AllInbounds: true,
	})
}

func (h *Handler) listSubscriptions(w http.ResponseWriter, r *http.Request) {
	list, err := h.Store.ListSubscriptions()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if list == nil {
		list = []*store.Subscription{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"subscriptions": list})
}

type subInput struct {
	Name    string `json:"name"`
	Note    string `json:"note"`
	Enabled bool   `json:"enabled"`
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
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.Syncer.TriggerAll()
	sub, _ := h.Store.GetSubscription(id)
	writeJSON(w, http.StatusCreated, sub)
}

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
	h.Syncer.TriggerAll()
	sub, _ := h.Store.GetSubscription(id)
	writeJSON(w, http.StatusOK, sub)
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
	sub, _ := h.Store.GetSubscription(id)
	writeJSON(w, http.StatusOK, sub)
}
