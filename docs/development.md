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

## 演示环境

演示环境是一份不连接主控的前端构建：`VITE_DEMO=1` 时 `web/src/lib/api.ts` 把所有接口请求交给 `web/src/demo/mock.ts`，读取类接口返回 `web/src/demo/fixtures.json` 中的预置数据，修改类接口提示「演示环境为只读」。订阅的分享页（含各格式的查看与下载）在生成预置数据时由主控渲染并保存为静态文件（`web/src/demo/public/share/`），「预览分享页」打开它们。路由改用 hash 模式，以适应 GitHub Pages 没有 SPA 回退的限制。正式构建不包含这部分代码。

```bash
cd web && pnpm build:demo   # 输出到 web/dist-demo，可用任意静态服务器预览
```

推送到 `main` 且改动了 `web/` 时，`Demo` 工作流会构建并发布到 GitHub Pages。

预置数据与静态分享页由真实主控生成，接口或分享页有变化时重新生成即可（步骤见 `web/src/demo/fixtures.py` 开头的说明）。README 中的截图也取自演示环境，分享页截图取自写入同样数据的本地主控。

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
| 推送到 `main`（改动 `web/`） | 演示环境发布到 GitHub Pages |

```bash
git tag v0.1.0
git push origin v0.1.0
```

镜像同时提供 `linux/amd64` 与 `linux/arm64`。含 `-` 的标签（如 `v0.2.0-rc.1`）发布为预发布版本。

被控端使用独立的版本号：最后一次改动被控端相关代码（`cmd/agent`、`internal/agent`、`internal/shared`、`internal/version`、`go.mod`、`go.sum`）的提交短哈希，由 `make print-agent-version` 计算并在构建时注入主控（`version.AgentVersion`）与被控端（`version.Version`）。面板据此判断已接入的被控端是否需要更新，只改主控或界面时不会提示。
