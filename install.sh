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

[ "$(id -u)" -eq 0 ] || { printf '%s\n' '请使用 root 身份运行安装器。' >&2; exit 1; }

clear_screen(){ command -v clear >/dev/null 2>&1 && clear || printf '\033c'; }
step(){ printf '\n[ %s ] %s\n' "$1" "$2"; }
ok(){ printf '[  OK  ] %s\n' "$1"; }
fail(){ printf '[ 失败 ] %s\n' "$1" >&2; exit 1; }
ask(){ printf '%s' "$1"; IFS= read -r answer || true; printf '%s' "$answer"; }

welcome(){ clear_screen; cat <<'EOF'
╔════════════════════════════════════════════════════════════╗
║                         KotaUI                             ║
║              轻量、安全的 sing-box 管理面板                ║
╠════════════════════════════════════════════════════════════╣
║  本向导将完成：证书、面板访问、管理员账号与服务启动。      ║
║  请先确认域名或 IP 已指向当前服务器，且 TCP 80 可验证。    ║
╚════════════════════════════════════════════════════════════╝
EOF
}

validate_port(){ case "$1" in ''|*[!0-9]*) return 1;; esac; [ "$1" -ge 1024 ] && [ "$1" -le 65535 ]; }
validate_path(){ case "$1" in ''|*[!A-Za-z0-9_-]*) return 1;; esac; [ "${#1}" -le 48 ]; }

choose_certificate(){
  step '1 / 5' '选择证书类型'
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
  if [ -z "$CERT_EMAIL" ]; then CERT_EMAIL=$(ask '证书通知邮箱： '); fi
  [ -n "$CERT_EMAIL" ] || fail '证书通知邮箱不能为空。'
}

choose_panel(){
  step '2 / 5' '设置面板访问参数'
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
  step '3 / 5' '设置管理员账号和密码'
  while [ -z "$ADMIN_USER" ]; do ADMIN_USER=$(ask '管理员账号： '); done
  while [ -z "$ADMIN_PASSWORD" ]; do
    printf '管理员密码： '; stty -echo; IFS= read -r ADMIN_PASSWORD || true; stty echo; printf '\n'
    [ -n "$ADMIN_PASSWORD" ] || printf '密码不能为空。\n'
  done
  if [ -z "${KOTAUI_NONINTERACTIVE:-}" ]; then
    printf '再次输入管理员密码： '; stty -echo; IFS= read -r second || true; stty echo; printf '\n'
    [ "$ADMIN_PASSWORD" = "$second" ] || fail '两次密码不一致。'
  fi
}

install_packages(){
  . /etc/os-release 2>/dev/null || true
  step '4 / 5' '安装轻量运行环境与 sing-box 核心'
  case "${ID:-}" in
    alpine) apk add --no-cache ca-certificates curl git go certbot openssl sing-box ;;
    debian|ubuntu) apt-get update -qq; DEBIAN_FRONTEND=noninteractive apt-get install -y -qq ca-certificates curl git golang-go certbot openssl; if ! command -v sing-box >/dev/null 2>&1; then curl -fsSL https://sing-box.app/install.sh | sh; fi ;;
    *) fail '当前仅支持 Alpine、Debian 和 Ubuntu。';;
  esac
  ok '运行环境与 sing-box 核心已准备。'
}

acquire_certificate(){
  step '5 / 5' '申请证书并启用自动续签'
  if [ "$CERT_TYPE" = domain ]; then certbot certonly --standalone --non-interactive --agree-tos -m "$CERT_EMAIL" -d "$CERT_SUBJECT"; else certbot certonly --standalone --non-interactive --agree-tos -m "$CERT_EMAIL" --ip-address "$CERT_SUBJECT"; fi
  cert_dir="/etc/letsencrypt/live/$CERT_SUBJECT"
  [ -r "$cert_dir/fullchain.pem" ] && [ -r "$cert_dir/privkey.pem" ] || fail '证书文件未生成。'
  mkdir -p "$DATA_DIR/certs" "$DATA_DIR/sing-box" "$PREFIX/bin"
  ln -sfn "$cert_dir/fullchain.pem" "$DATA_DIR/certs/fullchain.pem"; ln -sfn "$cert_dir/privkey.pem" "$DATA_DIR/certs/privkey.pem"
  ok '证书已签发并已链接到 KotaUI。'
}

install_program(){
  if [ -z "$SOURCE_DIR" ]; then SOURCE_DIR=$(mktemp -d); trap 'rm -rf "$SOURCE_DIR"' EXIT; git clone --depth=1 https://github.com/ptfpwcpzy/KotaUI.git "$SOURCE_DIR"; fi
  [ -f "$SOURCE_DIR/go.mod" ] || fail '未找到 KotaUI Go 源码。'
  install -d -m 755 "$PREFIX" "$PREFIX/bin"
  (cd "$SOURCE_DIR" && CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o "$PREFIX/kotaui" ./cmd/kotaui)
  install -m 755 "$SOURCE_DIR/service/kota" "$BIN_DIR/kota"
  install -m 755 "$SOURCE_DIR/service/kota-cert-renew" "$BIN_DIR/kota-cert-renew"
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
KOTAUI_SINGBOX_BIN=$(command -v sing-box)
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
    systemctl daemon-reload; systemctl enable --now kotaui; systemctl enable --now kotaui-singbox kota-cert-renew.timer
  fi
}

final_screen(){
  clear_screen
  cat <<EOF
╔════════════════════ 安装完成 · 请保存以下信息 ════════════════════╗
║                                                                    ║
║  KotaUI 管理地址                                                   ║
║  https://$CERT_SUBJECT:$PANEL_PORT/$PANEL_PATH
║                                                                    ║
║  管理员账号：$ADMIN_USER
║  管理员密码：$ADMIN_PASSWORD
║                                                                    ║
║  证书状态：已签发，自动续签已启用                                  ║
║  重要：请立即保存地址、账号和密码。初始密码仅在此显示一次。       ║
╚════════════════════════════════════════════════════════════════════╝
使用 kota 可随时打开本机管理菜单。
EOF
}

welcome
choose_certificate
choose_panel
choose_admin
install_packages
acquire_certificate
install_program
install_services
step '6 / 6' '完成服务健康检查'
attempt=0
until "$BIN_DIR/kota" check >/dev/null 2>&1; do
  attempt=$((attempt + 1))
  [ "$attempt" -lt 15 ] || fail 'KotaUI 健康检查失败，请执行 kota 查看状态和日志。'
  sleep 1
done
ok '健康检查通过。'
final_screen
