package xray

import (
	"context"
	"fmt"
	"strings"
	"time"

	statscmd "github.com/xtls/xray-core/app/stats/command"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/Yuuuuu0/x-hub/internal/shared/protocol"
)

// SetBeforeStop 设置停止 Xray 前的回调。
func (m *Manager) SetBeforeStop(fn func()) { m.beforeStop = fn }

func (m *Manager) statsClient() (statscmd.StatsServiceClient, func(), error) {
	conn, err := grpc.NewClient(fmt.Sprintf("127.0.0.1:%d", m.st.APIPort), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, nil, err
	}
	return statscmd.NewStatsServiceClient(conn), func() { conn.Close() }, nil
}

// QueryUserTraffic 读取并清零各用户的流量计数，返回 email → [上行, 下行]。Xray 未运行时返回空。
func (m *Manager) QueryUserTraffic() (map[string][2]int64, error) {
	if !m.running() {
		return nil, nil
	}
	client, closeFn, err := m.statsClient()
	if err != nil {
		return nil, err
	}
	defer closeFn()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := client.QueryStats(ctx, &statscmd.QueryStatsRequest{Pattern: "user>>>", Reset_: true})
	if err != nil {
		return nil, err
	}
	out := map[string][2]int64{}
	for _, st := range resp.Stat {
		// 名称形如 user>>>s1.i2>>>traffic>>>uplink
		parts := strings.Split(st.Name, ">>>")
		if len(parts) != 4 || parts[2] != "traffic" || st.Value == 0 {
			continue
		}
		v := out[parts[1]]
		if parts[3] == "uplink" {
			v[0] += st.Value
		} else {
			v[1] += st.Value
		}
		out[parts[1]] = v
	}
	return out, nil
}

// OnlineIPs 返回当前在线用户的来源 IP（email → IP 列表），Xray 不支持在线统计时返回空。
func (m *Manager) OnlineIPs() map[string][]string {
	if !m.running() || !hasCap(m.Caps(), protocol.CapOnlineStats) {
		return nil
	}
	client, closeFn, err := m.statsClient()
	if err != nil {
		return nil
	}
	defer closeFn()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	users, err := client.GetAllOnlineUsers(ctx, &statscmd.GetAllOnlineUsersRequest{})
	if err != nil {
		return nil
	}
	out := map[string][]string{}
	for _, name := range users.Users {
		parts := strings.Split(name, ">>>")
		if len(parts) != 3 {
			continue
		}
		resp, err := client.GetStatsOnlineIpList(ctx, &statscmd.GetStatsRequest{Name: name})
		if err != nil {
			continue
		}
		for ip := range resp.Ips {
			out[parts[1]] = append(out[parts[1]], ip)
		}
	}
	return out
}

func hasCap(caps []string, c string) bool {
	for _, x := range caps {
		if x == c {
			return true
		}
	}
	return false
}
