#!/usr/bin/env bash
# x-hub 被控端安装脚本。由主控提供，用法见面板「服务器 → 安装命令」。
#   安装：bash install.sh --master https://hub.example.com:2096 --token <令牌> [--fp <公钥指纹>]
#   卸载：bash install.sh --uninstall
set -euo pipefail

BIN=/usr/local/bin/xhub-agent
CONF_DIR=/etc/xhub-agent
DATA_DIR=/var/lib/xhub-agent
UNIT=/etc/systemd/system/xhub-agent.service
SVC_USER=xhub

MASTER="" TOKEN="" FP="" UNINSTALL=0

die() { echo "错误：$*" >&2; exit 1; }
info() { echo "==> $*"; }

while [ $# -gt 0 ]; do
  case "$1" in
    --master) MASTER="${2:-}"; shift 2 ;;
    --token) TOKEN="${2:-}"; shift 2 ;;
    --fp) FP="${2:-}"; shift 2 ;;
    --uninstall) UNINSTALL=1; shift ;;
    *) die "未知参数 $1" ;;
  esac
done

[ "$(id -u)" -eq 0 ] || die "请以 root 身份运行"
command -v systemctl >/dev/null || die "需要 systemd"

if [ "$UNINSTALL" -eq 1 ]; then
  info "停止并移除 xhub-agent"
  systemctl disable --now xhub-agent 2>/dev/null || true
  rm -f "$UNIT" "$BIN"
  systemctl daemon-reload
  rm -rf "$CONF_DIR" "$DATA_DIR"
  id "$SVC_USER" >/dev/null 2>&1 && userdel "$SVC_USER" 2>/dev/null || true
  info "卸载完成"
  exit 0
fi

[ -n "$MASTER" ] && [ -n "$TOKEN" ] || die "缺少 --master 或 --token"
MASTER="${MASTER%/}"

case "$(uname -m)" in
  x86_64|amd64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) die "不支持的架构 $(uname -m)" ;;
esac

CURL=(curl -fsSL --retry 3)
if [ -n "$FP" ]; then
  CURL+=(-k --pinnedpubkey "sha256//$FP")
fi

if ! id "$SVC_USER" >/dev/null 2>&1; then
  info "创建系统用户 $SVC_USER"
  useradd --system --no-create-home --shell /usr/sbin/nologin "$SVC_USER" 2>/dev/null \
    || useradd --system --no-create-home --shell /sbin/nologin "$SVC_USER"
fi
install -d -m 0750 -o "$SVC_USER" -g "$SVC_USER" "$CONF_DIR" "$DATA_DIR"

info "下载被控端程序（$ARCH）"
"${CURL[@]}" -o "$BIN.new" "$MASTER/agent/dl/agent/$ARCH"
chmod 0755 "$BIN.new"
mv -f "$BIN.new" "$BIN"

info "接入主控"
FP_ARGS=()
[ -n "$FP" ] && FP_ARGS=(--fp "$FP")
"$BIN" enroll --master "$MASTER" --token "$TOKEN" ${FP_ARGS[@]+"${FP_ARGS[@]}"} \
  --config "$CONF_DIR/agent.json" --data "$DATA_DIR"
chown -R "$SVC_USER:$SVC_USER" "$CONF_DIR" "$DATA_DIR"

info "写入 systemd 服务"
cat >"$UNIT" <<EOF
[Unit]
Description=x-hub agent
After=network-online.target
Wants=network-online.target

[Service]
User=$SVC_USER
Group=$SVC_USER
ExecStart=$BIN run --config $CONF_DIR/agent.json
Restart=always
RestartSec=3
AmbientCapabilities=CAP_NET_BIND_SERVICE
CapabilityBoundingSet=CAP_NET_BIND_SERVICE
NoNewPrivileges=true
LimitNOFILE=1048576

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable xhub-agent >/dev/null
systemctl restart xhub-agent
info "安装完成，可在面板查看该服务器状态。日志：journalctl -u xhub-agent -f"
