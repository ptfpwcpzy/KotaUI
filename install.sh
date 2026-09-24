#!/bin/sh
set -eu

PREFIX=${KOTAUI_PREFIX:-/opt/kotaui}
DATA_DIR=${KOTAUI_DATA_DIR:-/var/lib/kotaui}
BIN_DIR=${KOTAUI_BIN_DIR:-/usr/local/bin}
SOURCE_DIR=${KOTAUI_SOURCE_DIR:-}
PANEL_PORT=${KOTAUI_PANEL_PORT:-}
PANEL_PATH=${KOTAUI_PANEL_PATH:-}
ADMIN_USER=${KOTAUI_ADMIN_USER:-}
ADMIN_PASSWORD=${KOTAUI_ADMIN_PASSWORD:-}
CERT_TYPE=${KOTAUI_CERT_TYPE:-}
CERT_SUBJECT=${KOTAUI_CERT_SUBJECT:-}
CERT_EMAIL=${KOTAUI_CERT_EMAIL:-}
CERTBOT_BIN=${KOTAUI_CERTBOT_BIN:-certbot}
GO_BIN=${KOTAUI_GO_BIN:-}
GO_VERSION=${KOTAUI_GO_VERSION:-go1.22.12}
GO_TOOLCHAIN_DIR=${KOTAUI_GO_TOOLCHAIN_DIR:-$PREFIX/go}

[ "$(id -u)" -eq 0 ] || { printf '%s\n' '请使用 root 身份运行安装器。' >&2; exit 1; }

clear_screen(){ command -v clear >/dev/null 2>&1 && clear || printf '\033c'; }
step(){ printf '\n[ %s ] %s\n' "$1" "$2"; }
ok(){ printf '[  OK  ] %s\n' "$1"; }
fail(){ printf '[ 失败 ] %s\n' "$1" >&2; exit 1; }
ask(){ printf '%s' "$1" >&2; IFS= read -r answer || true; printf '%s' "$answer"; }

welcome(){ clear_screen; cat <<'EOF'
+----------------------------------------------------------+
|                         KotaUI                           |
|                轻量 sing-box 管理面板                    |
+----------------------------------------------------------+
  作者：那么羡慕你
  项目：https://github.com/ptfpwcpzy/KotaUI

  本次安装将依次完成：
  [1/6] 选择域名或 IP 证书
  [2/6] 设置面板端口与访问路径
  [3/6] 设置管理员账号与密码
  [4/6] 准备运行环境与 sing-box
  [5/6] 构建面板与流量统计核心
  [6/6] 申请证书、启动服务并健康检查

  适用 Alpine、Debian、Ubuntu；建议单核 512 MB 以上内存。
  请确认域名或 IP 已指向本服务器，且 TCP 80 可验证。
+----------------------------------------------------------+
  作者那么羡慕你，仅供学习自用，请勿随意传播。
+----------------------------------------------------------+
EOF
}
