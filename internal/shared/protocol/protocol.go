// Package protocol 定义主控与被控之间 WebSocket 通信的消息信封与载荷结构。
package protocol

import "encoding/json"

// 消息类型。
const (
	TypeHello     = "hello"     // A→M 连接建立后的首条消息
	TypeWelcome   = "welcome"   // M→A 对 hello 的应答
	TypeHeartbeat = "heartbeat" // A→M 周期性状态上报
	TypeResult    = "result"    // A→M 对主控指令的应答
	TypeEndpoint  = "endpoint"  // M→A 主控访问地址变更通知
	TypeXray      = "xray"      // M→A Xray 进程控制
	TypeLogs      = "logs"      // M→A 读取 Xray 最近日志
	TypeApply     = "apply"     // M→A 下发完整 Xray 配置
	TypeProbe     = "probe"     // M→A 在被控端本机执行探测
	TypeTraffic   = "traffic"   // A→M 上报流量账本批次
	TypeAck       = "ack"       // M→A 确认已入库的最大批次序号
)

// 探测类型。
const (
	ProbeRealityTarget = "reality_target" // 检测 Reality 目标网站是否支持 TLS 1.3 / H2
	ProbePort          = "port"           // 检测端口是否可用
)

// Xray 控制动作。
const (
	XrayStart   = "start"
	XrayStop    = "stop"
	XrayRestart = "restart"
	XrayInstall = "install" // 安装或切换到指定版本
)

// Feature 表示被控端 Xray 在运行时探测到的可选能力。
const (
	CapOnlineStats = "online_stats"
)

// Envelope 是所有消息的统一信封。
// 主控发出的指令带 ID，被控端以 TypeResult 应答并在 Re 中回填该 ID。
type Envelope struct {
	T  string          `json:"t"`
	ID string          `json:"id,omitempty"`
	Re string          `json:"re,omitempty"`
	D  json.RawMessage `json:"d,omitempty"`
}

// Endpoint 描述被控端连接主控所用的地址与可选的公钥指纹。
type Endpoint struct {
	URL string `json:"url"`          // 主控公开地址，如 https://hub.example.com:2096
	FP  string `json:"fp,omitempty"` // 自签模式下证书公钥的 SHA-256（base64），为空走系统 CA
}

// HostInfo 为被控主机的静态信息。
type HostInfo struct {
	Hostname        string `json:"hostname"`
	OS              string `json:"os"`
	Platform        string `json:"platform"`
	PlatformVersion string `json:"platformVersion"`
	Kernel          string `json:"kernel"`
	Arch            string `json:"arch"`
	CPUModel        string `json:"cpuModel"`
	CPUCores        int    `json:"cpuCores"`
}

// XrayStatus 为被控端 Xray 进程状态。
type XrayStatus struct {
	Installed bool   `json:"installed"`
	Version   string `json:"version"`
	Enabled   bool   `json:"enabled"` // 期望运行
	Running   bool   `json:"running"`
	StartedAt int64  `json:"startedAt"`
	LastError string `json:"lastError,omitempty"`
}

// Hello 为被控端连接后上报的身份与状态。AppliedRev / AppliedHash 为本地已应用的配置版本。
type Hello struct {
	AgentVersion string     `json:"agentVersion"`
	Host         HostInfo   `json:"host"`
	Xray         XrayStatus `json:"xray"`
	Caps         []string   `json:"caps"`
	AppliedRev   int64      `json:"appliedRev"`
	AppliedHash  string     `json:"appliedHash"`
}

// Apply 为主控下发的完整 Xray 配置（不含被控端自管的 api / stats / policy 段）。
// 被控端忽略 Rev 小于已应用版本的配置。
type Apply struct {
	Rev    int64           `json:"rev"`
	Hash   string          `json:"hash"`
	Config json.RawMessage `json:"config"`
}

// ApplyResult 为配置应用结果。
type ApplyResult struct {
	Rev  int64  `json:"rev"`
	Hash string `json:"hash"`
}

