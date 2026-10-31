# x-hub

一个面板管理你所有的 VPS 代理节点，并分享给朋友和家人。

x-hub 适合**自己用、顺便分给亲友**的场景：

- **像 3x-ui 一样用表单配置节点**，不用手写 Xray JSON；
- **只部署一个面板**：其余 VPS 只运行一个无界面的被控端，不开放任何管理端口；
- **每人一个订阅链接**：每个朋友一份独立凭据，用了多少流量一目了然，可以限额、设到期、随时停用；
- **没有机场那一套**：没有套餐、订单、注册和工单。

## 功能一览

**服务器**
- 面板生成一行命令，在 VPS 上执行即可接入；Xray 自动安装
- 实时查看在线状态、CPU / 内存 / 磁盘、网速与本月流量
- 自动识别服务器所在国家或地区（可手动修改）；记录 VPS 的流量配额、计费方式与到期时间
- 一键启停、重启、切换或批量升级 Xray，可锁定版本；新版本启动失败自动回滚

**节点（入站）**
- 支持 VLESS + Reality + Vision、VLESS + XHTTP + Reality、VLESS / VMess + WS + TLS、Trojan + TLS、Shadowsocks 2022
- 密钥、ShortID 一键生成；Reality 伪装目标可以从服务器本机实测是否可用
- TLS 证书可以用节点上已有的证书文件，也可以由被控端通过 Cloudflare DNS 自动申请、自动续签
- 保存后自动下发到服务器，面板显示是否同步成功

**订阅（分享给谁）**
- 每个订阅一个链接，自动适配 v2rayN / Shadowrocket、Clash Verge / Mihomo、sing-box
- 可以覆盖全部节点，也可以只给某几个，或在全部节点中排除某几个
- 订阅链接用浏览器打开是一个分享页：用量、到期时间、二维码，以及各格式订阅内容的查看与下载
- 流量上限、到期时间、每月重置日；超额或到期自动停用
- 流量按天统计，能看到每人在每个节点上的用量，以及当前在线的设备
- 新增、停用订阅即时生效，不会打断其他人正在用的连接

**外部节点**
- 自购的服务器、住宅代理、机场订阅都可以加进来，和自有节点一起出现在订阅里
- 支持粘贴分享链接或 Mihomo 节点定义，也支持外部订阅（Base64 与 Clash 格式），自动定时更新并显示剩余流量
- 链式代理：例如先连自己的服务器，再由它连接住宅 IP
- 按正则筛掉「剩余流量」之类的信息节点；指定哪些订阅能看到

**分流（Clash 系、sing-box 与 Shadowrocket）**
- 在网页上编辑分组、规则与 DNS 等基础配置，同一份方案同时生成 Mihomo、sing-box 与 Shadowrocket 配置；每个订阅可以选用不同的方案
- 分组按名称正则自动收纳节点，增删服务器后无需改规则
- 规则集统一管理：可以引用外部链接，也可以直接托管在面板里
- 现有的 Mihomo 配置可一键导入

**其他**
- 两步验证（TOTP）登录
- 一键备份，命令行恢复

## 准备

- **一台运行主控的机器**：装有 Docker 即可，可以是其中一台 VPS
- **要管理的 VPS**：Linux（使用 systemd），amd64 或 arm64
- **一个域名（推荐）**：用于通过 HTTPS 访问面板；没有域名也能用，浏览器会提示证书不受信任

## 快速开始

### 1. 部署主控

```bash
mkdir xhub && cd xhub
curl -fsSLO https://raw.githubusercontent.com/Yuuuuu0/x-hub/main/deploy/docker-compose.yml
docker compose up -d
docker logs xhub
```

日志里会打印**面板地址**和**初始密码**，例如：

```
 面板地址：https://<服务器IP>:<映射的宿主机端口>/aB3dE5fG7hJ9/
 初始账号：admin
 初始密码：xxxxxxxxxxxxxxxx
```

