package api

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Yuuuuu0/x-hub/internal/master/routing"
	"github.com/Yuuuuu0/x-hub/internal/master/store"
)

// ruleSetCache 缓存主控中转的外部规则集原文（规则集库中的外部规则集、sing-box 的 geosite / geoip），
// 过期后重新拉取，拉取失败时继续使用旧内容。
type ruleSetCache struct {
	mu    sync.Mutex
	items map[string]cachedRuleSet
}

type cachedRuleSet struct {
	url  string
	at   time.Time
	body []byte
}

var ruleClient = &http.Client{Timeout: 30 * time.Second}

// errNoPublicURL 表示输出引用了规则集库，但规则集需经主控转换，必须有公开访问地址。
var errNoPublicURL = errors.New("使用规则集库中的规则集需要先在「设置 → 访问」中填写公开访问地址")

// geoTagRe 为可中转的 SagerNet 规则集标签。
var geoTagRe = regexp.MustCompile(`^geo(site|ip)-[a-z0-9@!_.-]+$`)

// get 返回 key 对应的规则集原文：缓存未过期时直接返回，否则从 url 拉取；拉取失败且有同地址的旧内容时返回旧内容。
func (c *ruleSetCache) get(ctx context.Context, key, url string, ttl time.Duration) ([]byte, error) {
	c.mu.Lock()
	cached, ok := c.items[key]
	c.mu.Unlock()
	ok = ok && cached.url == url
	if ok && time.Since(cached.at) < ttl {
		return cached.body, nil
	}
	body, err := fetchRuleSet(ctx, url)
	if err != nil {
		if ok {
			return cached.body, nil
		}
		return nil, err
	}
	c.mu.Lock()
	if c.items == nil {
		c.items = map[string]cachedRuleSet{}
	}
	c.items[key] = cachedRuleSet{url: url, at: time.Now(), body: body}
	c.mu.Unlock()
	return body, nil
}

// externalRuleSet 返回外部规则集的原文（经缓存）。
func (h *Handler) externalRuleSet(ctx context.Context, rs *store.RuleSet) ([]byte, error) {
	return h.ruleSets.get(ctx, "set:"+strconv.FormatInt(rs.ID, 10), rs.URL, time.Duration(rs.Interval)*time.Second)
}

