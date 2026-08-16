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

validate_port(){ case "$1" in ''|*[!0-9]*) return 1;; esac; [ "$1" -ge 1024 ] && [ "$1" -le 65535 ]; }
validate_path(){ case "$1" in ''|*[!A-Za-z0-9_-]*) return 1;; esac; [ "${#1}" -le 48 ]; }

choose_certificate(){
  step '1 / 6' '选择证书类型'
  if [ -z "$CERT_TYPE" ]; then
    printf '1) 域名证书\n2) IP 证书\n'
    choice=$(ask '请选择 [默认 1：域名]： ')
    case "${choice:-1}" in 1) CERT_TYPE=domain;; 2) CERT_TYPE=ip;; *) fail '证书类型无效。';; esac
  fi
  if [ "$CERT_TYPE" = domain ]; then
    while [ -z "$CERT_SUBJECT" ]; do CERT_SUBJECT=$(ask '请输入域名： '); [ -n "$CERT_SUBJECT" ] || printf '域名不能为空。\n'; done
  elif [ "$CERT_TYPE" = ip ]; then
    printf '注意：IP 证书的有效期通常较短，需要依赖自动续签。\n'
    detected=$(curl -4fsS --max-time 5 https://api.ipify.org 2>/dev/null || true)
    if [ -z "$CERT_SUBJECT" ]; then CERT_SUBJECT=$(ask "请输入 IP [默认：${detected:-请手动输入}]： "); CERT_SUBJECT=${CERT_SUBJECT:-$detected}; fi
    [ -n "$CERT_SUBJECT" ] || fail '无法自动识别公网 IP，请手动输入。'
  else fail '证书类型必须为 domain 或 ip。'; fi
  if [ -z "$CERT_EMAIL" ]; then
    token=$(od -An -N8 -tx1 /dev/urandom 2>/dev/null | tr -d ' \n' || true)
    token=${token:-$(date +%s)}
    CERT_EMAIL="kotaui-${token}@gmail.com"
  fi
}

choose_panel(){
  step '2 / 6' '设置面板访问参数'
  while :; do
    [ -n "$PANEL_PORT" ] || PANEL_PORT=$(ask '自定义面板端口 [默认 1989]： ')
    PANEL_PORT=${PANEL_PORT:-1989}; validate_port "$PANEL_PORT" && break
    printf '端口必须是 1024–65535 的数字。\n'; PANEL_PORT=
  done
  while :; do
    [ -n "$PANEL_PATH" ] || PANEL_PATH=$(ask '自定义面板路径 [默认 ptf]： ')
    PANEL_PATH=${PANEL_PATH:-ptf}; PANEL_PATH=$(printf '%s' "$PANEL_PATH" | tr -d '/')
    validate_path "$PANEL_PATH" && break
    printf '路径只允许字母、数字、下划线或连字符。\n'; PANEL_PATH=
  done
}

choose_admin(){
  step '3 / 6' '设置管理员账号和密码'
  while [ -z "$ADMIN_USER" ]; do ADMIN_USER=$(ask '管理员账号： '); done
  while [ -z "$ADMIN_PASSWORD" ]; do
    ADMIN_PASSWORD=$(ask '管理员密码（输入内容会显示，仅输入一次）： ')
    [ -n "$ADMIN_PASSWORD" ] || printf '密码不能为空。\n'
  done
}

install_packages(){
  . /etc/os-release 2>/dev/null || true
  step '4 / 6' '安装轻量运行环境与 sing-box 核心'
  case "${ID:-}" in
    alpine) apk add --no-cache ca-certificates curl git go certbot openssl sing-box python3 py3-pip py3-virtualenv ;;
    debian|ubuntu) apt-get update -qq; DEBIAN_FRONTEND=noninteractive apt-get install -y -qq ca-certificates curl git golang-go certbot openssl python3 python3-venv python3-pip; if ! command -v sing-box >/dev/null 2>&1; then curl -fsSL https://sing-box.app/install.sh | sh; fi ;;
    *) fail '当前仅支持 Alpine、Debian 和 Ubuntu。';;
  esac
  ok '运行环境与 sing-box 核心已准备。'
}

prepare_ip_certbot(){
  [ "$CERT_TYPE" = ip ] || return 0
  if "$CERTBOT_BIN" --help all 2>&1 | grep -q -- '--ip-address'; then return 0; fi
  printf '正在准备支持 IP 证书的 Certbot…\n'
  venv="$PREFIX/certbot-venv"
  rm -rf "$venv"
  python3 -m venv "$venv" || fail '无法创建 Certbot 运行环境。'
  "$venv/bin/pip" install --quiet --upgrade 'certbot>=5.4' || fail '无法安装支持 IP 证书的 Certbot。'
  CERTBOT_BIN="$venv/bin/certbot"
  "$CERTBOT_BIN" --help all 2>&1 | grep -q -- '--ip-address' || fail '新版 Certbot 未提供 IP 证书功能。'
}

acquire_certificate(){
  step '6 / 6' '申请证书并启用自动续签'
  prepare_ip_certbot
  if [ "$CERT_TYPE" = domain ]; then "$CERTBOT_BIN" certonly --standalone --non-interactive --agree-tos -m "$CERT_EMAIL" -d "$CERT_SUBJECT"; else "$CERTBOT_BIN" certonly --standalone --non-interactive --agree-tos --preferred-profile shortlived -m "$CERT_EMAIL" --ip-address "$CERT_SUBJECT"; fi
  cert_dir="/etc/letsencrypt/live/$CERT_SUBJECT"
  [ -r "$cert_dir/fullchain.pem" ] && [ -r "$cert_dir/privkey.pem" ] || fail '证书文件未生成。'
  mkdir -p "$DATA_DIR/certs" "$DATA_DIR/sing-box" "$PREFIX/bin"
  ln -sfn "$cert_dir/fullchain.pem" "$DATA_DIR/certs/fullchain.pem"; ln -sfn "$cert_dir/privkey.pem" "$DATA_DIR/certs/privkey.pem"
  ok '证书已签发并已链接到 KotaUI。'
}

