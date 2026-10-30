# x-hub 架构设计

> 状态：定稿 v1 · 2026-10-29
> 许可证：MIT

## 1. 产品定位

面向个人自用与亲友轻量分享的**主从分离版 3x-ui**：一台主控（Master）通过 Web 表单集中管理多台 VPS 上的 Xray，被控端（Agent）无 Web 界面、无管理端口。

**目标**

- 一行命令接入被控 VPS，集中查看状态、资源、流量，控制 Xray 启停与版本。
- 纯表单创建/修改入站，主控自动编译 Xray 配置并下发，全程不手写 JSON。
- 多订阅（成员）：每个订阅一条聚合链接，凭据自动注入各节点，按订阅跨服务器统计流量，支持限额、到期、停用。
- 保留单节点分享链接 / 二维码导出。

**非目标**

- 商业计费、套餐、订单、用户注册、工单。
- Inbound/Host/Squad 等多层映射模板。
- 多管理员、高可用、设备数强制限制。

## 2. 术语

| 术语 | 含义 |
|---|---|
| 服务器（Node） | 一台运行 Agent 的 VPS |
| 入站（Inbound） | 某台服务器上的一个 Xray 入站，即用户眼中的"节点" |
| 订阅（Subscription） | 一个成员/使用者，拥有一条聚合订阅链接与一组派生凭据 |

## 3. 总体架构

```
                       ┌──────────────────── Master (xhub-master) ─────────────────────┐
  浏览器 ──HTTPS──▶    │  Vue3 + Ant Design Vue（embed）                                │
                       │  REST API ── Service ── Store(SQLite)                         │
  客户端 ──HTTPS──▶    │  订阅渲染：Base64 / Mihomo / sing-box                          │
                       │  Compiler：领域模型 → Xray JSON / 分享链接 / Clash / sing-box   │
                       │  NodeHub：Agent 长连接、期望版本、指令下发                     │
                       │  Accounting：流量入库、额度/到期判定 → 触发重编译              │
                       └──────────────▲────────────────────────────────────────────────┘
                                      │ WSS（Agent 主动外连）
             ┌────────────────────────┼────────────────────────┐
     ┌───────┴────────┐       ┌───────┴────────┐       ┌───────┴────────┐
     │ xhub-agent     │       │ xhub-agent     │       │ xhub-agent     │
     │ ├ 配置应用器   │       └────────────────┘       └────────────────┘
     │ ├ 流量账本     │
     │ ├ 系统指标     │
     │ └ Xray 进程管理│──gRPC(127.0.0.1)──▶ xray（子进程）
     └────────────────┘
```

**核心原则**

- **主控是唯一事实来源**，配置以声明式"期望版本"下发，Agent 收敛到期望状态。
- **控制面与数据面解耦**：主控宕机时各节点 Xray 按最后一份本地配置继续运行；恢复后自动对齐。
- Agent 只执行白名单指令，不提供远程 shell。

### 技术栈

| 层 | 选型 |
|---|---|
| Master / Agent | Go，单 module、两个静态二进制；复用 `xray-core` 的 gRPC proto 与账户类型 |
| 前端 | Vue 3 + Vite + TypeScript + Ant Design Vue + Lucide，`embed` 进 Master |
| 存储 | SQLite（纯 Go 驱动，WAL） |
| HTTP / WS | 标准库 `net/http` + `coder/websocket` |
| 主从协议 | WebSocket + JSON 消息 |

## 4. 主从通信

### 4.1 接入

```
curl -fsSL https://<master>/install.sh | bash -s -- \
  --master wss://<master>/agent/ws --token <一次性令牌> [--fp <证书SHA256指纹>]
```

1. 主控添加服务器时生成一次性接入令牌（1 小时有效，库中只存哈希）。
2. Agent 用令牌换取长期 `node secret`（本地 0600 保存），令牌作废。
3. 后续连接：`Authorization: Bearer <node_id>.<secret>`，主控只存 secret 的 SHA-256。
4. `--fp` 可选：传入则锁定证书公钥指纹（SPKI SHA-256，base64，与 `curl --pinnedpubkey` 同格式，自签模式），否则走系统 CA。
5. `install.sh` 与 Agent 二进制由主控提供，保证主从版本一致。

### 4.2 连接生命周期