// serveRelayedRuleSet 原样输出外部规则集（Mihomo 格式），供 Mihomo 从主控下载。
func (h *Handler) serveRelayedRuleSet(w http.ResponseWriter, r *http.Request, rs *store.RuleSet) {
	body, err := h.externalRuleSet(r.Context(), rs)
	if err != nil {
		h.Log.Warn("relay rule set", "name", rs.Name, "err", err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	switch rs.Format {
	case "mrs":
		w.Header().Set("Content-Type", "application/octet-stream")
	case "text":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	default:
		w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
	}
	w.Write(body)
}

// serveGeoRuleSet 中转 SagerNet 官方的 geosite-* / geoip-* 二进制规则集，供 sing-box 从主控下载，缓存一天。
func (h *Handler) serveGeoRuleSet(w http.ResponseWriter, r *http.Request, tag string) {
	if !geoTagRe.MatchString(tag) {
		http.NotFound(w, r)
		return
	}
	body, err := h.ruleSets.get(r.Context(), "geo:"+tag, routing.GeoRuleSetURL(tag), 24*time.Hour)
	if err != nil {
		h.Log.Warn("relay geo rule set", "tag", tag, "err", err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Write(body)
}

// serveConvertedRuleSet 输出规则集的 sing-box（.json）或 Shadowrocket（.list）格式。托管规则集直接转换；
// 外部规则集由主控拉取（经缓存）后转换。
func (h *Handler) serveConvertedRuleSet(w http.ResponseWriter, r *http.Request, rs *store.RuleSet, ext string) {
	lines := routing.RuleLines(rs.Content)
	if rs.Source != routing.SetHosted {
		body, err := h.externalRuleSet(r.Context(), rs)
		if err == nil {
			lines, err = routing.ParseMihomoRuleSet(body, rs.Format)
		}
		if err != nil {
			h.Log.Warn("convert rule set", "name", rs.Name, "err", err)
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
	}
	if ext == ".json" {
		body, _ := routing.SingBoxRuleSet(rs.Behavior, lines)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Write(body)
		return
	}
	body, _ := routing.ShadowrocketRuleSet(rs.Behavior, lines)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write(body)
}

func fetchRuleSet(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := ruleClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("拉取规则集失败：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("拉取规则集失败：HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 20<<20))
}

// ruleSetCheck 为外部规则集的检测结果。Suggest 为内容与所选格式不一致时建议使用的格式；
// FetchError 非空表示无法拉取，未做检测。
type ruleSetCheck struct {
	Rules      int    `json:"rules"`
	Warning    string `json:"warning,omitempty"`
	Suggest    string `json:"suggest,omitempty"`
	FetchError string `json:"fetchError,omitempty"`
}

// inspectRuleSet 检测规则集内容的实际格式是否与所选格式 format（空表示 yaml）一致，并统计条目数（mrs 无法统计）。
func inspectRuleSet(body []byte, format string) ruleSetCheck {
	detected := routing.DetectRuleSetFormat(body)
	switch detected {
	case "":
		return ruleSetCheck{Warning: "链接返回的内容为空，没有可用的规则。"}
	case "html":
		return ruleSetCheck{Warning: "链接返回的是网页，不是规则集文件。GitHub 上的文件请使用 Raw 链接。"}
	}
	var res ruleSetCheck
	if detected != "mrs" {
		lines, err := routing.ParseMihomoRuleSet(body, detected)
		if err != nil {
			return ruleSetCheck{Warning: err.Error()}
		}
		if res.Rules = len(lines); res.Rules == 0 {
			return ruleSetCheck{Warning: "规则集中没有规则。"}
		}
	}
	if selected := cmp.Or(format, "yaml"); detected != selected {
		res.Suggest = detected
		res.Warning = fmt.Sprintf("链接的内容实际是 %s 格式，当前选择的是 %s。格式不一致时客户端无法正确解析，生效规则数会是 0 或明显偏少。", detected, selected)
	}
	return res
}

// checkRuleSet 拉取外部规则集并检测格式，供保存前提示。拉取失败不算错误，由前端决定是否继续保存。
func (h *Handler) checkRuleSet(w http.ResponseWriter, r *http.Request) {
	var in struct {
		URL    string `json:"url"`
		Format string `json:"format"`
	}
	if err := readJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	body, err := fetchRuleSet(ctx, strings.TrimSpace(in.URL))
	if err != nil {
		writeJSON(w, http.StatusOK, ruleSetCheck{FetchError: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, inspectRuleSet(body, in.Format))
}

// renderSingBoxProfile 以订阅可用的节点把方案渲染为 sing-box 配置。
func (h *Handler) renderSingBoxProfile(p *routing.Profile, nodes []outNode) ([]byte, []string, error) {
	refs, err := h.ruleSetRefs()
	if err != nil {
		return nil, nil, err
	}
	if len(p.RuleSetNames()) > 0 && h.Settings.Access().PublicURL == "" {
		return nil, nil, errNoPublicURL
	}
	return p.RenderSingBox(routingNodes(nodes), refs, h.geoRuleBase())
}

// renderShadowrocketProfile 把方案渲染为 Shadowrocket 配置。分组只引用通用订阅中的节点
// （有分享链接且没有前置），因为节点由同一订阅的通用链接导入。
func (h *Handler) renderShadowrocketProfile(p *routing.Profile, nodes []outNode) ([]byte, []string, error) {
	refs, err := h.ruleSetRefs()
	if err != nil {
		return nil, nil, err
	}
	if len(p.RuleSetNames()) > 0 && h.Settings.Access().PublicURL == "" {
		return nil, nil, errNoPublicURL
	}
	var list []routing.Node
	var skipped []string
	for _, n := range nodes {
		switch {
		case n.Dialer != "":
			skipped = append(skipped, fmt.Sprintf("节点 %s 使用链式代理，通用订阅无法表达，Shadowrocket 中不可用", n.Name))
		case n.Link == "":
			skipped = append(skipped, fmt.Sprintf("节点 %s 的类型无法生成分享链接，Shadowrocket 中不可用", n.Name))
		default:
			list = append(list, n.Node)
		}
	}
	out, warnings, err := p.RenderShadowrocket(list, refs)
	return out, append(skipped, warnings...), err
}
