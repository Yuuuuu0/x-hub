// Package probe 在被控端本机网络执行探测，结果比在主控上探测更贴近真实链路。
package probe

import (
	"context"
	"crypto/tls"
	"net"
	"time"

	"github.com/Yuuuuu0/x-hub/internal/shared/protocol"
)

// RealityTarget 检测 Reality 目标网站：以 serverName 握手，要求证书有效、支持 TLS 1.3，并记录是否协商到 H2。
func RealityTarget(ctx context.Context, target, serverName string) protocol.ProbeTargetResult {
	if serverName == "" {
		serverName, _, _ = net.SplitHostPort(target)
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	d := &tls.Dialer{Config: &tls.Config{ServerName: serverName, NextProtos: []string{"h2", "http/1.1"}}}
	start := time.Now()
	c, err := d.DialContext(ctx, "tcp", target)
	if err != nil {
		return protocol.ProbeTargetResult{Error: err.Error()}
	}
	defer c.Close()
	st := c.(*tls.Conn).ConnectionState()
	r := protocol.ProbeTargetResult{
		TLS13:     st.Version == tls.VersionTLS13,
		H2:        st.NegotiatedProtocol == "h2",
		LatencyMs: time.Since(start).Milliseconds(),
	}
	r.OK = r.TLS13
	if !r.TLS13 {
		r.Error = "目标不支持 TLS 1.3，不适合作为 Reality 目标"
	}
	return r
}