```
Agent                                          Master
  │── WSS + Bearer ───────────────────────────────▶│ 校验；同节点旧连接踢下线
  │── hello{agent_ver, xray_ver, caps,            │
  │         applied_rev, applied_hash, ledger_head}▶│
  │◀─ welcome{server_time, ack_seq} ───────────────│ Agent 据 ack_seq 清理账本
  │◀─ apply{rev, config}（hash 不一致时）──────────│
  │── heartbeat(15s) / traffic(30s) ──────────────▶│ 45s 无消息判离线
  │◀─ ack{seq} / 指令 ─────────────────────────────│
```

断线重连：指数退避 1s → 60s，带抖动；凭据被拒（401）时退避 5 分钟。15s 心跳 + 30s WebSocket Ping 满足 Cloudflare 100s 空闲超时；Xray 状态变化时立即补发一次心跳。

**地址迁移**：Agent 在本地保存主控地址候选列表（最多 3 个），按顺序尝试。主控在 `welcome` 中携带当前推荐地址，修改访问设置时向在线 Agent 广播 `endpoint`；Agent 把推荐地址置为首选、旧地址保留为回退，通过首选地址连接成功后丢弃回退地址。因此修改端口、域名或 TLS 模式时，在线被控端不会失联。

### 4.3 消息

信封：`{ "t": "<type>", "id": "<req id>", "re": "<reply to>", "d": { ... } }`。主控指令带 `id`，Agent 以 `result{re, ok, err, data}` 应答；普通指令超时 30s，升级 5min。

| 类型 | 方向 | 要点 |
|---|---|---|
| `hello` | A→M | 版本、能力、已应用版本与哈希、账本最早未确认 seq |
| `heartbeat` | A→M | cpu/mem/disk/load/uptime、网卡累计计数、xray 状态、在线快照 |
| `traffic` | A→M | `seq`、`hour`、`items[{email, up, down}]`、`nic{rx, tx}` |
| `apply` | M→A | `rev` + 完整配置；Agent 串行处理，`rev ≤ applied` 忽略 |
| `endpoint` | M→A | 主控推荐地址变更 `{url, fp}` |
| `xray` | M→A | `start/stop/restart/install{version}`；安装包经主控中转，摘要通过 `X-Content-Sha256` 响应头下发 |
| `probe` | M→A | `reality_dest{host}`、`port_free{port}` |
| `logs` | M→A | 读取最近 N 行日志（环形缓冲） |

## 5. 配置编译与下发

```
UI 保存 → DB 事务 → node.desired_rev++ → Compiler（纯函数）生成完整 Xray JSON
       → apply{rev, config}
Agent：xray -test 校验 → 原子写盘 → 应用 → 回报 applied_rev/hash
重连：hello 中 hash 不一致则重推
```

界面显示每台服务器 **已同步 / 待同步 / 失败（原因）**。

| 变更 | 应用方式 |
|---|---|
| 订阅增减、超额、到期、停用、范围调整 | 主控照常下发完整配置；被控端比对上次应用的配置，仅 `clients` 变化时经 gRPC `AlterInbound` 在线增删用户，不重启（失败回退为重启） |
| 入站参数（端口、SNI、密钥等） | 重启 Xray（仅影响该服务器） |
| 路由 / 出站 / 全局设置 | 重启 Xray |

重启或升级 Xray 前，Agent 先读取一次流量并写入账本。

**Agent 自管段**：`api`（监听 `127.0.0.1:<随机端口>`）、`stats`、`policy` 中的用户统计开关由 Agent 维护并注入，主控编译的配置不包含这些段。本地无配置时 Agent 写入只含这些段的基础配置，保证 Xray 可先行启动。

## 6. 领域模型

入站不存 Xray JSON，以 Go 的有类型结构（按 `protocol` / `transport` / `security` 判别的联合类型）存为 `spec_json`。同一份模型统一产出四种结果，配合 golden file 测试保证一致：

```
Inbound
  ├─ ToXrayInbound(clients)  服务端配置
  ├─ ToShareLink(cred)       vless:// 等分享链接
  ├─ ToMihomo(cred)          Clash/Mihomo proxy
  └─ ToSingbox(cred)         sing-box outbound
```

**v1 协议范围**（均已用 Mihomo v1.19.33 实际连通，除 XHTTP 外均已用 sing-box v1.14.4 连通）

- VLESS + TCP + Reality + Vision
- VLESS + XHTTP + Reality（sing-box 不支持 XHTTP，sing-box 订阅不输出该类节点）
- VLESS / VMess + WS + TLS（Xray 已将 VMess 标记为弃用，表单中注明）
- Trojan + TLS
- Shadowsocks 2022（Xray 多用户仅支持 `2022-blake3-aes-128-gcm` / `256-gcm`）

