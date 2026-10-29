// Package compiler 把数据库中的入站与订阅组装为某台服务器的完整 Xray 配置。
package compiler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/Yuuuuu0/x-hub/internal/master/model"
	"github.com/Yuuuuu0/x-hub/internal/master/store"
)

// Node 生成服务器的 Xray 配置及其哈希。配置不含被控端自管的 api / stats / policy 段。
// 只编译已启用的入站；每个入站的 clients 为可使用它的有效订阅。
func Node(st *store.Store, nodeID int64) (json.RawMessage, string, error) {
	inbounds, err := st.ListNodeInbounds(nodeID)
	if err != nil {
		return nil, "", err
	}
	xin := make([]any, 0, len(inbounds))
	for _, in := range inbounds {
		if !in.Enabled {
			continue
		}
		spec, err := model.ParseSpec(in.Protocol, in.Spec)
		if err != nil {
			return nil, "", fmt.Errorf("入站 %d：%w", in.ID, err)
		}
		subs, err := st.InboundSubscriptions(in.ID)
		if err != nil {
			return nil, "", err
		}
		clients := make([]model.Client, 0, len(subs))
		for _, s := range subs {
			clients = append(clients, model.Client{Email: model.ClientEmail(s.ID, in.ID), UUID: s.UUID})
		}
		xin = append(xin, model.XrayInbound(in.ID, in.Listen, in.Port, spec, clients))
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
		return nil, "", err
	}
	sum := sha256.Sum256(raw)
	return raw, hex.EncodeToString(sum[:8]), nil
}
