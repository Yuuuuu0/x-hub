# 开发与发布

## 本地开发

依赖 Go 1.26+、Node 24+、pnpm 10。

```bash
make web            # 构建前端（嵌入主控）
make agents         # 构建 linux/amd64、linux/arm64 被控端
make master         # 构建主控
./dist/xhub-master --data ./data --agents ./dist/agents

# 前端热更新开发：代理到本地主控
cd web && XHUB_DEV_TARGET=https://127.0.0.1:2096/<面板路径> pnpm dev

make test           # go vet + go test + 前端类型检查
make docker         # 构建本地镜像
```

## 目录结构

```
cmd/{master,agent}/      主控与被控端入口
internal/master/         主控：存储、接口、配置编译、同步、订阅输出、流量核算
internal/agent/          被控端：连接、Xray 托管、流量账本、证书
internal/shared/         主控与被控端共用的协议、版本与公钥锁定
web/                     面板前端（Vue 3 + Ant Design Vue）
deploy/                  安装脚本与 compose 模板
```

设计说明见 [架构设计](architecture.md)。

## 发布

GitHub Actions：

| 触发 | 产物 |
|---|---|
| 推送 / PR | CI：gofmt、`go vet`、`go test -race`、前端类型检查与构建 |
| 推送到 `main` | 镜像 `ghcr.io/yuuuuu0/xhub-master:latest` 与 `:sha-<短哈希>`，版本号为 7 位短哈希 |
| 推送 `v*` 标签 | 镜像 `:<版本>`、`:<主.次>`；GitHub Release 附带主控、被控端程序与 SHA256SUMS |

```bash
git tag v0.1.0
git push origin v0.1.0
```

镜像同时提供 `linux/amd64` 与 `linux/arm64`。含 `-` 的标签（如 `v0.2.0-rc.1`）发布为预发布版本。