**节点证书**（TLS 类入站）

- 证书文件：填写节点上已有证书的绝对路径；保存时被控端以 xhub 用户实际读取并校验，返回域名与到期时间。Xray 默认每 3600 秒重新读取证书文件，续签后无需重启。
- 自动申请：被控端内置 lego，以 Let's Encrypt + Cloudflare DNS-01 签发并每 12 小时检查续签（剩余不足 30 天）。ACME 邮箱与 Cloudflare Token 存于主控「设置 → 证书」，随 `apply` 下发给需要证书的节点；证书写入被控端数据目录 `certs/<域名>/`，主控编译的配置以相对路径引用。
- 申请失败不阻塞整台服务器：被控端跳过缺少证书的入站，其余配置照常生效，但不记录为已应用版本，主控显示失败原因，下次同步时重试。

**辅助能力**

- X25519 密钥对、ShortID 由主控以 `crypto/ecdh` 生成；Reality 私钥存主控，公钥派生用于链接。
- 伪装目标探测由 Agent 在本机网络执行（TLS1.3 / H2 / 延迟），内置常用 SNI 快选。
- 保存前由 Agent 检查端口占用。
- 全局 Xray 行为（屏蔽 BT、屏蔽私有 IP、IPv4/IPv6 优先、DNS）以开关呈现。
- 较新的协议特性默认关闭并标注"部分客户端不支持"，默认产物保证主流客户端（含 Shadowrocket）可用。
- Xray v26.3.27 之后的版本在 `minClientVer` 留空时默认拒绝低于 26.3.27 的客户端。第三方 Reality 客户端上报的版本很低（Mihomo 源码固定为 1.8.2，已实测被拒），因此表单提供「兼容第三方客户端」开关，新建入站默认开启（`minClientVer = 1.8.0`），只用 Xray 内核客户端时可关闭以遵循 Xray 默认值。
- 默认目标为 `dl.google.com:443`；快选列表不含 Xray 告警的 apple / icloud / microsoft 类域名（实测 `www.microsoft.com` 在部分网络下 Reality 握手失败），命中告警规则时表单给出提示。

## 7. 订阅与凭据

- 订阅链接 `token` 与连接凭据 `secret` 分离：重置链接不影响凭据，轮换凭据不改链接。
- 凭据由 `secret` 确定性派生，无需"订阅×入站"凭据表：
  - VLESS / VMess：订阅 `uuid`
  - Trojan：`HMAC-SHA256(secret, "trojan")` 取前 32 位十六进制
  - SS2022：`HKDF-SHA256(secret, info="ss2022:<inbound_id>")`，按 method 取 16 / 32 字节；客户端密码为 `服务端密钥:用户密钥`
- SS2022 入站没有任何有效订阅时，写入由服务端密钥派生的占位用户：Xray 在无用户时会退化为单用户模式，仅凭服务端密钥即可连接，占位用户可防止已移除的订阅继续使用（已实测）。
- Xray client email 统一为 `s{subId}.i{inboundId}`，统计天然细到"订阅 × 入站"。
- 入站生效 clients = `enabled 且 state=active 的订阅` ∩ `(all_inbounds 或 在授权表中)`。
- 订阅地址：`<public_url>/<sub_path>/{token}`，`sub_path` 首次启动随机生成、可在设置中修改，且不能与面板路径相同；令牌无效或订阅停用时一律 404。
- 订阅链接令牌可单独重置（旧链接失效、已导入节点不受影响）；凭据重置则使已导入节点失效。
- 节点名称按模板生成（`{node}` `{remark}` `{port}` `{flag}` `{region}` `{traffic}` `{days}`），重名自动追加序号。模板以 `|` 分段，某段变量全部为空时省略。地区取服务器手动指定的国家或地区，否则为被控端按出口 IP 检测的结果（Cloudflare trace，失败时 ipinfo.io，连接前检测、失败时重连再测）；剩余流量为服务器流量配额减本计费周期按计费方式（双向 / 仅出站 / 取较大值）统计的用量，剩余天数取 VPS 到期时间。
  - 按 User-Agent 自动选择格式（sing-box → Mihomo → Base64），`?format=base64|mihomo|singbox` 可强制。
  - sing-box 输出 1.12+ 格式（TUN + 本地 mixed 入站），规则集不设置 `download_detour`：它在 1.14 被弃用，而替代的 `http_client` 在 1.12 / 1.13 上无法解析。1.14 下会有一条弃用警告，1.16 起需按客户端版本输出 `http_clients`。
  - sing-box 的 VMess `security: auto` 会被 Xray 拒绝（unknown security type），输出时固定为 `aes-128-gcm`。
  - 返回 `subscription-userinfo`、`profile-update-interval`、`content-disposition` 头。
  - Mihomo 使用内置精简模板（select + url-test + 基础规则），已用 Mihomo v1.19.33 校验配置并实际连通。

