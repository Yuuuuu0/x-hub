package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/Yuuuuu0/x-hub/internal/master/routing"
	"github.com/Yuuuuu0/x-hub/internal/master/store"
)

// ruleSetRefs 返回规则集库中各规则集的下载信息：设置了公开访问地址时统一指向主控（托管的直接提供，
// 外部的由主控拉取后中转），客户端无需能访问规则集的原始地址；未设置时外部规则集保持原链接。
func (h *Handler) ruleSetRefs() (map[string]routing.RuleSetRef, error) {
	list, err := h.Store.ListRuleSets()
	if err != nil {
		return nil, err
	}
	out := make(map[string]routing.RuleSetRef, len(list))
	for _, rs := range list {
		ref := routing.RuleSetRef{Name: rs.Name, Behavior: rs.Behavior, URL: rs.URL, Format: rs.Format, Interval: rs.Interval}
		if rs.Source == routing.SetHosted {
			ref.URL, ref.Format = h.hostedRuleURL(rs.Name, ".yaml"), ""
		} else if u := h.hostedRuleURL(rs.Name, mihomoRuleExt[rs.Format]); u != "" {
			ref.URL = u
		}
		if rs.Format != "mrs" {
			ref.SingBoxURL = h.hostedRuleURL(rs.Name, ".json")
			ref.ShadowrocketURL = h.hostedRuleURL(rs.Name, ".list")
		}
		out[rs.Name] = ref
	}
	return out, nil
}

// mihomoRuleExt 为外部规则集按格式中转时使用的扩展名。
var mihomoRuleExt = map[string]string{"": ".yaml", "yaml": ".yaml", "text": ".txt", "mrs": ".mrs"}

// ruleBaseURL 返回主控提供规则集的目录地址（以 / 结尾），未设置公开访问地址时为空。
func (h *Handler) ruleBaseURL() string {
	base := h.Settings.Access().PublicURL
	if base == "" {
		return ""
	}
	return base + "/" + h.Settings.Subscription().Path + "/rules/" + h.Settings.RulesToken() + "/"
}

// hostedRuleURL 返回主控提供的规则集地址：.yaml / .txt / .mrs 为 Mihomo 格式（托管规则集或中转的外部规则集），
// .json / .list 为转换后的 sing-box / Shadowrocket 格式。未设置公开访问地址时为空。
func (h *Handler) hostedRuleURL(name, ext string) string {
	base := h.ruleBaseURL()
	if base == "" {
		return ""
	}
	return base + url.PathEscape(name) + ext
}

// geoRuleBase 返回主控中转 sing-box geosite / geoip 规则集的目录地址，未设置公开访问地址时为空。
func (h *Handler) geoRuleBase() string {
	base := h.ruleBaseURL()
	if base == "" {
		return ""
	}
	return base + "geo/"
}

func (h *Handler) ruleSetNames() (map[string]bool, error) {
	list, err := h.Store.ListRuleSets()
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, rs := range list {
		out[rs.Name] = true
	}
	return out, nil
}

// parseProfile 解析并校验方案，返回规范化后的 JSON。
func (h *Handler) parseProfile(raw json.RawMessage) (*routing.Profile, []byte, []string, error) {
	var p routing.Profile
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, nil, nil, errors.New("方案格式错误")
	}
	names, err := h.ruleSetNames()
	if err != nil {
		return nil, nil, nil, err
	}
	issues := p.Validate(names)
	b, _ := json.Marshal(p)
	return &p, b, issues, nil
}

type profileView struct {
	*store.Profile
	Users []string `json:"users"`
}

func (h *Handler) listProfiles(w http.ResponseWriter, r *http.Request) {
	list, err := h.Store.ListProfiles()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]profileView, 0, len(list))
	for _, p := range list {
		users, _ := h.Store.ProfileUsers(p.ID)
		if users == nil {
			users = []string{}
		}
		out = append(out, profileView{Profile: p, Users: users})
	}
	writeJSON(w, http.StatusOK, map[string]any{"profiles": out})
}

