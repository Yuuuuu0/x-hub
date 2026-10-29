package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Yuuuuu0/x-hub/internal/master/external"
	"github.com/Yuuuuu0/x-hub/internal/master/routing"
	"github.com/Yuuuuu0/x-hub/internal/master/store"
	"github.com/Yuuuuu0/x-hub/internal/master/sub"
)

// outNode 为订阅输出中的一个节点：渲染用的节点定义，以及 Base64 链接（为空表示不输出到 Base64）。
type outNode struct {
	routing.Node
	Link string
}

// subscriptionNodes 收集订阅可用的全部节点：自有节点在前，随后为可见且启用的外部节点。
// 外部节点名称加前缀并与已有名称去重；前置入站不在此订阅中的来源整体跳过并返回提示。
func (h *Handler) subscriptionNodes(s *store.Subscription) ([]outNode, []string, error) {
	own, err := sub.Collect(h.Store, s, h.Settings.Subscription().NameTemplate)
	if err != nil {
		return nil, nil, err
	}
	out := make([]outNode, 0, len(own))
	taken := map[string]bool{}
	byInbound := map[int64]string{}
	for _, n := range own {
		out = append(out, outNode{Node: routing.Node{Name: n.Name, Own: true, Mihomo: n.Mihomo, SingBox: n.SingBox}, Link: n.Link})
		taken[n.Name] = true
		byInbound[n.InboundID] = n.Name
	}
	sources, err := h.Store.ListExternalSources()
	if err != nil {
		return nil, nil, err
	}
	var warnings []string
	for _, src := range sources {
		if !src.Enabled || !src.VisibleTo(s.ID) {
			continue
		}
		var dialer string
		dialerNode := false
		switch kind, ref, _ := strings.Cut(src.Chain, ":"); kind {
		case chainInbound:
			id, _ := strconv.ParseInt(ref, 10, 64)
			name, ok := byInbound[id]
			if !ok {
				warnings = append(warnings, fmt.Sprintf("外部节点「%s」的前置入站不在此订阅中，已跳过", src.Name))
				continue
			}
			dialer, dialerNode = name, true
		case chainGroup:
			dialer = ref
		}
		for _, p := range filteredProxies(src) {
			name := src.Prefix + p.Name
			for i := 2; taken[name]; i++ {
				name = fmt.Sprintf("%s%s %d", src.Prefix, p.Name, i)
			}
			taken[name] = true
			sb, _ := external.SingBox(p.Mihomo, name)
			out = append(out, outNode{
				Node: routing.Node{Name: name, Mihomo: p.Mihomo, SingBox: sb, Dialer: dialer, DialerNode: dialerNode},
				Link: p.ShareLink(name),
			})
		}
	}
	return out, warnings, nil
}

// filteredProxies 返回来源中按包含 / 排除正则筛选后的节点。
func filteredProxies(src *store.ExternalSource) []external.Proxy {
	var proxies []external.Proxy
	if json.Unmarshal(src.Nodes, &proxies) != nil {
		return nil
	}
	inc, _ := regexp.Compile(src.Include)
	exc, _ := regexp.Compile(src.Exclude)
	out := proxies[:0]
	for _, p := range proxies {
		if (src.Include != "" && inc != nil && !inc.MatchString(p.Name)) || (src.Exclude != "" && exc != nil && exc.MatchString(p.Name)) {
			continue
		}
		out = append(out, p)
	}
	return out
}

func routingNodes(nodes []outNode) []routing.Node {
	out := make([]routing.Node, len(nodes))
	for i, n := range nodes {
		out[i] = n.Node
	}
	return out
}

// 前置类型。
const (
	chainInbound = "inbound"
	chainGroup   = "group"
)

type externalInput struct {
	Kind      string  `json:"kind"`
	Name      string  `json:"name"`
	Enabled   *bool   `json:"enabled"`
	Content   string  `json:"content"`
	UserAgent string  `json:"userAgent"`
	Interval  int     `json:"interval"`
	Prefix    string  `json:"prefix"`
	Include   string  `json:"include"`
	Exclude   string  `json:"exclude"`
	Chain     string  `json:"chain"`
	AllSubs   bool    `json:"allSubs"`
	SubIDs    []int64 `json:"subIds"`
}