## 8. 流量统计与额度

### 8.1 采集与上报

1. Agent 每 10s `QueryStats(reset=true)` 读取 `user>>>*>>>traffic>>>*` 增量，连同网卡增量按小时累计进本地账本（原子写文件）。
2. 每 30s（以及每次连上主控时立即）把未封存的小时累计量封存为带递增 `seq` 的批次并上报 `traffic`；离线期间每小时只产生一个批次。
3. 主控在单事务内 UPSERT `traffic_hourly` / `node_traffic_hourly`、累加订阅当前周期 `used_*`、推进 `nodes.last_seq`，`seq ≤ last_seq` 的重传批次跳过，随后回复 `ack{seq}`；`welcome` 也携带 `ackSeq`，处理 ack 丢失与被控端账本被清空的情况。
4. Agent 收到 ack 后删除对应批次。Xray 停止 / 重启前先采集一次，避免内存计数丢失。
5. 网卡计数器变小（机器重启）时以当前值为增量。

已实测（v26.7.28）：主控停机期间代理正常工作，期间流量在重连后完整入库；下载后立即重启 Xray 不丢计数；经 Mihomo 下载 21MB 统计偏差 +0.15%。

### 8.2 额度控制

- 流量入库后检查受影响订阅；每分钟扫描一次到期与周期重置。
- 超额 / 到期 → 更新 `state` → 触发同步，被控端经 gRPC 在线移除该订阅的用户（不重启）。
- 超额判定依赖流量上报，存在约 10~40 秒的延迟，期间仍可能超出少量流量。
- 支持按月重置（`reset_day`，1~28，按统计时区的当日 00:00），重置时清零当前周期用量并恢复 `state`；也可手动清零。修改重置日时以当前时刻开始新周期但保留已用量。

### 8.3 在线 IP / 设备数

- 仅在 Xray 支持 `online_stats` 能力的节点启用：配置开启 `statsUserOnline`，心跳携带 `email → [ip]` 快照。
- Xray 按活跃连接引用计数，连接断开即移除，且忽略 127.0.0.1 / ::1 来源。
- 主控在内存中按订阅取并集，展示当前在线 IP 与去重设备数；不落库。
- 只展示，不做限制。

## 9. Xray 版本与能力

**能力判定 = 静态能力表 ∧ 运行时探测**

```go
// internal/shared/xraycap
type Feature string

const (
    FeatOnlineStats Feature = "online_stats"
    FeatXHTTP       Feature = "xhttp"
)

var minVersion = map[Feature]Version{ /* M1 对照官方 changelog 填写 */ }
```

- Agent 在 `hello` 上报版本，并对 `online_stats` 等 API 实际探测（`codes.Unimplemented` 即不支持）。
- 主控存入 `nodes.capabilities_json`；Compiler 据此决定写入哪些字段，前端据此隐藏不支持的功能。

**版本管理**

- 系统设置：默认安装版本、可选版本上限；升级列表只显示不超过上限的版本。
- 单服务器可锁定版本：批量切换时保持锁定版本，新装 Xray 也使用锁定版本。
- 批量切换：所有在线服务器并行切换，返回每台的结果（已切换 / 无需切换 / 失败 / 离线）。
- Xray 安装包由主控中转：主控从 GitHub 下载、校验 sha256 并缓存，Agent 从 `/dl/xray/{ver}/{arch}` 获取。
- 测试矩阵：**v26.7.28** 与最新版。最低支持版本取 v1 所用特性要求的最高版本。

## 10. 访问、证书与系统设置

### 10.1 原则

**凡能在 Web 设置页修改的参数一律存数据库，不通过环境变量配置。** 容器部署仅需挂载数据目录（及可选的证书目录）。

### 10.2 首次启动

1. 无配置时使用默认值：监听 `:2096`，TLS 模式 `selfsigned`，随机面板路径。
2. 生成初始管理员账号密码，与访问地址一并打印到日志。
3. 首次登录后，`public_url` 以浏览器访问地址预填，引导管理员确认。

### 10.3 访问模式（Web 设置）