func (h *Handler) getProfile(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	p, err := h.Store.GetProfile(id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

type profileInput struct {
	Name string          `json:"name"`
	Note string          `json:"note"`
	Spec json.RawMessage `json:"spec"`
}

func (in *profileInput) validate() error {
	in.Name, in.Note = strings.TrimSpace(in.Name), strings.TrimSpace(in.Note)
	if n := utf8.RuneCountInString(in.Name); n < 1 || n > 32 {
		return errors.New("名称长度需在 1 到 32 之间")
	}
	if utf8.RuneCountInString(in.Note) > 200 {
		return errors.New("备注不能超过 200 字")
	}
	return nil
}

// writeIssues 以 400 返回方案校验问题列表。
func writeIssues(w http.ResponseWriter, issues []string) {
	writeJSON(w, http.StatusBadRequest, map[string]any{"error": "方案有 " + fmt.Sprint(len(issues)) + " 处问题：" + issues[0], "issues": issues})
}

// profileDefaults 返回各客户端基础配置的默认模板，供编辑器填入。
func (h *Handler) profileDefaults(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"singbox": routing.DefaultSingBoxBase, "shadowrocket": routing.DefaultShadowrocketBase})
}

// createProfile 新建方案；未提供内容时以默认方案为起点。
func (h *Handler) createProfile(w http.ResponseWriter, r *http.Request) {
	var in profileInput
	if err := readJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := in.validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(in.Spec) == 0 || string(in.Spec) == "null" {
		in.Spec, _ = json.Marshal(routing.DefaultProfile())
	}
	_, spec, issues, err := h.parseProfile(in.Spec)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(issues) > 0 {
		writeIssues(w, issues)
		return
	}
	id, err := h.Store.CreateProfile(in.Name, in.Note, spec)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	p, _ := h.Store.GetProfile(id)
	writeJSON(w, http.StatusCreated, p)
}

func (h *Handler) updateProfile(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in profileInput
	if err := readJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := in.validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	_, spec, issues, err := h.parseProfile(in.Spec)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(issues) > 0 {
		writeIssues(w, issues)
		return
	}
	if err := h.Store.UpdateProfile(id, in.Name, in.Note, spec); err != nil {
		writeStoreError(w, err)
		return
	}
	p, _ := h.Store.GetProfile(id)
	writeJSON(w, http.StatusOK, p)
}

