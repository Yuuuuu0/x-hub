# x-hub

专为个人自用与亲友轻量分享设计的**主从分离版 3x-ui**：一台主控通过 Web 表单集中管理多台 VPS 上的 [Xray-core](https://github.com/XTLS/Xray-core)，被控端无 Web 界面、无管理端口。

> v1 功能已全部完成（M1–M5），尚待真实 VPS 与客户端的长期验证。设计与验证记录见 [架构设计](docs/architecture.md)。

## 特性

- **一行命令接入**：面板生成一次性安装命令，被控端主动外连主控，无需开放任何管理端口
- **三种访问模式**：反向代理 / 读取证书文件（自动热加载）/ 自签证书 + 公钥指纹锁定
- **实时状态**：在线状态、CPU / 内存 / 磁盘、实时网速、累计流量
- **Xray 管理**：自动安装默认版本、启动 / 停止 / 重启、切换版本（失败自动回滚）、崩溃自动拉起、查看日志
- **版本管理**：版本上限、单服务器锁定版本、批量切换（失败自动回滚）
- **账户安全**：可选 TOTP 两步验证
- **备份恢复**：一键下载数据库快照，命令行恢复
- **零环境变量**：业务参数均在面板「设置」中修改，切换 TLS 模式、面板路径无需重启
- **表单式入站**：VLESS + Reality + Vision、VLESS + XHTTP + Reality、VLESS / VMess + WS + TLS、Trojan + TLS、Shadowsocks 2022；一键生成 X25519 密钥、ShortID 与 SS2022 密钥，Reality 目标网站可从服务器本机实测 TLS 1.3 / H2
- **节点证书**：填写节点上已有证书的路径（保存前实测可读），或由被控端经 Let's Encrypt + Cloudflare DNS 自动申请与续签
- **自动下发**：保存后主控编译配置并推送，被控端校验后替换配置并重启 Xray；离线服务器上线后自动补发，面板展示同步状态
- **多订阅**：每个订阅独立 UUID 与订阅链接，可覆盖全部入站或指定入站；增删、停用、调整范围经 gRPC 在线生效，不重启 Xray、不中断现有连接
- **订阅链接**：按客户端自动输出通用 Base64、Mihomo 或 sing-box 配置，附带用量 / 到期信息头
- **单节点分享**：按订阅生成 `vless://` / `vmess://` / `trojan://` / `ss://` 链接与二维码
- **流量统计**：按订阅 × 入站 × 小时统计，本地账本 + 序号确认，断网或主控停机期间不丢流量；订阅详情含每日趋势与各入站明细
- **限额与到期**：订阅可设流量上限、到期时间与每月重置日，超额 / 到期自动停用、恢复后自动启用
- **服务器流量**：按 VPS 计费周期统计网卡总流量，可设配额
- **在线设备**：显示各订阅当前在线的来源 IP
- **第三方客户端兼容**：一键放宽新版 Xray 的 Reality 客户端版本限制，Mihomo / Clash Verge 等可直接连接

## 部署主控

```bash
mkdir xhub && cd xhub
curl -fsSLO https://raw.githubusercontent.com/Yuuuuu0/x-hub/main/deploy/docker-compose.yml
docker compose up -d
docker logs xhub        # 查看面板地址与初始密码
```

首次启动默认使用自签证书，面板路径随机生成。容器内固定监听 2096，宿主机端口被占用时修改 compose 中端口映射的左侧即可（如 `"8443:2096"`）。登录后：

1. 确认「公开访问地址」（被控端与订阅链接使用）
2. 如有自己的证书，在 compose 中挂载证书目录，再到「设置 → 访问」切换为「证书文件」模式
3. 在「设置 → 账户与安全」修改初始密码

### Nginx 反向代理

由 Nginx 终结 TLS、主控只对本机开放时，按以下步骤配置。

**1. compose 端口只绑定本机**

```yaml
    ports:
      - "127.0.0.1:2096:2096"
```

**2. 先通过 SSH 隧道进入面板完成切换**

此时外部已无法直连 2096，在本地电脑执行：

```bash
ssh -L 2096:127.0.0.1:2096 root@<服务器IP>
```

然后浏览器访问 `https://127.0.0.1:2096/<面板路径>/`（自签证书，需手动信任），在「设置 → 访问」中：

| 设置项 | 值 |
|---|---|
| TLS 模式 | 反向代理 |
| 公开访问地址 | `https://hub.example.com`（Nginx 对外的地址，非 443 端口需带上端口） |
| 可信代理 | Docker 部署填 `172.16.0.0/12`（经端口映射访问时，主控看到的来源是 Docker 网桥地址；自定义过网段时用 `docker network inspect` 查看）；二进制部署填 `127.0.0.1` |

保存后主控改为提供 HTTP，再配置 Nginx。

**3. Nginx 配置**

```nginx
# 放在 http {} 块中：仅对 WebSocket 请求发送 Connection: upgrade
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

    # 面板、订阅链接与被控端接入均由主控按路径分发，未知路径主控返回 404，整站转发即可。
    location / {
        proxy_pass http://127.0.0.1:2096;
        proxy_http_version 1.1;

        # 被控端通过 /agent/ws 保持 WebSocket 长连接
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection $connection_upgrade;

        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        # 主控优先采信 CF-Connecting-IP，必须清除客户端自带的该头，否则可伪造来源 IP 绕过登录限流
        proxy_set_header CF-Connecting-IP "";

        # 被控端每 15 秒发送心跳；首次安装 Xray 时主控需先从 GitHub 下载安装包，留足超时
        proxy_read_timeout 300s;
        proxy_send_timeout 300s;
        proxy_buffering off;
    }
}
```

**Nginx 前面还有 Cloudflare 橙云时**，在 `server {}` 中加入以下配置，由 Nginx 还原真实客户端 IP 后再经 `X-Real-IP` 传给主控（`CF-Connecting-IP` 仍照常清除）：

```nginx
    real_ip_header CF-Connecting-IP;
    # Cloudflare 回源 IP 段，完整列表见 https://www.cloudflare.com/ips/
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

此时主控的可信代理仍按第 2 步填写（Nginx 的地址），无需填入 Cloudflare 网段。

### 应急命令

```bash
docker exec -it xhub xhub-master settings show          # 查看访问配置
docker exec -it xhub xhub-master settings reset-access  # 恢复 :2096 + 自签证书 + 新面板路径（需重启容器）
docker exec -it xhub xhub-master admin reset            # 重置管理员密码并关闭两步验证
```

### 恢复备份

```bash
docker compose stop
docker compose run --rm xhub restore /data/xhub-backup-xxxx.db
docker compose up -d
```

## 接入被控端

在面板「服务器 → 添加服务器」后复制安装命令，以 root 在目标 VPS 执行即可。要求 systemd，支持 amd64 / arm64。

被控端以 `xhub` 系统用户运行（仅授予 `CAP_NET_BIND_SERVICE`），Xray 由被控端作为子进程托管：

| 路径 | 内容 |
|---|---|
| `/usr/local/bin/xhub-agent` | 被控端程序 |
| `/etc/xhub-agent/agent.json` | 节点凭据与主控地址 |
| `/var/lib/xhub-agent/` | Xray 二进制、配置与状态 |

日志：`journalctl -u xhub-agent -f`

## 开发

依赖 Go 1.26+、Node 24+、pnpm 10。

```bash
make web            # 构建前端（嵌入主控）
make agents         # 构建 linux/amd64、linux/arm64 被控端
make master         # 构建主控
./dist/xhub-master --data ./data --agents ./dist/agents

# 前端热更新开发：代理到本地主控
cd web && XHUB_DEV_TARGET=https://127.0.0.1:2096/<面板路径> pnpm dev

make test           # go vet + go test + 前端类型检查
```

## 发布

推送到 `main` 会构建 `edge` 镜像；推送版本标签会构建版本镜像与 `latest`，并在 GitHub Release 附带主控与被控端程序：

```bash
git tag v0.1.0
git push origin v0.1.0
```

镜像同时提供 `linux/amd64` 与 `linux/arm64`。含 `-` 的标签（如 `v0.2.0-rc.1`）视为预发布，不会更新 `latest`。

## 许可证

[MIT](LICENSE)