| 模式 | 场景 | 行为 | Agent 校验 |
|---|---|---|---|
| `proxy` | 前置 Nginx / Caddy / Cloudflare | 仅监听 HTTP | 系统 CA |
| `file` | 证书由外部脚本续签 | 读取证书文件，握手时按 mtime 热加载 | 系统 CA |
| `selfsigned` | 无域名 | 自动生成证书 | 安装命令带 `--fp` |

- 修改监听地址、端口、TLS 模式、面板路径时（容器部署时监听地址固定，见 11.1）：先校验（端口可绑定、证书可加载），再在进程内平滑重启 HTTP 服务，前端跳转到新地址；校验失败拒绝保存。
- `trusted_proxies`：Cloudflare / 反代场景下从 `CF-Connecting-IP` / `X-Forwarded-For` 取真实 IP。
- Cloudflare 橙云仅代理 443 / 2053 / 2083 / 2087 / 2096 / 8443 等端口，默认端口取 2096。
- TLS 模式下监听器先窥探首字节：TLS 握手照常处理，明文 HTTP 请求返回中文说明与切换命令，避免反向代理首次部署时只看到 Go 默认的 `Client sent an HTTP request to an HTTPS server`。
- 自签模式下生成安装命令前，主控会连接公开访问地址核对返回的证书公钥；若不是自签证书（前面有 Nginx / Cloudflare），拒绝生成并提示切换为反向代理模式。

### 10.4 设置项一览

| 分组 | 设置项 |
|---|---|
| 访问 | 监听地址与端口、面板路径、public_url、TLS 模式、证书/私钥路径、可信代理 |
| 订阅 | 订阅路径、更新间隔、节点命名模板 |
| Xray | 默认安装版本、版本上限、全局路由开关 |
| 统计 | 时区（默认 Asia/Shanghai）、小时明细保留天数（默认 90） |
| 安全 | 管理员账号密码、TOTP、会话时长 |

### 10.5 应急命令

误配置导致无法访问时，通过 CLI 恢复（容器内 `docker exec -it xhub xhub-master ...`）：

| 命令 | 作用 |
|---|---|
| `xhub-master settings show` | 打印当前访问配置 |
| `xhub-master settings reset-access` | 恢复 `:2096` + `selfsigned` + 新随机面板路径 |
| `xhub-master settings proxy --public-url URL [--trusted CIDR,...]` | 切换为反向代理模式；可信代理默认本机，容器中另加 `172.16.0.0/12` |
| `xhub-master admin reset` | 重置管理员密码并关闭 TOTP |

## 11. 部署

### 11.1 主控（Docker）

镜像 `ghcr.io/<owner>/xhub-master`，amd64 / arm64，基于 distroless（内嵌 `time/tzdata`），镜像内附各架构 Agent 二进制。

```yaml
services:
  xhub:
    image: ghcr.io/<owner>/xhub-master:latest
    container_name: xhub
    restart: unless-stopped
    ports:
      - "2096:2096"             # 宿主机端口被占用时只改左侧；前置反代时改为 "127.0.0.1:2096:2096"
    volumes:
      - ./data:/data
      # 使用自有证书时挂载证书所在目录（只读），再到「设置 → 访问」填写容器内路径。
      # 挂载目录而非单个文件：续签替换文件后单文件挂载会失效。
      # certbot 的 live/ 是指向 archive/ 的符号链接，需挂载整个 /etc/letsencrypt。
      # - /etc/ssl/xhub:/certs:ro
```

- 使用端口映射而非 host 网络：容器内固定监听 2096，对外端口由映射决定，避免占用宿主机端口。主控检测到运行在容器中（`/.dockerenv`）时，面板禁止修改监听地址，提示改 compose 映射并同步公开访问地址。
- Linux 上端口映射经 iptables DNAT，外部访问的来源 IP 照常保留，登录限流与日志不受影响。

也提供二进制 + systemd 的安装方式。

**备份与恢复**：面板「设置 → 备份」下载数据库一致性快照（`VACUUM INTO`），包含全部配置、凭据与流量统计。恢复需停止主控后执行 `xhub-master restore <备份文件>`（容器内：`docker compose run --rm xhub restore /data/<文件>`），原数据库保留为 `.bak-时间戳`。

### 11.2 被控端

- 一行命令安装，systemd 以专用用户运行，仅授予 `CAP_NET_BIND_SERVICE`。
- Xray 作为 Agent 子进程托管：崩溃自动拉起、日志环形缓冲、升级失败回滚。
- 主控所在机器作为节点时，Agent 直接装在宿主机，按普通服务器接入。

## 12. 数据模型（SQLite）