func (h *Handler) deleteProfile(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	users, err := h.Store.DeleteProfile(id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if len(users) > 0 {
		writeError(w, http.StatusConflict, "以下订阅正在使用该方案，请先为它们更换方案："+strings.Join(users, "、"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// importProfile 从现有 Mihomo 配置导入方案：规则集入库（同名且来源相同则复用，冲突时自动改名），
// 方案即使有问题也先保存，返回问题与导入说明供用户在编辑器中修正。
func (h *Handler) importProfile(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
		YAML string `json:"yaml"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20)).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	meta := profileInput{Name: in.Name}
	if err := meta.validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	res, err := routing.ImportMihomo(in.YAML)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	notes := res.Notes
	for _, set := range res.Sets {
		name, reused, err := h.importRuleSet(set)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		switch {
		case reused:
			notes = append(notes, fmt.Sprintf("规则集 %s 已存在且来源相同，直接复用", name))
		case name != set.Name:
			res.Profile.RenameRuleSet(set.Name, name)
			notes = append(notes, fmt.Sprintf("规则集 %s 与已有规则集同名但来源不同，已导入为 %s", set.Name, name))
		}
	}
	raw, _ := json.Marshal(res.Profile)
	_, spec, issues, err := h.parseProfile(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	id, err := h.Store.CreateProfile(meta.Name, "", spec)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if issues == nil {
		issues = []string{}
	}
	if notes == nil {
		notes = []string{}
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "notes": notes, "issues": issues})
}

// importRuleSet 把导入的规则集写入库，返回最终名称与是否复用了已有规则集。
func (h *Handler) importRuleSet(set routing.ImportedSet) (string, bool, error) {
	name := set.Name
	for i := 2; ; i++ {
		existing, err := h.Store.RuleSetByName(name)
		if errors.Is(err, store.ErrNotFound) {
			break
		}
		if err != nil {
			return "", false, err
		}
		if existing.Source == set.Source && existing.URL == set.URL && existing.Content == set.Content && existing.Behavior == set.Behavior {
			return name, true, nil
		}
		name = fmt.Sprintf("%s-%d", set.Name, i)
	}
	_, err := h.Store.CreateRuleSet(&store.RuleSet{
		Name: name, Behavior: set.Behavior, Source: set.Source, URL: set.URL,
		Format: set.Format, Interval: set.Interval, Content: set.Content,
	})
	return name, false, err
}

// previewProfile 以某个订阅的节点渲染方案（可传入编辑器中尚未保存的内容），返回配置与提示。
func (h *Handler) previewProfile(w http.ResponseWriter, r *http.Request) {
	var in struct {
		SubscriptionID int64           `json:"subscriptionId"`
		Client         string          `json:"client"` // mihomo（默认）、singbox 或 shadowrocket
		Spec           json.RawMessage `json:"spec"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20)).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	p, _, issues, err := h.parseProfile(in.Spec)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if issues == nil {
		issues = []string{}
	}
	if len(issues) > 0 {
		writeJSON(w, http.StatusOK, map[string]any{"issues": issues, "warnings": []string{}, "config": ""})
		return
	}
	s, err := h.Store.GetSubscription(in.SubscriptionID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "请选择用于预览的订阅")
		return
	}
	nodes, warnings, err := h.subscriptionNodes(s)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	render := h.renderMihomoProfile
	switch in.Client {
	case "singbox":
		render = h.renderSingBoxProfile
	case "shadowrocket":
		render = h.renderShadowrocketProfile
	}
	out, renderWarnings, err := render(p, nodes)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	warnings = append(warnings, renderWarnings...)
	if warnings == nil {
		warnings = []string{}
	}
	type nodeInfo struct {
		Name string `json:"name"`
		Own  bool   `json:"own"`
	}
	infos := make([]nodeInfo, len(nodes))
	for i, n := range nodes {
		infos[i] = nodeInfo{n.Name, n.Own}
	}
	members, _ := p.ExpandGroups(routingNodes(nodes))
	writeJSON(w, http.StatusOK, map[string]any{"issues": issues, "warnings": warnings, "config": string(out), "nodes": infos, "members": members})
}

// renderMihomoProfile 以订阅可用的节点渲染方案。
func (h *Handler) renderMihomoProfile(p *routing.Profile, nodes []outNode) ([]byte, []string, error) {
	refs, err := h.ruleSetRefs()
	if err != nil {
		return nil, nil, err
	}
	for _, name := range p.RuleSetNames() {
		if ref, ok := refs[name]; ok && ref.URL == "" {
			return nil, nil, errors.New("使用托管规则集需要先在「设置 → 访问」中填写公开访问地址")
		}
	}
	return p.RenderMihomo(routingNodes(nodes), refs)
}

// subscriptionProfile 返回订阅选用的方案，未选用或方案已失效时返回 nil。
func (h *Handler) subscriptionProfile(s *store.Subscription) *routing.Profile {
	if s.ProfileID == 0 {
		return nil
	}
	sp, err := h.Store.GetProfile(s.ProfileID)
	if err != nil {
		return nil
	}
	var p routing.Profile
	if json.Unmarshal(sp.Spec, &p) != nil {
		return nil
	}
	return &p
}

var ruleSetNameRe = regexp.MustCompile(`^[\p{L}\p{N}_.-]{1,64}$`)

type ruleSetView struct {
	*store.RuleSet
	HostedURL string   `json:"hostedUrl"`
	Rules     int      `json:"rules"`
	Profiles  []string `json:"profiles"`
}

// ruleSetUsage 返回各规则集被哪些方案引用。
func (h *Handler) ruleSetUsage() (map[string][]string, error) {
	list, err := h.Store.ListProfiles()
	if err != nil {
		return nil, err
	}
	out := map[string][]string{}
	for _, sp := range list {
		var p routing.Profile
		if json.Unmarshal(sp.Spec, &p) != nil {
			continue
		}
		for _, n := range p.RuleSetNames() {
			out[n] = append(out[n], sp.Name)
		}
	}
	return out, nil
}

func (h *Handler) listRuleSets(w http.ResponseWriter, r *http.Request) {
	list, err := h.Store.ListRuleSets()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	usage, err := h.ruleSetUsage()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]ruleSetView, 0, len(list))
	for _, rs := range list {
		v := ruleSetView{RuleSet: rs, Profiles: usage[rs.Name]}
		if v.Profiles == nil {
			v.Profiles = []string{}
		}
		if rs.Source == routing.SetHosted {
			v.HostedURL, v.Rules = h.hostedRuleURL(rs.Name, ".yaml"), len(routing.RuleLines(rs.Content))
		}
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ruleSets": out})
}

func (h *Handler) validRuleSet(rs *store.RuleSet) error {
	rs.Name, rs.URL, rs.Note = strings.TrimSpace(rs.Name), strings.TrimSpace(rs.URL), strings.TrimSpace(rs.Note)
	if !ruleSetNameRe.MatchString(rs.Name) {
		return errors.New("名称需为 1 到 64 位文字、数字、- _ .")
	}
	if !slices.Contains(routing.SetBehaviors, rs.Behavior) {
		return errors.New("行为需为 domain、ipcidr 或 classical")
	}
	if rs.Interval == 0 {
		rs.Interval = 86400
	}
	if rs.Interval < 600 || rs.Interval > 7*86400 {
		return errors.New("更新间隔需在 600 秒到 7 天之间")
	}
	switch rs.Source {
	case routing.SetHosted:
		rs.URL, rs.Format = "", ""
		if len(rs.Content) > 2<<20 {
			return errors.New("规则内容不能超过 2MB")
		}
	case routing.SetURL:
		rs.Content = ""
		if u, err := url.Parse(rs.URL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return errors.New("请填写 http(s) 开头的规则集链接")
		}
		if !slices.Contains([]string{"", "yaml", "text", "mrs"}, rs.Format) {
			return errors.New("格式需为 yaml、text 或 mrs")
		}
	default:
		return errors.New("来源需为托管或外部链接")
	}
	return nil
}

func (h *Handler) createRuleSet(w http.ResponseWriter, r *http.Request) {
	var rs store.RuleSet
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 3<<20)).Decode(&rs); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if err := h.validRuleSet(&rs); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	id, err := h.Store.CreateRuleSet(&rs)
	if err != nil {
		writeRuleSetError(w, err)
		return
	}
	saved, _ := h.Store.GetRuleSet(id)
	writeJSON(w, http.StatusCreated, saved)
}

