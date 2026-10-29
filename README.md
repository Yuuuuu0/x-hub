# x-hub

专为个人自用与亲友轻量分享设计的**主从分离版 3x-ui**：一台主控通过 Web 表单集中管理多台 VPS 上的 [Xray-core](https://github.com/XTLS/Xray-core)，被控端无 Web 界面、无管理端口。

> 开发中。当前完成 M1：服务器接入与管理、Xray 安装 / 启停 / 版本切换。入站表单、多订阅与流量统计见 [架构设计](docs/architecture.md) 中的里程碑。

## 特性（M1）

- **一行命令接入**：面板生成一次性安装命令，被控端主动外连主控，无需开放任何管理端口
- **三种访问模式**：反向代理 / 读取证书文件（自动热加载）/ 自签证书 + 公钥指纹锁定
- **实时状态**：在线状态、CPU / 内存 / 磁盘、实时网速、累计流量
- **Xray 管理**：自动安装默认版本、启动 / 停止 / 重启、切换版本（失败自动回滚）、崩溃自动拉起、查看日志
- **版本上限**：可限制可安装的最高 Xray 版本，便于兼容特定客户端
- **零环境变量**：所有参数均在面板「设置」中修改，修改端口 / TLS 模式无需重启

## 部署主控

```bash
mkdir xhub && cd xhub
curl -fsSLO https://raw.githubusercontent.com/Yuuuuu0/x-hub/main/deploy/docker-compose.yml
docker compose up -d
docker logs xhub        # 查看面板地址与初始密码
```

首次启动默认监听 `:2096`、使用自签证书，面板路径随机生成。登录后：

1. 确认「公开访问地址」（被控端与订阅链接使用）
2. 如有自己的证书，在 compose 中挂载证书目录，再到「设置 → 访问」切换为「证书文件」模式
3. 在「设置 → 账户与安全」修改初始密码

### 应急命令

```bash
docker exec -it xhub xhub-master settings show          # 查看访问配置
docker exec -it xhub xhub-master settings reset-access  # 恢复 :2096 + 自签证书 + 新面板路径（需重启容器）
docker exec -it xhub xhub-master admin reset            # 重置管理员密码
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

## 许可证

[MIT](LICENSE)