// validateExternal 校验并规范化输入。
func (h *Handler) validateExternal(in *externalInput) error {
	in.Name, in.Content = strings.TrimSpace(in.Name), strings.TrimSpace(in.Content)
	in.Include, in.Exclude = strings.TrimSpace(in.Include), strings.TrimSpace(in.Exclude)
	if in.Name == "" || len([]rune(in.Name)) > 64 {
		return errors.New("名称为 1–64 个字符")
	}
	switch in.Kind {
	case store.ExternalManual:
		if in.Content == "" {
			return errors.New("请粘贴节点链接或 Mihomo 节点定义")
		}
		in.UserAgent, in.Interval = "", 0
	case store.ExternalRemote:
		u, err := url.Parse(in.Content)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return errors.New("订阅地址应为 http(s) 链接")
		}
		if in.Interval < 3600 {
			in.Interval = 3600
		}
	default:
		return errors.New("来源类型无效")
	}
	for _, re := range []string{in.Include, in.Exclude} {
		if _, err := regexp.Compile(re); err != nil {
			return fmt.Errorf("正则无效：%s", re)
		}
	}
	kind, ref, _ := strings.Cut(in.Chain, ":")
	switch kind {
	case "":
	case chainInbound:
		id, err := strconv.ParseInt(ref, 10, 64)
		if err != nil {
			return errors.New("前置入站无效")
		}
		if _, err := h.Store.GetInbound(id); err != nil {
			return errors.New("前置入站不存在")
		}
	case chainGroup:
		if strings.TrimSpace(ref) == "" {
			return errors.New("请填写前置分组名称")
		}
	default:
		return errors.New("前置类型无效")
	}
	if in.SubIDs == nil {
		in.SubIDs = []int64{}
	}
	return nil
}

// loadNodes 解析手动来源的内容或拉取外部订阅，写入来源的节点与拉取状态。手动来源解析失败时返回错误。
func loadNodes(ctx context.Context, e *store.ExternalSource) error {
	if e.Kind == store.ExternalManual {
		proxies, _, err := external.Parse(e.Content)
		if err != nil {
			return err
		}
		if len(proxies) == 0 {
			return errors.New("没有解析出任何节点")
		}
		e.Nodes, _ = json.Marshal(proxies)
		return nil
	}
	e.FetchedAt = time.Now().Unix()
	res, err := external.Fetch(ctx, e.Content, e.UserAgent)
	if err != nil {
		e.FetchError = err.Error()
		return nil
	}
	e.Nodes, _ = json.Marshal(res.Proxies)
	e.UserInfo, e.FetchError = res.UserInfo, ""
	return nil
}

type externalNodeView struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Server   string `json:"server"`
	Excluded bool   `json:"excluded"`
}

// externalView 为列表中的来源，附节点摘要（被筛选掉的标记 excluded）。
func externalView(e *store.ExternalSource) map[string]any {
	var proxies []external.Proxy
	json.Unmarshal(e.Nodes, &proxies)
	kept := map[string]bool{}
	for _, p := range filteredProxies(e) {
		kept[p.Name] = true
	}
	nodes := make([]externalNodeView, 0, len(proxies))
	for _, p := range proxies {
		nodes = append(nodes, externalNodeView{Name: e.Prefix + p.Name, Type: fmt.Sprint(p.Mihomo["type"]), Server: fmt.Sprint(p.Mihomo["server"]), Excluded: !kept[p.Name]})
	}
	return map[string]any{"source": e, "nodes": nodes}
}

func (h *Handler) listExternal(w http.ResponseWriter, r *http.Request) {
	list, err := h.Store.ListExternalSources()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, e := range list {
		out = append(out, externalView(e))
	}
	writeJSON(w, http.StatusOK, map[string]any{"sources": out})
}