// updateRuleSet 更新规则集；改名时同步更新引用它的方案。
func (h *Handler) updateRuleSet(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	old, err := h.Store.GetRuleSet(id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	var rs store.RuleSet
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 3<<20)).Decode(&rs); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	rs.ID = id
	if err := h.validRuleSet(&rs); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.Store.UpdateRuleSet(&rs); err != nil {
		writeRuleSetError(w, err)
		return
	}
	if rs.Name != old.Name {
		profiles, _ := h.Store.ListProfiles()
		for _, sp := range profiles {
			var p routing.Profile
			if json.Unmarshal(sp.Spec, &p) == nil && p.RenameRuleSet(old.Name, rs.Name) {
				b, _ := json.Marshal(p)
				h.Store.UpdateProfile(sp.ID, sp.Name, sp.Note, b)
			}
		}
	}
	saved, _ := h.Store.GetRuleSet(id)
	writeJSON(w, http.StatusOK, saved)
}

func (h *Handler) deleteRuleSet(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	rs, err := h.Store.GetRuleSet(id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	usage, err := h.ruleSetUsage()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if users := usage[rs.Name]; len(users) > 0 {
		writeError(w, http.StatusConflict, "以下方案仍在使用该规则集："+strings.Join(users, "、"))
		return
	}
	if err := h.Store.DeleteRuleSet(id); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeRuleSetError(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNameConflict) {
		writeError(w, http.StatusConflict, "已有同名规则集")
		return
	}
	writeStoreError(w, err)
}

// serveHostedRuleSet 对外提供规则集：rules/<令牌>/<名称>.yaml 为托管规则集的 Mihomo 格式，外部规则集按格式以
// .yaml / .txt / .mrs 原样中转；<名称>.json / <名称>.list 为 sing-box / Shadowrocket 格式；geo/<标签>.srs 为中转的
// SagerNet 规则集。
func (h *Handler) serveHostedRuleSet(w http.ResponseWriter, r *http.Request, rest string) {
	token, file, ok := strings.Cut(rest, "/")
	if !ok || token != h.Settings.RulesToken() {
		http.NotFound(w, r)
		return
	}
	if geo, isGeo := strings.CutPrefix(file, "geo/"); isGeo {
		tag, found := strings.CutSuffix(geo, ".srs")
		if !found {
			http.NotFound(w, r)
			return
		}
		h.serveGeoRuleSet(w, r, tag)
		return
	}
	name, ext := file, ""
	for _, e := range []string{".yaml", ".txt", ".mrs", ".json", ".list"} {
		if n, found := strings.CutSuffix(file, e); found {
			name, ext = n, e
		}
	}
	name, err := url.PathUnescape(name)
	if err != nil || ext == "" {
		http.NotFound(w, r)
		return
	}
	rs, err := h.Store.RuleSetByName(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	switch {
	case ext == ".json" || ext == ".list":
		h.serveConvertedRuleSet(w, r, rs, ext)
	case rs.Source != routing.SetHosted:
		h.serveRelayedRuleSet(w, r, rs)
	case ext == ".yaml":
		w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
		w.Write(routing.HostedMihomo(rs.Content))
	default:
		http.NotFound(w, r)
	}
}
