package xray

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	handlercmd "github.com/xtls/xray-core/app/proxyman/command"
	xproto "github.com/xtls/xray-core/common/protocol"
	"github.com/xtls/xray-core/common/serial"
	"github.com/xtls/xray-core/proxy/shadowsocks_2022"
	"github.com/xtls/xray-core/proxy/trojan"
	"github.com/xtls/xray-core/proxy/vless"
	"github.com/xtls/xray-core/proxy/vmess"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// userOp 为一次用户增删操作。Add 非空表示添加，否则按 Remove（email）删除。
type userOp struct {
	Tag      string
	Protocol string
	Add      *client
	Remove   string
}

// client 为各协议客户端字段的并集：VLESS / VMess 用 ID，Trojan / SS2022 用 Password。
type client struct {
	Email    string `json:"email"`
	ID       string `json:"id"`
	Flow     string `json:"flow"`
	Password string `json:"password"`
}

type inboundUsers struct {
	protocol string
	clients  map[string]client
}

// onlineProtocols 为支持经 HandlerService 在线增删用户的入站协议。
var onlineProtocols = map[string]bool{"vless": true, "vmess": true, "trojan": true, "shadowsocks": true}

// usersOnlyDiff 判断新旧主控配置是否只有入站 clients 不同。是则返回所需的用户增删操作（可能为空）；
// 否则返回 ok=false，由调用方重启 Xray。
func usersOnlyDiff(oldCfg, newCfg []byte) (ops []userOp, ok bool) {
	oldRest, oldUsers, err1 := splitClients(oldCfg)
	newRest, newUsers, err2 := splitClients(newCfg)
	if err1 != nil || err2 != nil || !bytes.Equal(oldRest, newRest) {
		return nil, false
	}
	for tag, nu := range newUsers {
		ou := oldUsers[tag]
		var tagOps []userOp
		for email, oc := range ou.clients {
			if nc, keep := nu.clients[email]; !keep || nc != oc {
				tagOps = append(tagOps, userOp{Tag: tag, Protocol: nu.protocol, Remove: email})
			}
		}
		for email, nc := range nu.clients {
			if oc, had := ou.clients[email]; !had || oc != nc {
				c := nc
				tagOps = append(tagOps, userOp{Tag: tag, Protocol: nu.protocol, Add: &c})
			}
		}
		if len(tagOps) > 0 && !onlineProtocols[nu.protocol] {
			return nil, false
		}
		ops = append(ops, tagOps...)
	}
	return ops, true
}

// splitClients 拆出各入站的 clients，返回去掉 clients 后的规范化配置与按 tag 索引的用户。
func splitClients(cfg []byte) ([]byte, map[string]inboundUsers, error) {
	var c map[string]any
	if err := json.Unmarshal(cfg, &c); err != nil {
		return nil, nil, err
	}
	users := map[string]inboundUsers{}
	inbounds, _ := c["inbounds"].([]any)
	for _, raw := range inbounds {
		in, _ := raw.(map[string]any)
		settings, _ := in["settings"].(map[string]any)
		tag, _ := in["tag"].(string)
		proto, _ := in["protocol"].(string)
		if settings == nil || tag == "" {
			continue
		}
		iu := inboundUsers{protocol: proto, clients: map[string]client{}}
		b, _ := json.Marshal(settings["clients"])
		var list []client
		if err := json.Unmarshal(b, &list); err != nil {
			return nil, nil, err
		}
		for _, u := range list {
			iu.clients[u.Email] = u
		}
		users[tag] = iu
		delete(settings, "clients")
	}
	rest, err := json.Marshal(c)
	return rest, users, err
}

// alterUsers 通过 Xray HandlerService 在线增删用户，先删后加。
func (m *Manager) alterUsers(ops []userOp) error {
	conn, err := grpc.NewClient(fmt.Sprintf("127.0.0.1:%d", m.st.APIPort), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	defer conn.Close()
	client := handlercmd.NewHandlerServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, add := range []bool{false, true} {
		for _, op := range ops {
			if (op.Add != nil) != add {
				continue
			}
			var operation *serial.TypedMessage
			if op.Add != nil {
				operation = serial.ToTypedMessage(&handlercmd.AddUserOperation{User: &xproto.User{
					Email:   op.Add.Email,
					Account: account(op.Protocol, op.Add),
				}})
			} else {
				operation = serial.ToTypedMessage(&handlercmd.RemoveUserOperation{Email: op.Remove})
			}
			if _, err := client.AlterInbound(ctx, &handlercmd.AlterInboundRequest{Tag: op.Tag, Operation: operation}); err != nil {
				return fmt.Errorf("%s %s: %w", op.Tag, map[bool]string{true: "add", false: "remove"}[add], err)
			}
		}
	}
	return nil
}

// account 按协议构造与 Xray 配置解析结果一致的账户。
func account(protocol string, c *client) *serial.TypedMessage {
	switch protocol {
	case "vmess":
		return serial.ToTypedMessage(&vmess.Account{Id: c.ID, SecuritySettings: &xproto.SecurityConfig{Type: xproto.SecurityType_AUTO}})
	case "trojan":
		return serial.ToTypedMessage(&trojan.Account{Password: c.Password})
	case "shadowsocks":
		return serial.ToTypedMessage(&shadowsocks_2022.Account{Key: c.Password})
	default:
		return serial.ToTypedMessage(&vless.Account{Id: c.ID, Flow: c.Flow})
	}
}