// ProbeRequest 为探测请求。
type ProbeRequest struct {
	Kind       string `json:"kind"`
	Target     string `json:"target,omitempty"`     // reality_target：域名:端口
	ServerName string `json:"serverName,omitempty"` // reality_target：握手使用的 SNI
	Port       int    `json:"port,omitempty"`       // port
}

// ProbeTargetResult 为 Reality 目标网站探测结果。
type ProbeTargetResult struct {
	OK        bool   `json:"ok"`
	TLS13     bool   `json:"tls13"`
	H2        bool   `json:"h2"`
	LatencyMs int64  `json:"latencyMs"`
	Error     string `json:"error,omitempty"`
}

// ProbePortResult 为端口探测结果。Owned 表示端口由当前 Xray 配置中的入站占用。
type ProbePortResult struct {
	Free  bool `json:"free"`
	Owned bool `json:"owned"`
}

// Welcome 为主控对 Hello 的应答。AckSeq 为主控已入库的最大流量批次序号。
type Welcome struct {
	ServerTime int64    `json:"serverTime"`
	Endpoint   Endpoint `json:"endpoint"` // 主控当前推荐的访问地址
	AckSeq     int64    `json:"ackSeq"`
}

// TrafficBatch 为一个小时内累计的流量增量。Users 键为 Xray 用户 email，值为 [上行, 下行] 字节数。
type TrafficBatch struct {
	Seq   int64               `json:"seq"`
	Hour  int64               `json:"hour"` // 整点 Unix 时间戳
	Users map[string][2]int64 `json:"users,omitempty"`
	NICRx int64               `json:"nicRx"`
	NICTx int64               `json:"nicTx"`
}

// Traffic 为流量上报消息，批次按序号递增。
type Traffic struct {
	Batches []TrafficBatch `json:"batches"`
}

// Ack 为主控对流量上报的确认。
type Ack struct {
	Seq int64 `json:"seq"`
}

// Heartbeat 为被控端周期性上报的资源与进程状态。网卡计数为开机以来累计值。
type Heartbeat struct {
	CPU       float64    `json:"cpu"`
	MemUsed   uint64     `json:"memUsed"`
	MemTotal  uint64     `json:"memTotal"`
	SwapUsed  uint64     `json:"swapUsed"`
	SwapTotal uint64     `json:"swapTotal"`
	DiskUsed  uint64     `json:"diskUsed"`
	DiskTotal uint64     `json:"diskTotal"`
	Load1     float64    `json:"load1"`
	Load5     float64    `json:"load5"`
	Load15    float64    `json:"load15"`
	Uptime    uint64     `json:"uptime"`
	NetRx     uint64     `json:"netRx"`
	NetTx     uint64     `json:"netTx"`
	Xray      XrayStatus `json:"xray"`
	Caps      []string   `json:"caps"`
	// Online 为当前在线用户的来源 IP（email → IP 列表），仅支持 online_stats 的 Xray 上报。
	Online map[string][]string `json:"online,omitempty"`
}

// Result 为被控端对指令的执行结果。
type Result struct {
	OK    bool            `json:"ok"`
	Error string          `json:"error,omitempty"`
	Data  json.RawMessage `json:"data,omitempty"`
}

// XrayCommand 为 Xray 控制指令。Install 时 Version 为目标版本（如 v26.7.28）。
type XrayCommand struct {
	Action  string `json:"action"`
	Version string `json:"version,omitempty"`
}

// LogsRequest 请求最近 Lines 行 Xray 日志。
type LogsRequest struct {
	Lines int `json:"lines"`
}

// LogsResponse 为日志查询结果。
type LogsResponse struct {
	Lines []string `json:"lines"`
}

// EnrollRequest 为被控端用一次性令牌换取节点凭据的请求。
type EnrollRequest struct {
	Token    string `json:"token"`
	Hostname string `json:"hostname"`
}

// EnrollResponse 为接入成功后下发的节点凭据。
type EnrollResponse struct {
	NodeID int64  `json:"nodeId"`
	Secret string `json:"secret"`
}

// Marshal 把载荷编码进信封。
func Marshal(t, id, re string, payload any) ([]byte, error) {
	env := Envelope{T: t, ID: id, Re: re}
	if payload != nil {
		d, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		env.D = d
	}
	return json.Marshal(env)
}
