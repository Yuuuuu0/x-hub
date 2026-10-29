// Package compiler 把数据库中的入站与订阅组装为某台服务器的完整 Xray 配置。
package compiler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/Yuuuuu0/x-hub/internal/master/model"
	"github.com/Yuuuuu0/x-hub/internal/master/store"
)

// Result 为某台服务器的编译结果。
type Result struct {
	Config      json.RawMessage
	Hash        string
	ACMEDomains []string // 需要被控端自动申请证书的域名
}

// Node 生成服务器的 Xray 配置及其哈希。配置不含被控端自管的 api / stats / policy 段。
// 只编译已启用的入站；每个入站的 clients 为可使用它的有效订阅。
func Node(st *store.Store, nodeID int64) (*Result, error) {
	inbounds, err := st.ListNodeInbounds(nodeID)
	if err != nil {
		return nil, err
	}
	res := &Result{}
	xin := make([]any, 0, len(inbounds))
	for _, in := range inbounds {
		if !in.Enabled {
			continue
		}
		spec, err := model.ParseSpec(in.Protocol, in.Spec)
		if err != nil {
			return nil, fmt.Errorf("入站 %d：%w", in.ID, err)
		}
		subs, err := st.InboundSubscriptions(in.ID)
		if err != nil {
			return nil, err
		}
		clients := make([]model.Client, 0, len(subs))
		for _, s := range subs {
			clients = append(clients, model.Client{
				Email: model.ClientEmail(s.ID, in.ID),
				Cred:  model.CredentialFor(in.Protocol, spec, s.UUID, s.Secret, in.ID),
			})
		}
		xin = append(xin, model.XrayInbound(in.ID, in.Listen, in.Port, in.Protocol, spec, clients))
		if d := spec.ACMEDomain(); d != "" && !slices.Contains(res.ACMEDomains, d) {
			res.ACMEDomains = append(res.ACMEDomains, d)
		}
	}
	cfg := map[string]any{
		"log":      map[string]any{"loglevel": "warning", "access": "none"},
		"inbounds": xin,
		"outbounds": []any{
			map[string]any{"tag": "direct", "protocol": "freedom"},
			map[string]any{"tag": "block", "protocol": "blackhole"},
		},
		"routing": map[string]any{
			"domainStrategy": "AsIs",
			// 禁止经代理访问服务器所在内网，避免被用作跳板。
			"rules": []any{map[string]any{"type": "field", "ip": []string{"geoip:private"}, "outboundTag": "block"}},
		},
	}
	// encoding/json 对 map 键排序，输出确定，可直接用于哈希比较。
	raw, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(raw)
	res.Config, res.Hash = raw, hex.EncodeToString(sum[:8])
	return res, nil
}