install_program(){
  if [ -z "$SOURCE_DIR" ]; then SOURCE_DIR=$(mktemp -d); trap 'rm -rf "$SOURCE_DIR"' EXIT; git clone --depth=1 https://github.com/ptfpwcpzy/KotaUI.git "$SOURCE_DIR"; fi
  [ -f "$SOURCE_DIR/go.mod" ] || fail '未找到 KotaUI Go 源码。'
  step '5 / 6' '构建 KotaUI 与用户流量统计核心'
  install -d -m 755 "$PREFIX" "$PREFIX/bin" "$DATA_DIR" "$DATA_DIR/sing-box"
  (cd "$SOURCE_DIR" && CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o "$PREFIX/kotaui" ./cmd/kotaui)
  install -m 755 "$SOURCE_DIR/service/kota" "$BIN_DIR/kota"
  install -m 755 "$SOURCE_DIR/service/kota-cert-renew" "$BIN_DIR/kota-cert-renew"
  install -m 755 "$SOURCE_DIR/service/kota-build-singbox-stats" "$PREFIX/bin/kota-build-singbox-stats"
  "$PREFIX/bin/kota-build-singbox-stats" "$PREFIX/sing-box-v2ray"
  cat > "$DATA_DIR/runtime.env" <<EOF
KOTAUI_DATA_DIR=$DATA_DIR
KOTAUI_LISTEN=0.0.0.0:$PANEL_PORT
KOTAUI_PANEL_PORT=$PANEL_PORT
KOTAUI_PANEL_PATH=/$PANEL_PATH
KOTAUI_SUBSCRIPTION_PORT=1109
KOTAUI_DOMAIN=$CERT_SUBJECT
KOTAUI_CERT_TYPE=$CERT_TYPE
KOTAUI_TLS_CERT=$DATA_DIR/certs/fullchain.pem
KOTAUI_TLS_KEY=$DATA_DIR/certs/privkey.pem
KOTAUI_ADMIN_USER=$ADMIN_USER
KOTAUI_ADMIN_PASSWORD=$ADMIN_PASSWORD
KOTAUI_CERTBOT_BIN=$CERTBOT_BIN
KOTAUI_SINGBOX_BIN=$PREFIX/sing-box-v2ray
KOTAUI_SINGBOX_CONFIG=$DATA_DIR/sing-box/config.json
KOTAUI_MANAGE_SINGBOX=1
KOTAUI_STATS_PORT=9090
EOF
  chmod 600 "$DATA_DIR/runtime.env"
}

install_services(){
  . /etc/os-release 2>/dev/null || true
  if [ "${ID:-}" = alpine ]; then
    install -d -m 755 /etc/init.d /etc/conf.d /etc/periodic/6hourly
    install -m 755 "$SOURCE_DIR/service/kotaui.openrc" /etc/init.d/kotaui
    install -m 755 "$SOURCE_DIR/service/kotaui-singbox.openrc" /etc/init.d/kotaui-singbox
    printf 'KOTAUI_RUNTIME_ENV="%s/runtime.env"\n' "$DATA_DIR" > /etc/conf.d/kotaui
    cp "$BIN_DIR/kota-cert-renew" /etc/periodic/6hourly/kota-cert-renew; chmod 700 /etc/periodic/6hourly/kota-cert-renew
    rc-update add kotaui default >/dev/null 2>&1 || true; rc-update add kotaui-singbox default >/dev/null 2>&1 || true; rc-service kotaui restart || true; rc-service kotaui-singbox restart || true
  else
    install -m 644 "$SOURCE_DIR/service/kotaui.service" /etc/systemd/system/kotaui.service
    install -m 644 "$SOURCE_DIR/service/kotaui-singbox.service" /etc/systemd/system/kotaui-singbox.service
    install -m 644 "$SOURCE_DIR/service/kota-cert-renew.service" /etc/systemd/system/kota-cert-renew.service
    install -m 644 "$SOURCE_DIR/service/kota-cert-renew.timer" /etc/systemd/system/kota-cert-renew.timer
    systemctl daemon-reload; systemctl reset-failed kotaui-singbox >/dev/null 2>&1 || true; systemctl enable --now kotaui; systemctl enable --now kotaui-singbox kota-cert-renew.timer
  fi
}

final_screen(){
  clear_screen
  cat <<EOF
============================================================
                    KotaUI 安装完成
============================================================
管理地址： https://$CERT_SUBJECT:$PANEL_PORT/$PANEL_PATH
管理员账号： $ADMIN_USER
管理员密码： $ADMIN_PASSWORD

证书已签发，自动续签已启用。
重要：请立即保存以上地址、账号和密码。
初始密码仅在本页面显示一次。
------------------------------------------------------------
下次需要管理 KotaUI 时，请在终端输入 kota 呼出主菜单。
EOF
}

welcome
choose_certificate
choose_panel
choose_admin
install_packages
install_program
acquire_certificate
install_services
step '完成' '执行服务健康检查'
attempt=0
until "$BIN_DIR/kota" check >/dev/null 2>&1; do
  attempt=$((attempt + 1))
  [ "$attempt" -lt 15 ] || fail 'KotaUI 健康检查失败，请执行 kota 查看状态和日志。'
  sleep 1
done
ok '健康检查通过。'
final_screen
