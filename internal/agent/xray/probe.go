package xray

import (
	"context"
	"fmt"
	"time"

	statscmd "github.com/xtls/xray-core/app/stats/command"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"github.com/Yuuuuu0/x-hub/internal/shared/protocol"
)

// probe 在进程启动后探测运行时能力：调用在线用户 API，Unimplemented 视为不支持。
// API 端口可能尚未就绪，失败时重试，进程退出（done 关闭）则放弃。
func (m *Manager) probe(port int, done <-chan struct{}) {
	conn, err := grpc.NewClient(fmt.Sprintf("127.0.0.1:%d", port), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return
	}
	defer conn.Close()
	client := statscmd.NewStatsServiceClient(conn)
	for range 10 {
		select {
		case <-done:
			return
		case <-time.After(time.Second):
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_, err := client.GetAllOnlineUsers(ctx, &statscmd.GetAllOnlineUsersRequest{})
		cancel()
		var caps []string
		switch status.Code(err) {
		case codes.OK:
			caps = []string{protocol.CapOnlineStats}
		case codes.Unimplemented:
			caps = []string{}
		default:
			continue
		}
		m.mu.Lock()
		m.caps = caps
		m.mu.Unlock()
		m.notify()
		return
	}
	m.log.Warn("xray api probe failed", "port", port)
}