```sql
admins(id, username, password_hash /*argon2id*/, totp_secret, created_at)
sessions(id, admin_id, token_hash, ip, ua, expire_at)
settings(key PK, value_json)

nodes(
  id, name, remark, sort, enabled,
  public_host,
  secret_hash,
  desired_rev, desired_hash, applied_rev, applied_hash,
  sync_state /*pending|ok|failed*/, sync_error,
  agent_version, xray_version, xray_pinned_version, capabilities_json,
  last_seen_at, last_seq,
  net_reset_day, net_quota_bytes,
  created_at
)
enroll_tokens(id, node_id, token_hash, expire_at, used_at)

inbounds(
  id, node_id /*FK CASCADE*/, tag /*in-{id}*/, remark, sort, enabled,
  listen, port, protocol, spec_json,
  link_host_override, link_port_override,
  created_at, updated_at,
  UNIQUE(node_id, port)
)

subscriptions(
  id, name, note, sort,
  token UNIQUE, secret, uuid,
  enabled, state /*active|exceeded|expired*/,
  all_inbounds,
  limit_bytes, expire_at, reset_day,
  period_start_at, used_up, used_down,
  created_at
)
subscription_inbounds(sub_id, inbound_id, PK(sub_id, inbound_id))  -- 双向 FK CASCADE

traffic_hourly(hour, sub_id, inbound_id, node_id, up, down, PK(hour, sub_id, inbound_id))
traffic_daily (day,  sub_id, inbound_id, node_id, up, down, PK(day,  sub_id, inbound_id))
node_traffic_hourly(hour, node_id, rx, tx, PK(hour, node_id))
```

- 流量表不建外键、冗余 `node_id`：删除服务器或入站后历史保留，前端显示"已删除"。
- `id_seq` 记录服务器 / 入站 / 订阅的 ID 高水位，新建时取高水位 + 1：SQLite 默认会复用被删除的最大 ID，复用会把已删除对象的流量历史与 Xray 统计标识错配到新对象上。
- `nodes.xray_pinned_version` 为锁定的 Xray 版本。
- 当前周期用量读 `subscriptions.used_*`；历史从流量表聚合。
- 小时明细默认保留 90 天，之后按统计时区汇总入 `traffic_daily`。
- 实时指标与在线 IP 只在内存。
- `nodes` 另有 `last_seq`（已确认的流量批次序号）、`net_reset_day` / `net_quota_bytes`（服务器流量计费周期与配额）。

## 13. 安全要点

- Agent 无管理端口；Xray API 仅监听 `127.0.0.1`。
- Agent 只接受白名单指令，无远程 shell。
- 接入令牌一次性、短时效；节点 secret、会话 token、接入令牌在库中只存哈希。
- 面板路径随机化、登录限流、可选 TOTP 两步验证（RFC 6238，允许前后各 30 秒偏差；`admin reset` 同时关闭）。
- 订阅路径与面板路径分离，分享链接不暴露面板入口。

## 14. 目录结构

```
x-hub/
├─ cmd/{master,agent}/
├─ internal/
│  ├─ master/{api, store, hub, model, compiler, syncer, sub, accounting, settings}
│  ├─ agent/{conn, xray, probe, ledger, metrics}
│  └─ shared/{protocol, xraycap, tlspin}
├─ web/                      # Vue3 + Ant Design Vue
├─ deploy/{install.sh, systemd, docker-compose.yml}
└─ docs/
```

## 15. 里程碑

| 阶段 | 内容 | 验收 |
|---|---|---|
| M1 | 仓库骨架、首次启动与设置、管理员登录、一行命令接入、WS 长连接、心跳与指标、Xray 安装启停、能力探测 | 两台 VPS 接入后状态显示正常 |
| M2 | VLESS + Reality + Vision 表单、Compiler、下发与同步状态、分享链接与二维码 | 网页建节点后客户端可连通；完成 Vision 流量计数实测 |
| M3 | 订阅 CRUD、凭据派生与注入、订阅接口（Base64 + Mihomo） | 三个订阅各自可用且凭据互不相同 |
| M4 | 流量账本上报、统计图表、额度/到期/停用、按月重置、在线 IP | 断网 10 分钟恢复后流量不丢 |
| M5 | 其余协议、sing-box 格式、Xray 升级与回滚、TOTP、备份导出 | — |

## 16. 分流方案（M6）

目标：替代 sub-store，统一管理节点、分流规则与规则文件，同一份定义渲染为各客户端配置。

### 16.1 模型