// parseExternal 试解析手动内容或试拉取订阅，供表单预览，不保存。
func (h *Handler) parseExternal(w http.ResponseWriter, r *http.Request) {
	var in externalInput
	if err := readJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var proxies []external.Proxy
	var skipped []string
	var userInfo string
	if in.Kind == store.ExternalRemote {
		ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
		defer cancel()
		res, err := external.Fetch(ctx, strings.TrimSpace(in.Content), in.UserAgent)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		proxies, skipped, userInfo = res.Proxies, res.Skipped, res.UserInfo
	} else {
		var err error
		if proxies, skipped, err = external.Parse(in.Content); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	e := &store.ExternalSource{Prefix: in.Prefix, Include: in.Include, Exclude: in.Exclude}
	e.Nodes, _ = json.Marshal(proxies)
	view := externalView(e)
	if skipped == nil {
		skipped = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"nodes": view["nodes"], "skipped": skipped, "userInfo": userInfo})
}

func (h *Handler) createExternal(w http.ResponseWriter, r *http.Request) {
	var in externalInput
	if err := readJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.validateExternal(&in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	e := &store.ExternalSource{Kind: in.Kind, Enabled: true, Nodes: json.RawMessage("[]")}
	applyExternalInput(e, &in)
	if err := loadNodes(r.Context(), e); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	id, err := h.Store.CreateExternalSource(e)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	e, _ = h.Store.GetExternalSource(id)
	writeJSON(w, http.StatusCreated, externalView(e))
}

func (h *Handler) updateExternal(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	e, err := h.Store.GetExternalSource(id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	var in externalInput
	if err := readJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	in.Kind = e.Kind
	if err := h.validateExternal(&in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	reload := in.Content != e.Content || in.UserAgent != e.UserAgent
	applyExternalInput(e, &in)
	if reload {
		if err := loadNodes(r.Context(), e); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if err := h.Store.UpdateExternalSource(e); err != nil {
		writeStoreError(w, err)
		return
	}
	e, _ = h.Store.GetExternalSource(id)
	writeJSON(w, http.StatusOK, externalView(e))
}

func applyExternalInput(e *store.ExternalSource, in *externalInput) {
	e.Name, e.Content, e.UserAgent, e.Interval = in.Name, in.Content, in.UserAgent, in.Interval
	e.Prefix, e.Include, e.Exclude, e.Chain = in.Prefix, in.Include, in.Exclude, in.Chain
	e.AllSubs, e.SubIDs = in.AllSubs, in.SubIDs
	if in.Enabled != nil {
		e.Enabled = *in.Enabled
	}
}

func (h *Handler) deleteExternal(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := h.Store.DeleteExternalSource(id); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// refreshExternal 立即拉取外部订阅。
func (h *Handler) refreshExternal(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	e, err := h.Store.GetExternalSource(id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if e.Kind != store.ExternalRemote {
		writeError(w, http.StatusBadRequest, "只有订阅来源可以更新")
		return
	}
	h.fetchExternal(r.Context(), e)
	e, _ = h.Store.GetExternalSource(id)
	writeJSON(w, http.StatusOK, externalView(e))
}

// fetchExternal 拉取外部订阅并记录结果；失败时保留上次的节点。
func (h *Handler) fetchExternal(ctx context.Context, e *store.ExternalSource) {
	res, err := external.Fetch(ctx, e.Content, e.UserAgent)
	if err != nil {
		h.Log.Warn("fetch external subscription", "source", e.ID, "err", err)
		h.Store.SetExternalFetch(e.ID, nil, "", err.Error())
		return
	}
	nodes, _ := json.Marshal(res.Proxies)
	h.Store.SetExternalFetch(e.ID, nodes, res.UserInfo, "")
}

// RunExternalRefresh 定时拉取到期的外部订阅，直到 ctx 取消。
func (h *Handler) RunExternalRefresh(ctx context.Context) {
	t := time.NewTicker(5 * time.Minute)
	defer t.Stop()
	for {
		list, err := h.Store.ListExternalSources()
		if err == nil {
			now := time.Now().Unix()
			for _, e := range list {
				if e.Kind == store.ExternalRemote && e.Enabled && e.FetchedAt+int64(e.Interval) <= now {
					h.fetchExternal(ctx, e)
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
