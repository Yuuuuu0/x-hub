// Package metrics 采集被控主机的静态信息与资源占用。
package metrics

import (
	"runtime"
	"strings"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"

	"github.com/Yuuuuu0/x-hub/internal/shared/protocol"
)

// virtualNICPrefixes 为统计总流量时排除的虚拟网卡前缀，避免容器 / 隧道流量重复计数。
var virtualNICPrefixes = []string{"lo", "docker", "veth", "br-", "virbr", "cni", "flannel", "tun", "tap", "wg"}

// HostInfo 采集主机静态信息。
func HostInfo() protocol.HostInfo {
	info := protocol.HostInfo{Arch: runtime.GOARCH, CPUCores: runtime.NumCPU()}
	if h, err := host.Info(); err == nil {
		info.Hostname, info.OS, info.Platform = h.Hostname, h.OS, h.Platform
		info.PlatformVersion, info.Kernel = h.PlatformVersion, h.KernelVersion
	}
	if cs, err := cpu.Info(); err == nil && len(cs) > 0 {
		info.CPUModel = strings.TrimSpace(cs[0].ModelName)
	}
	return info
}

// Sample 采集一次资源占用。CPU 使用率为距上次调用的平均值，首次调用为 0。
func Sample() protocol.Heartbeat {
	var hb protocol.Heartbeat
	if p, err := cpu.Percent(0, false); err == nil && len(p) > 0 {
		hb.CPU = p[0]
	}
	if v, err := mem.VirtualMemory(); err == nil {
		hb.MemUsed, hb.MemTotal = v.Used, v.Total
	}
	if s, err := mem.SwapMemory(); err == nil {
		hb.SwapUsed, hb.SwapTotal = s.Used, s.Total
	}
	if d, err := disk.Usage("/"); err == nil {
		hb.DiskUsed, hb.DiskTotal = d.Used, d.Total
	}
	if l, err := load.Avg(); err == nil {
		hb.Load1, hb.Load5, hb.Load15 = l.Load1, l.Load5, l.Load15
	}
	if up, err := host.Uptime(); err == nil {
		hb.Uptime = up
	}
	if nics, err := net.IOCounters(true); err == nil {
		for _, n := range nics {
			if isVirtual(n.Name) {
				continue
			}
			hb.NetRx += n.BytesRecv
			hb.NetTx += n.BytesSent
		}
	}
	return hb
}

func isVirtual(name string) bool {
	for _, p := range virtualNICPrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}
