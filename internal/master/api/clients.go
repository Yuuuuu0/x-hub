package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/Yuuuuu0/x-hub/internal/master/routing"
	"github.com/Yuuuuu0/x-hub/internal/master/store"
)

// ruleSetCache 缓存外部规则集拉取后的条目，按规则集的更新间隔过期。
type ruleSetCache struct {
	mu    sync.Mutex
	items map[int64]cachedRuleSet
}

type cachedRuleSet struct {
	url, format string
	at          time.Time
	lines       []string
}

var ruleClient = &http.Client{Timeout: 30 * time.Second}

// errNoPublicURL 表示输出引用了规则集库，但规则集需经主控转换，必须有公开访问地址。
var errNoPublicURL = errors.New("使用规则集库中的规则集需要先在「设置 → 访问」中填写公开访问地址")

// serveConvertedRuleSet 输出规则集的 sing-box（.json）或 Shadowrocket（.list）格式。托管规则集直接转换；
// 外部规则集由主控拉取后转换并缓存，拉取失败时使用缓存中的旧内容。
func (h *Handler) serveConvertedRuleSet(w http.ResponseWriter, r *http.Request, rs *store.RuleSet, ext string) {
	lines := routing.RuleLines(rs.Content)
	if rs.Source != routing.SetHosted {
		var err error
		if lines, err = h.ruleSetLines(r.Context(), rs); err != nil {
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

func (h *Handler) ruleSetLines(ctx context.Context, rs *store.RuleSet) ([]string, error) {
	c := &h.ruleSets
	c.mu.Lock()
	cached, ok := c.items[rs.ID]
	c.mu.Unlock()
	fresh := ok && cached.url == rs.URL && cached.format == rs.Format && time.Since(cached.at) < time.Duration(rs.Interval)*time.Second
	if fresh {
		return cached.lines, nil
	}
	lines, err := fetchRuleSet(ctx, rs)
	if err != nil {
		if ok && cached.url == rs.URL {
			return cached.lines, nil
		}
		return nil, err
	}
	c.mu.Lock()
	if c.items == nil {
		c.items = map[int64]cachedRuleSet{}
	}
	c.items[rs.ID] = cachedRuleSet{url: rs.URL, format: rs.Format, at: time.Now(), lines: lines}
	c.mu.Unlock()
	return lines, nil
}

func fetchRuleSet(ctx context.Context, rs *store.RuleSet) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rs.URL, nil)
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
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 20<<20))
	if err != nil {
		return nil, err
	}
	return routing.ParseMihomoRuleSet(raw, rs.Format)
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
	return p.RenderSingBox(routingNodes(nodes), refs)
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