- **分流方案（profile）**：可建多个，每个订阅选择一个（未选择时使用内置精简方案）。
  - 分组：名称、类型（select / url-test / fallback / load-balance）、固定成员（其他分组、DIRECT、REJECT）、节点筛选（正则包含 / 排除，来源：全部 / 自有 / 外部）、测速参数。
  - 规则：有序列表，`类型, 值, 目标分组[, no-resolve]`；Mihomo 的逻辑规则（AND / OR / NOT 等）以原文保存，仅 Mihomo 输出。
  - 基础配置：各客户端一份原文（Mihomo YAML / sing-box JSON），承载 DNS、TUN、嗅探、端口等差异较大的部分；不含节点、分组、规则集与规则。
- **规则集（全局库）**：名称、行为（domain / ipcidr / classical）、来源（托管：面板编辑内容；外部：URL + 格式 + 更新间隔）。规则以名称引用规则集，规则集改名时同步更新所有方案。
- **外部节点**（阶段二）：分享链接 / Mihomo 节点定义 / 外部订阅（Base64 与 Mihomo YAML，主控定时拉取）；支持前置节点（链式代理：Mihomo `dialer-proxy`、sing-box `detour`）；可设可见订阅；无流量统计。

### 16.2 渲染

- 分组成员由主控按正则展开为具体节点名（不依赖客户端的 include-all），各客户端行为一致；按正则未匹配到任何节点的分组补 DIRECT，并在预览中提示。
- 保存时校验：正则合法、分组 / 规则集引用存在、分组之间无循环引用、MATCH 位于最后。
- 规则集统一由主控以 `<公开地址>/<订阅路径>/rules/<令牌>/<名称>.<扩展名>` 提供（令牌防枚举），客户端不需要能访问规则集的原始地址：
  - Mihomo：托管规则集为 `.yaml`；外部规则集按格式以 `.yaml` / `.txt` / `.mrs` 原样中转；
  - sing-box / Shadowrocket：`.json` / `.list` 为转换后的格式（见 16.4、16.5）；
  - 外部规则集由主控拉取，按更新间隔缓存在内存中，拉取失败时使用旧内容；未设置公开访问地址时 Mihomo 保持原链接。
  - 保存外部规则集前由面板调用 `POST /api/rulesets/check` 拉取内容，按内容判断实际格式（zstd 文件头为 mrs，含 `payload:` 行为 yaml，其余为 text，并识别网页）。与所选格式不一致时提示并给出建议，确认后仍可保存；拉取失败不阻止保存。
- 导入：粘贴现有 Mihomo YAML，拆分为分组、规则集（入库，同名同 URL 复用）、规则与基础配置；`use` + `filter` 的分组转换为节点筛选。

### 16.3 外部节点（阶段二）

- **来源**：两类，统一存放在 `external_sources`。
  - 手动：粘贴分享链接（vless / vmess / trojan / ss / hysteria2 / tuic / socks5 / http，每行一条，也可整体 Base64）或 Mihomo 节点 YAML（`proxies:` 列表或单个节点）。
  - 订阅：URL + User-Agent（默认 `clash.meta`）+ 更新间隔；主控定时拉取并缓存解析结果，自动识别 Mihomo YAML 与 Base64；记录 `subscription-userinfo` 以展示剩余流量与到期时间；拉取失败保留上次结果。
- **统一表示**：每个节点保存为 Mihomo 节点定义（`map`）+ 原始分享链接（来自链接时）。Base64 输出优先使用原始链接，否则由节点定义转换（ss / vmess / vless / trojan / hysteria2 / socks5），无法转换的类型只出现在 Mihomo 输出中。
- **筛选与命名**：订阅来源可按正则包含 / 排除节点（去掉「剩余流量」之类的信息节点），可加名称前缀；与自有节点重名时追加序号。
- **链式代理**：来源可设置前置，作用于其全部节点：
  - 经由某个自有入站：订阅中不含该入站时，这些节点不输出；
  - 经由方案中的某个分组：方案没有该分组，或节点自身被该分组（直接或间接）包含时，这些节点不输出，并在预览中提示。
  - Mihomo 输出为 `dialer-proxy`；Base64 无法表达链式代理，带前置的节点不出现在 Base64 中；sing-box 输出为 `detour`。
- **可见范围**：全部订阅或指定订阅（默认不可见，需显式选择），停用的来源不输出。外部节点不经过 Xray，没有流量统计。
- **内置精简方案**改为由分流渲染器生成（与旧输出等价），自有节点与外部节点走同一条渲染路径。

