package api

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Yuuuuu0/x-hub/internal/master/auth"
	"github.com/Yuuuuu0/x-hub/internal/master/model"
	"github.com/Yuuuuu0/x-hub/internal/master/routing"
	"github.com/Yuuuuu0/x-hub/internal/master/share"
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

// subView 为订阅列表项：附带订阅链接、单独授权的入站与当前在线 IP。
type subView struct {
	*store.Subscription
	URL        string   `json:"url"`
	InboundIDs []int64  `json:"inboundIds"`
	OnlineIPs  []string `json:"onlineIps"`
}

// subscriptionURL 返回订阅链接，未设置公开访问地址时为空。
func (h *Handler) subscriptionURL(token string) string {
	base := h.Settings.Access().PublicURL
	if base == "" {
		return ""
	}
	return base + "/" + h.Settings.Subscription().Path + "/" + token
}

func (h *Handler) viewSubscription(s *store.Subscription, ids map[int64][]int64, online map[int64][]string) subView {
	v := subView{Subscription: s, URL: h.subscriptionURL(s.Token), InboundIDs: ids[s.ID], OnlineIPs: online[s.ID]}
	if v.InboundIDs == nil {
		v.InboundIDs = []int64{}
	}
	if v.OnlineIPs == nil {
		v.OnlineIPs = []string{}
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
	online := h.Hub.OnlineBySub()
	out := make([]subView, 0, len(list))
	for _, s := range list {
		out = append(out, h.viewSubscription(s, ids, online))
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
	writeJSON(w, status, h.viewSubscription(s, ids, h.Hub.OnlineBySub()))
}

// subInput 为订阅编辑输入。指针字段为空表示不修改（如仅切换启用状态）。
type subInput struct {
	Name        string  `json:"name"`
	Note        string  `json:"note"`
	Enabled     bool    `json:"enabled"`
	AllInbounds *bool   `json:"allInbounds"`
	InboundIDs  []int64 `json:"inboundIds"` // AllInbounds 为假时是授权的入站，为真时是排除的入站
	LimitBytes  *int64  `json:"limitBytes"`
	ExpireAt    *int64  `json:"expireAt"`
	ResetDay    *int    `json:"resetDay"`
	ProfileID   *int64  `json:"profileId"`
}

// quotaSet 表示请求携带了额度相关字段。
func (in *subInput) quotaSet() bool {
	return in.LimitBytes != nil || in.ExpireAt != nil || in.ResetDay != nil
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
	if in.LimitBytes != nil && *in.LimitBytes < 0 {
		return errors.New("流量上限不能为负数")
	}
	if in.ExpireAt != nil && *in.ExpireAt < 0 {
		return errors.New("到期时间无效")
	}
	if in.ResetDay != nil && (*in.ResetDay < 0 || *in.ResetDay > 28) {
		return errors.New("重置日需在 1 到 28 之间，0 表示不重置")
	}
	return nil
}

// applyProfile 设置订阅使用的分流方案，未携带时不修改。
func (h *Handler) applyProfile(id int64, in *subInput) error {
	if in.ProfileID == nil {
		return nil
	}
	if *in.ProfileID != 0 {
		if _, err := h.Store.GetProfile(*in.ProfileID); err != nil {
			return errors.New("所选分流方案不存在")
		}
	}
	return h.Store.SetSubscriptionProfile(id, *in.ProfileID)
}

// applyQuota 写入额度字段，未携带的字段沿用当前值。
func (h *Handler) applyQuota(id int64, in *subInput) error {
	if !in.quotaSet() {
		return nil
	}
	cur, err := h.Store.GetSubscription(id)
	if err != nil {
		return err
	}
	limit, expire, reset := cur.LimitBytes, cur.ExpireAt, cur.ResetDay
	if in.LimitBytes != nil {
		limit = *in.LimitBytes
	}
	if in.ExpireAt != nil {
		expire = *in.ExpireAt
	}
	if in.ResetDay != nil {
		reset = *in.ResetDay
	}
	return h.Store.UpdateSubscriptionQuota(id, limit, expire, reset)
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
	if err == nil && in.AllInbounds != nil {
		err = h.Store.SetSubscriptionScope(id, *in.AllInbounds, in.InboundIDs)
	}
	if err == nil {
		err = h.applyQuota(id, &in)
	}
	if err == nil {
		err = h.applyProfile(id, &in)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.Accounting.Evaluate(id)
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
	if err := h.applyQuota(id, &in); err != nil {
		writeStoreError(w, err)
		return
	}
	if err := h.applyProfile(id, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.Accounting.Evaluate(id)
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

// resetSubscriptionTraffic 清零订阅当前周期用量，超额状态随之恢复。
func (h *Handler) resetSubscriptionTraffic(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := h.Store.ResetSubscriptionUsage(id, time.Now().Unix()); err != nil {
		writeStoreError(w, err)
		return
	}
	h.Accounting.Evaluate(id)
	h.writeSubscription(w, http.StatusOK, id)
}

// subscriptionTraffic 返回订阅最近 days 天按天汇总的流量，以及当前周期在各入站上的明细。
func (h *Handler) subscriptionTraffic(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	s, err := h.Store.GetSubscription(id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	if days < 1 || days > 366 {
		days = 30
	}
	loc := h.Settings.Location()
	now := time.Now().In(loc)
	first := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -(days - 1))
	series, err := h.Store.SubTrafficSeries(id, first.Unix())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	type day struct {
		Day  string `json:"day"`
		Up   int64  `json:"up"`
		Down int64  `json:"down"`
	}
	out := make([]day, days)
	index := map[string]int{}
	for i := range out {
		d := first.AddDate(0, 0, i).Format("2006-01-02")
		out[i].Day, index[d] = d, i
	}
	for _, p := range series {
		if i, ok := index[time.Unix(p.At, 0).In(loc).Format("2006-01-02")]; ok {
			out[i].Up += p.Up
			out[i].Down += p.Down
		}
	}

	usage, err := h.Store.SubTrafficByInbound(id, s.PeriodStart-s.PeriodStart%3600)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	type item struct {
		store.InboundUsage
		NodeName string `json:"nodeName"`
		Remark   string `json:"remark"`
		Deleted  bool   `json:"deleted"`
	}
	items := make([]item, 0, len(usage))
	for _, u := range usage {
		it := item{InboundUsage: u}
		if n, err := h.Store.GetNode(u.NodeID); err == nil {
			it.NodeName = n.Name
		}
		if in, err := h.Store.GetInbound(u.InboundID); err == nil {
			it.Remark = in.Remark
		} else {
			it.Deleted = true
		}
		items = append(items, it)
	}
	writeJSON(w, http.StatusOK, map[string]any{"daily": out, "byInbound": items})
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
	nodes, _, err := h.subscriptionNodes(s)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if wantsSharePage(r) {
		h.serveSharePage(w, r, s, nodes)
		return
	}
	format := sub.DetectFormat(r.UserAgent(), r.URL.Query().Get("format"))
	var body []byte
	ctype := "text/yaml; charset=utf-8"
	p := h.subscriptionProfile(s)
	if p == nil {
		p = routing.BuiltinProfile()
	}
	switch format {
	case sub.FormatMihomo:
		body, _, err = h.renderMihomoProfile(p, nodes)
	case sub.FormatSingBox:
		body, _, err = h.renderSingBoxProfile(p, nodes)
		ctype = "application/json; charset=utf-8"
	case sub.FormatShadowrocket:
		body, _, err = h.renderShadowrocketProfile(p, nodes)
		ctype = "text/plain; charset=utf-8"
	default:
		// Base64 无法表达链式代理，带前置的外部节点不输出。
		links := make([]string, 0, len(nodes))
		for _, n := range nodes {
			if n.Dialer == "" && n.Link != "" {
				links = append(links, n.Link)
			}
		}
		body, ctype = sub.Base64(links), "text/plain; charset=utf-8"
	}
	if err != nil {
		h.Log.Warn("render subscription", "sub", s.ID, "format", format, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if r.URL.Query().Get("view") == "1" {
		h.serveViewer(w, r, s, format, body)
		return
	}
	hdr := w.Header()
	hdr.Set("Content-Type", ctype)
	hdr.Set("Cache-Control", "no-store")
	hdr.Set("Subscription-Userinfo", sub.UserInfo(s))
	hdr.Set("Profile-Update-Interval", strconv.Itoa(cfg.UpdateHours))
	filename := "x-hub-" + s.Name
	if format == sub.FormatShadowrocket || r.URL.Query().Get("download") == "1" {
		filename += formatExt[format] // 分享页下载与 Shadowrocket 配置带扩展名，便于识别文件类型
	}
	hdr.Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(filename))
	h.Log.Info("subscription fetched", "sub", s.ID, "format", format, "nodes", len(nodes), "ip", h.clientIP(r))
	w.Write(body)
}

// 各格式的文件扩展名与在分享页中的名称。
var (
	formatExt  = map[sub.Format]string{sub.FormatMihomo: ".yaml", sub.FormatSingBox: ".json", sub.FormatShadowrocket: ".conf", sub.FormatBase64: ".txt"}
	formatName = map[sub.Format]string{sub.FormatMihomo: "Mihomo", sub.FormatSingBox: "sing-box", sub.FormatShadowrocket: "Shadowrocket 分流配置", sub.FormatBase64: "通用（节点链接）"}
)

// serveViewer 在浏览器中显示订阅内容；通用格式解码为每行一条分享链接。
func (h *Handler) serveViewer(w http.ResponseWriter, r *http.Request, s *store.Subscription, format sub.Format, body []byte) {
	content := string(body)
	if format == sub.FormatBase64 {
		if dec, err := base64.StdEncoding.DecodeString(content); err == nil {
			content = string(dec)
		}
	}
	setPageHeaders(w)
	err := share.RenderViewer(w, &share.Viewer{
		Name: s.Name, Format: formatName[format], Content: content,
		Download: r.URL.Path + "?format=" + string(format) + "&download=1", Back: r.URL.Path + "?format=page",
	})
	if err != nil {
		h.Log.Warn("render subscription viewer", "sub", s.ID, "err", err)
	}
}

// setPageHeaders 设置分享页与查看页的响应头：禁止缓存与收录，只允许内联样式与脚本。
func setPageHeaders(w http.ResponseWriter) {
	hdr := w.Header()
	hdr.Set("Content-Type", "text/html; charset=utf-8")
	hdr.Set("Cache-Control", "no-store")
	hdr.Set("X-Robots-Tag", "noindex, nofollow")
	hdr.Set("Referrer-Policy", "no-referrer")
	hdr.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; img-src data:; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
}

// baseClientKeywords 为只识别为通用格式、但不应收到分享页的客户端关键字（小写）。
var baseClientKeywords = []string{"shadowrocket", "v2ray", "nekobox", "nekoray", "hiddify", "quantumult", "surge", "loon", "karing"}

// wantsSharePage 判断是否返回分享页：显式 ?format=page，或浏览器直接打开订阅链接
// （Accept 含 text/html、User-Agent 为浏览器且不是已知的代理客户端）。显式指定其他格式时不返回分享页。
func wantsSharePage(r *http.Request) bool {
	if f := r.URL.Query().Get("format"); f != "" {
		return f == "page"
	}
	ua := strings.ToLower(r.UserAgent())
	if !strings.Contains(r.Header.Get("Accept"), "text/html") || !strings.Contains(ua, "mozilla/") {
		return false
	}
	if sub.DetectFormat(ua, "") != sub.FormatBase64 {
		return false
	}
	for _, k := range baseClientKeywords {
		if strings.Contains(ua, k) {
			return false
		}
	}
	return true
}

// serveSharePage 输出订阅分享页：用量、到期、各格式订阅内容的查看与下载入口、节点列表。
func (h *Handler) serveSharePage(w http.ResponseWriter, r *http.Request, s *store.Subscription, nodes []outNode) {
	link := h.subscriptionURL(s.Token)
	if link == "" {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		link = scheme + "://" + r.Host + r.URL.Path
	}
	p := &share.Page{
		Name: s.Name, Status: "正常使用", StatusTone: "ok", SubURL: link, QR: share.QRCode(link),
		Formats: share.Formats(link), UpdateHours: h.Settings.Subscription().UpdateHours,
		Used: share.HumanBytes(s.UsedUp + s.UsedDown), Expire: "永不", UsageTone: "",
	}
	switch s.State {
	case store.SubExceeded:
		p.Status, p.StatusTone = "流量已用完，暂停使用", "bad"
	case store.SubExpired:
		p.Status, p.StatusTone = "已到期，暂停使用", "bad"
	}
	if s.LimitBytes > 0 {
		p.Limit = share.HumanBytes(s.LimitBytes)
		p.Percent = min(100, float64(s.UsedUp+s.UsedDown)*100/float64(s.LimitBytes))
		switch {
		case p.Percent >= 100:
			p.UsageTone = "bad"
		case p.Percent >= 80:
			p.UsageTone = "warn"
		}
	}
	if s.ResetDay > 0 {
		p.Reset = fmt.Sprintf("每月 %d 日重置", s.ResetDay)
	}
	if s.ExpireAt > 0 {
		at := time.Unix(s.ExpireAt, 0).In(h.Settings.Location())
		days := int(time.Until(at).Hours()/24 + 0.999)
		p.Expire = at.Format("2006-01-02")
		switch {
		case days <= 0:
			p.Expire, p.ExpireTone = p.Expire+"（已到期）", "bad"
		case days <= 7:
			p.Expire, p.ExpireTone = fmt.Sprintf("%s（剩 %d 天）", p.Expire, days), "warn"
		default:
			p.Expire = fmt.Sprintf("%s（剩 %d 天）", p.Expire, days)
		}
	}
	for _, n := range nodes {
		p.Nodes = append(p.Nodes, share.NodeItem{Name: n.Name, ClashOnly: n.Dialer != "" || n.Link == ""})
	}
	setPageHeaders(w)
	if err := share.Render(w, p); err != nil {
		h.Log.Warn("render share page", "sub", s.ID, "err", err)
	}
	h.Log.Info("share page viewed", "sub", s.ID, "ip", h.clientIP(r))
}