面板地址最后那一段是随机生成的面板路径，只有带上它才能打开面板，直接访问域名根路径会显示 404。

compose 默认把宿主机的 2096 端口映射给主控。2096 被占用时，只需修改 `docker-compose.yml` 里端口映射的左侧，例如 `"8443:2096"`。

### 2. 选择访问方式

| 方式 | 适合 | 怎么做 |
|---|---|---|
| **Nginx 反向代理**（推荐） | 有域名，服务器上已有 Nginx | 见下文 [Nginx 反向代理](#nginx-反向代理) |
| 直接访问 | 先试试 / 没有域名 | 访问 `https://服务器IP:2096/面板路径/`，浏览器提示证书不安全时选择继续 |
| 使用自己的证书文件 | 有证书但不想装 Nginx | 在 compose 中挂载证书目录，登录后在「设置 → 访问」选择「证书文件」并填写容器内路径 |

### 3. 首次登录

登录后会提示确认**公开访问地址**，即别人从外面访问面板用的地址（如 `https://hub.example.com`）。被控端安装命令和订阅链接都会用到它，请务必填对。

接着建议：
- 在「设置 → 账户与安全」修改初始密码，并开启两步验证；
- 在「设置 → Xray」选择默认安装的 Xray 版本。需要兼容特定客户端时，可以设一个版本上限。

### 4. 接入服务器

「服务器 → 添加服务器」，填写名称后会弹出安装命令。以 root 身份在目标 VPS 上执行，弹窗会实时显示接入进度，几秒后服务器就会显示在线，并自动安装 Xray。

被控端会按出口 IP 自动识别国家或地区，也可以在「编辑」中手动指定，或改用自定义的地区名称（如「东京」）。「VPS 套餐」中填写流量配额、计费方式（双向合计 / 仅出站 / 取较大值）和到期时间后，节点名称里会带上剩余流量与剩余天数。

> 如果用了 Nginx 反向代理，请在完成反代配置**之后**再生成安装命令。

### 5. 创建节点

「入站 → 新建入站」，选择服务器和协议组合：

| 协议组合 | 什么时候用 |
|---|---|
| **VLESS + Reality + Vision** | 首选。不需要域名和证书，速度快 |
| VLESS + XHTTP + Reality | 同样不需要证书；sing-box 客户端不支持 |
| VLESS / VMess + WS + TLS | 需要套 Cloudflare 等 CDN 时 |
| Trojan + TLS | 有域名和证书，习惯用 Trojan 时 |
| Shadowsocks 2022 | 中转、内网互联等不需要伪装的场景 |

Reality 节点的「**兼容第三方客户端**」开关默认开启。新版 Xray 默认会拒绝 Clash Verge、Mihomo Party、sing-box 等非 Xray 内核的客户端，关闭这个开关后这些客户端就连不上了。

保存后，配置会自动下发到服务器。服务器卡片上显示「配置已同步」即表示生效。

### 6. 分享给自己和朋友

「订阅」页面里，每个人建一个订阅（首次启动已自动创建「自己」）：

- 「订阅链接」：复制链接或扫码导入客户端，节点有变化时客户端更新订阅即可；
- 「编辑」：设置流量上限、到期时间、每月重置日，以及能用哪些节点（全部入站 / 指定入站 / 排除指定入站）；
- 「流量」：查看每天的用量，以及在各节点上的明细；
- 开关：随时停用，对方立即无法连接；
- 「重置 → 订阅链接」：链接泄露时使用，旧链接失效，已导入的节点仍可用；
- 「重置 → 连接凭据」：让已导入的节点全部失效，需要重新导入。

直接把「自动识别」订阅链接发给使用者即可：用浏览器打开会显示分享页，列出用量、到期时间、节点和二维码，Mihomo、sing-box、通用、Shadowrocket 分流配置四种订阅内容都可以在浏览器中查看或下载为文件；客户端导入同一个链接时照常拿到节点。

想临时测试单个节点，可以在「入站」列表点击二维码图标，复制单节点分享链接。

订阅中的节点按「入站」列表的顺序输出，拖动行首的手柄即可调整（新入站排在最后）；「服务器」页的卡片同样可以拖动排序。调整顺序不会重启 Xray，客户端更新订阅后生效。外部节点排在自有节点之后。

节点名称由「设置 → 订阅 → 节点名称模板」决定，默认格式为 `{flag} {region}-{remark}|📊{traffic}|⏳{days}`，例如 `🇯🇵 日本-acck|📊1.14TB|⏳69D`（地区-入站备注|剩余流量|剩余天数）。从旧版本升级的面板会保留原来的模板，可在该处点击输入框右侧的按钮换成推荐格式。模板以 `|` 分段，某段中的变量都为空时整段省略，所以没有设置流量配额或到期时间的服务器不会显示对应部分。剩余流量和天数会随时间变化，客户端更新订阅后名称随之改变，Clash 系客户端中手动选中的节点可能需要重新选择。

### 7. 添加外部节点（可选）

「外部节点」页面可以加入不由 x-hub 管理的节点：

- 「手动添加」：粘贴分享链接（每行一条，支持 vless / vmess / trojan / ss / hysteria2 / tuic / socks5 / http）或 Mihomo 节点定义；
- 「外部订阅」：填写机场订阅地址，按设定的间隔自动更新；更新失败时继续使用上一次的节点；
- 「名称排除」填 `剩余|到期|官网` 之类的正则，可以去掉订阅里的信息节点；「名称前缀」便于在分流中按来源分组；
- 「前置」用于链式代理：选「经由自有服务器」时，客户端先连到该入站，再由它连接外部节点，常用于住宅代理；也可以经由分流方案中的某个分组；
- 「可见范围」决定哪些订阅包含这些节点，默认不对任何订阅开放。

外部节点不经过 x-hub 的服务器，没有流量统计与限额。带前置的节点只出现在 Clash 系与 sing-box 客户端的订阅中，Shadowrocket、v2rayN 等通用订阅不包含它们。

### 8. 自定义分流（可选）

Clash 系、sing-box 与 Shadowrocket 默认拿到内置的精简分流。想要按自己的习惯分流，到「分流」页面：

- 「导入 Mihomo 配置」：粘贴正在用的配置文件，分组、规则、规则集会被拆开导入，DNS、TUN 等设置原样保留为基础配置；配置里的固定节点和 proxy-providers 不会导入（可在「外部节点」中添加），引用它们的分组会改为按名称筛选节点；
- 也可以「新建方案」从默认模板开始，在「分组」「规则」「基础配置」中修改；
- 分组的「包含节点」用正则匹配节点名称，例如 `(?i)香港|HK`，编辑时会显示匹配到的节点；
- 「基础配置」中 Mihomo、sing-box、Shadowrocket 各有一份（sing-box 为 JSON；sing-box 与 Shadowrocket 留空时使用默认模板）；
- 「预览」用某个订阅的节点生成各客户端的完整配置，保存前即可检查效果；
- 最后在「订阅 → 编辑」中为订阅选择这个方案。

「规则集」标签页统一管理规则集，多个方案可以共用。托管的规则集直接在面板里编辑内容；外部链接的规则集（如 GitHub 上的）由面板拉取并缓存。客户端统一从面板下载规则集，不需要能访问规则集的原始链接；sing-box 与 Shadowrocket 使用的规则集由面板转换格式后提供，sing-box 的 geosite / geoip 规则集也经面板中转。因此需要先在「设置 → 访问」中填写公开访问地址。保存外部链接时，面板会拉取一次内容，检测格式是否选对（以内容为准，比如 Loyalsoldier 的 `.txt` 文件其实是 yaml）。

sing-box 不支持的内容会跳过并在预览中提示：逻辑规则（AND / OR 等）、IP-ASN、mrs 格式的规则集、XHTTP 节点；故障转移与负载均衡分组以自动测速代替，只含 REJECT 的分组会把对应规则改为拒绝。

## 客户端导入

订阅链接会根据客户端自动返回对应格式，一般直接导入即可。

| 客户端 | 格式 | 说明 |
|---|---|---|
| v2rayN / v2rayNG / Shadowrocket / NekoBox | 通用 | |
| Clash Verge / Mihomo Party / Stash | Mihomo | 默认自带「节点选择」「自动选择」分组，局域网与国内直连；可在「分流」中自定义 |
| sing-box 官方客户端（SFA / SFI / SFM） | sing-box | 需 1.12 及以上版本；不包含 XHTTP 节点；可在「分流」中自定义 |

自动识别不准时，可以在订阅链接弹窗里切换格式，复制带 `?format=` 参数的链接。

**Shadowrocket 分流**：Shadowrocket 的节点和分流规则分开导入。

1. 首页「+」添加订阅，使用「自动识别」或「通用」链接，导入节点；
2. 「配置」页添加远程配置，使用订阅链接弹窗中的「Shadowrocket」链接（`?format=shadowrocket`），并选中使用这份配置。

配置里的分组直接引用订阅中的节点名称，两者来自同一个订阅时自动对应；修改方案或节点后，在 Shadowrocket 中更新订阅与配置即可。带前置的外部节点、逻辑规则（AND / OR 等）、GEOSITE、端口范围不会出现在 Shadowrocket 配置中，预览里会提示。

## Nginx 反向代理

**1. 主控端口只对本机开放**

修改 `docker-compose.yml`：

```yaml
    ports:
      - "127.0.0.1:2096:2096"
```

**2. 把主控切换为反向代理模式**

主控默认自己提供 HTTPS，而 Nginx 会用 HTTP 连接它，所以需要先切换：

```bash
docker exec xhub xhub-master settings proxy --public-url https://hub.example.com
docker restart xhub
```

`--public-url` 填写 Nginx 对外的地址，不是 443 端口时需带上端口。

**3. 配置 Nginx**

```nginx
# 放在 http {} 块中
map $http_upgrade $connection_upgrade {
    default upgrade;
    ''      '';
}

server {
    listen 443 ssl;
    listen [::]:443 ssl;
    http2 on;                      # Nginx 1.25.1 以下改为 listen 443 ssl http2;
    server_name hub.example.com;

    ssl_certificate     /etc/letsencrypt/live/hub.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/hub.example.com/privkey.pem;

    location / {
        proxy_pass http://127.0.0.1:2096;   # 与 compose 中映射的宿主机端口一致
        proxy_http_version 1.1;

        # 服务器通过 WebSocket 与主控保持连接
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection $connection_upgrade;

        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        # 防止伪造来源 IP 绕过登录限制，请保留
        proxy_set_header CF-Connecting-IP "";

        proxy_read_timeout 300s;
        proxy_send_timeout 300s;
        proxy_buffering off;
    }
}
```

执行 `nginx -t && nginx -s reload` 后，访问 `https://hub.example.com/面板路径/` 即可。

<details>
<summary>Nginx 前面还套了 Cloudflare（橙云）时</summary>

在 `server {}` 中加入以下配置，让 Nginx 还原访客的真实 IP：

```nginx
    real_ip_header CF-Connecting-IP;
    # Cloudflare 回源 IP 段，最新列表见 https://www.cloudflare.com/ips/
    set_real_ip_from 173.245.48.0/20;
    set_real_ip_from 103.21.244.0/22;
    set_real_ip_from 103.22.200.0/22;
    set_real_ip_from 103.31.4.0/22;
    set_real_ip_from 141.101.64.0/18;
    set_real_ip_from 108.162.192.0/18;
    set_real_ip_from 190.93.240.0/20;
    set_real_ip_from 188.114.96.0/20;
    set_real_ip_from 197.234.240.0/22;
    set_real_ip_from 198.41.128.0/17;
    set_real_ip_from 162.158.0.0/15;
    set_real_ip_from 104.16.0.0/13;
    set_real_ip_from 104.24.0.0/14;
    set_real_ip_from 172.64.0.0/13;
    set_real_ip_from 131.0.72.0/22;
    set_real_ip_from 2400:cb00::/32;
    set_real_ip_from 2606:4700::/32;
    set_real_ip_from 2803:f800::/32;
    set_real_ip_from 2405:b500::/32;
    set_real_ip_from 2405:8100::/32;
    set_real_ip_from 2a06:98c0::/29;
    set_real_ip_from 2c0f:f248::/32;
```

</details>

## 日常维护

**更新主控**

```bash
docker compose pull && docker compose up -d
```

**更新被控端**：服务器卡片「⋯ → 重装命令」，在 VPS 上重新执行即可。卡片上显示 Agent 版本与主控不一致时，就该更新了。

**备份与恢复**：「设置 → 备份」下载备份文件。恢复时把文件放到 compose 的 `data` 目录下，然后执行：

```bash
docker compose stop
docker compose run --rm xhub restore /data/xhub-backup-xxxx.db
docker compose up -d
```

备份包含全部设置、节点私钥与订阅凭据，请妥善保管。服务器无需重新接入。

**卸载被控端**：安装命令弹窗底部附有卸载命令，在 VPS 上执行即可。

**常用命令**

```bash
docker exec xhub xhub-master settings show          # 查看面板路径、访问设置
docker exec xhub xhub-master admin reset            # 忘记密码 / 两步验证丢失时重置
docker exec xhub xhub-master settings reset-access  # 访问设置改错打不开面板时恢复默认（需重启容器）
journalctl -u xhub-agent -f                          # 在 VPS 上查看被控端日志
```

## 常见问题

**打开面板显示「主控当前以 HTTPS 提供服务，但收到了明文 HTTP 请求」**

前面有 Nginx 反代，但主控还没切换为反向代理模式，执行 [Nginx 反向代理](#nginx-反向代理) 第 2 步即可。

**Nginx 返回 502**

检查 `proxy_pass` 是否为 `http://`，端口是否与 compose 中映射的宿主机端口一致。可以在服务器上执行 `curl http://127.0.0.1:端口/`，返回 404 说明主控正常。

**访问域名显示 404**

这是正常的。面板地址需要带上面板路径，可以用 `docker exec xhub xhub-master settings show` 查看。

**安装被控端时报 `curl: (90) SSL: public key does not match pinned public key`**

这条安装命令是在切换为反向代理模式之前生成的。在面板上重新生成一次安装命令即可。

**Clash Verge / Mihomo 连不上 Reality 节点**

编辑节点，确认「兼容第三方客户端」已开启。

**TLS 节点提示证书文件不可用**

被控端以 `xhub` 用户运行，无法读取 root 才能读的证书（certbot 生成的私钥默认如此）。可以在续签脚本里把证书复制一份并 `chown xhub`，或者改用「自动申请」。

**流量超额后对方还多用了一点**

流量每 10~40 秒统计一次，超额停用会有这么长的延迟。

**服务器卡片显示「同步失败」**

把鼠标放在上面可以看到原因，修正后点击旁边的重试按钮。

## 安全提示

- **订阅链接等同于密码**，只发给对应的人；泄露后在订阅卡片上「重置 → 订阅链接」。
- 面板路径相当于第二道门，不要公开。
- 备份文件包含所有节点的私钥，不要上传到公开的地方。

## 了解更多

- [架构设计](docs/architecture.md)：工作原理与设计取舍
- [开发与发布](docs/development.md)

## 许可证

[MIT](LICENSE)