### 16.4 sing-box 渲染（阶段三）

- 基础配置为 JSON 原文，为空时使用默认模板；`outbounds`、`route.rule_set`、`route.final` 由方案生成，基础配置中的 `route.rules`（sniff、hijack-dns 等）排在方案规则之前。
- 分组：select → selector，url-test → urltest；fallback / load-balance 以 urltest 代替并提示。直连出站标签为 `DIRECT`，分组成员可直接引用。sing-box 没有 REJECT 出站：从成员中移除，只含 REJECT 的分组视为拒绝分组，以它为目标的规则输出 `action: reject`（REJECT-DROP 为 `method: drop`）。
- 规则：域名 / IP / 端口 / 进程 / 网络类规则逐条转换；GEOIP LAN → `ip_is_private`，GEOSITE / GEOIP 其他值 → SagerNet 官方 `geosite-*` / `geoip-*` 二进制规则集（经主控 `rules/<令牌>/geo/<标签>.srs` 中转，缓存一天）；第一条需要目标 IP 且未设 no-resolve 的规则前插入 `action: resolve`，与 Mihomo 的解析行为一致。RAW（逻辑规则）、IP-ASN 跳过并提示。MATCH → `route.final`。
- 规则集：规则集库中的规则集统一指向主控的 `<名称>.json`（sing-box 源格式）：托管规则集直接转换；外部规则集由主控拉取 Mihomo YAML / text 后转换（与 Mihomo 中转共用缓存）；mrs 无法转换。基础配置中引用的规则集（如 DNS 规则的 `geosite-cn`）一并定义。
- 主控提供的规则集以 `download_detour: DIRECT` 直连下载（与订阅本身一致，避免节点不可用时客户端无法启动）；该字段在 1.14 起提示弃用，1.16 前需改为按客户端版本输出 `http_clients`。
- 节点：自有节点沿用原有转换；外部节点由 Mihomo 定义转换为 sing-box 出站，前置输出为 `detour`；XHTTP 等 sing-box 不支持的节点跳过并提示。

### 16.5 Shadowrocket 渲染

- 输出 Shadowrocket 配置（Surge 兼容的 `.conf`），只能以 `?format=shadowrocket` 显式获取：Shadowrocket 拉取订阅与配置时 User-Agent 相同，无法自动区分。节点不写入配置，仍由同一订阅的通用（Base64）链接导入；分组直接列出节点名称，因此只引用通用订阅中存在的节点（有分享链接且没有前置），名称含逗号或等号的节点跳过。
- 基础配置为 `[General]` 等段落原文，为空时使用默认模板；`[Proxy Group]` 与 `[Rule]` 由方案生成。
- 分组类型与 Mihomo 同名直接输出（select / url-test / fallback / load-balance）。规则：DOMAIN / DOMAIN-SUFFIX / DOMAIN-KEYWORD / IP-CIDR / IP-CIDR6 / IP-ASN / GEOIP / DST-PORT（单个端口）原样输出；GEOIP LAN 展开为私有地址段；MATCH → FINAL。GEOSITE、DOMAIN-REGEX、进程、端口范围、RAW 跳过并提示。
- 规则集统一指向主控的 `<名称>.list`（「类型,值」列表）：domain 行为的 `+.` / `.` 转为 DOMAIN-SUFFIX，通配符转为 DOMAIN-WILDCARD；外部规则集与 sing-box 共用拉取缓存。

### 16.6 阶段

| 阶段 | 内容 |
|---|---|
| 一 | 分流方案、规则集库与托管、导入 Mihomo YAML、Mihomo 渲染与预览、订阅选择方案 |
| 二 | 外部节点、外部订阅、链式代理 |
| 三 | sing-box 渲染与规则集格式转换 |
| 四 | Shadowrocket 渲染（iOS 分流） |

## 17. 待验证事项

| 事项 | 时机 |
|---|---|
| ~~XTLS Vision 在 Linux splice 场景下用户流量计数是否准确~~ | 已验证（v26.7.28，splice 路径下载 20MB 统计偏差 +0.17%，上传 5MB +0.15%） |
| 能力表中各特性的最低 Xray 版本（已确认 v26.7.28 支持 `online_stats`） | M2 |
| v26.7.28 下 Shadowrocket 对各协议组合的兼容性 | 待真实设备验证 |
| ACME 在真实 Cloudflare 域名上的签发与续签 | 待真实环境验证（失败路径已验证） |
| sing-box 1.16 起需按客户端版本输出 `http_clients` | 1.16 发布后 |
