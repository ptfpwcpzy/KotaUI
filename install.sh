#!/bin/sh
# KotaUI first-install wizard. Designed for Alpine, Debian and Ubuntu VPS hosts.
set -eu

PREFIX=${KOTAUI_PREFIX:-/opt/kotaui}
DATA_DIR=${KOTAUI_DATA_DIR:-/var/lib/kotaui}
BIN_DIR=${KOTAUI_BIN_DIR:-/usr/local/bin}
REPO_URL=${KOTAUI_REPO_URL:-https://github.com/ptfpwcpzy/KotaUI.git}
PANEL_PORT=${KOTAUI_PANEL_PORT:-1989}
PANEL_PATH=${KOTAUI_PANEL_PATH:-ptf}
SUB_PORT=${KOTAUI_SUBSCRIPTION_PORT:-1109}
STATS_PORT=${KOTAUI_STATS_PORT:-9090}
SINGBOX_VERSION=${KOTAUI_SINGBOX_VERSION:-v1.13.18}
BUILD_SWAP_MB=${KOTAUI_BUILD_SWAP_MB:-768}
CREATE_BUILD_SWAP=${KOTAUI_CREATE_BUILD_SWAP:-1}
CONFIGURE_FIREWALL=${KOTAUI_CONFIGURE_FIREWALL:-0}
CERT_TYPE=${KOTAUI_CERT_TYPE:-}
CERT_SUBJECT=${KOTAUI_CERT_SUBJECT:-}
CERT_EMAIL=${KOTAUI_CERT_EMAIL:-}
ADMIN_USER=${KOTAUI_ADMIN_USER:-}
ADMIN_PASSWORD=${KOTAUI_ADMIN_PASSWORD:-}
NONINTERACTIVE=${KOTAUI_NONINTERACTIVE:-0}

info(){ printf '\033[1;34m[ KotaUI ]\033[0m %s\n' "$*"; }
ok(){ printf '\033[1;32m[   OK   ]\033[0m %s\n' "$*"; }
warn(){ printf '\033[1;33m[  提示  ]\033[0m %s\n' "$*"; }
fail(){ printf '\033[1;31m[  失败  ]\033[0m %s\n' "$*" >&2; exit 1; }
clear_screen(){ command -v clear >/dev/null 2>&1 && clear || printf '\033c'; }
step(){ printf '\n\033[1;36m[ %s ]\033[0m %s\n' "$1" "$2"; }

welcome(){
  clear_screen
  cat <<'EOF'
╔════════════════════════════════════════════════════════════╗
║                         KotaUI                             ║
║              轻量、安全的 sing-box 管理面板                ║
╠════════════════════════════════════════════════════════════╣
║  本向导将完成：                                             ║
║  证书申请与自动续签 · 面板安全配置 · 服务启动              ║
║                                                            ║
║  请确保域名或 IP 已指向当前服务器，且 TCP 80 可用于验证。  ║
╚════════════════════════════════════════════════════════════╝
EOF
}

need_root(){ [ "$(id -u)" -eq 0 ] || fail '请使用 root 身份运行安装脚本。'; }
valid_port(){ case "$1" in ''|*[!0-9]*) return 1;; esac; [ "$1" -ge 1024 ] && [ "$1" -le 65535 ]; }
valid_path(){ case "$1" in ''|*[!A-Za-z0-9_-]*|.*) return 1;; esac; [ "${#1}" -ge 2 ] && [ "${#1}" -le 32 ]; }
valid_user(){ case "$1" in ''|*[!A-Za-z0-9_-]*) return 1;; esac; [ "${#1}" -ge 3 ] && [ "${#1}" -le 32 ]; }
valid_ipv4(){ printf '%s' "$1" | awk -F. 'NF==4 {for(i=1;i<=4;i++) if($i !~ /^[0-9]+$/ || $i>255) exit 1; exit 0} {exit 1}'; }
valid_domain(){
  case "$1" in ''|*..*|.*|*.) return 1;; esac
  printf '%s' "$1" | grep -Eq '^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?)+$'
}
read_value(){
  prompt=$1; default=${2:-}; secret=${3:-0}
  if [ "$NONINTERACTIVE" = 1 ]; then printf '%s' "$default"; return 0; fi
  if [ "$secret" = 1 ]; then
    printf '%s' "$prompt" >&2; stty -echo; IFS= read -r value || true; stty echo; printf '\n' >&2
  else
    printf '%s' "$prompt" >&2; IFS= read -r value || true
  fi
  [ -n "$value" ] || value=$default
  printf '%s' "$value"
}

install_runtime(){
  case "$ID" in
    alpine) apk add --no-cache nodejs npm git openssl certbot curl ca-certificates go >/dev/null ;;
    debian|ubuntu)
      export DEBIAN_FRONTEND=noninteractive
      apt-get update -qq
      apt-get install -y -qq nodejs npm git openssl certbot curl ca-certificates golang >/dev/null
      ;;
  esac
}

maybe_enable_build_swap(){
  BUILD_SWAP_FILE="$DATA_DIR/.kota-build.swap"
  [ "$CREATE_BUILD_SWAP" = 1 ] || return 0
  mem_kb=$(awk '/MemTotal/ {print $2}' /proc/meminfo 2>/dev/null || printf 0)
  [ "$mem_kb" -lt 900000 ] || return 0
  [ "$BUILD_SWAP_MB" -gt 0 ] 2>/dev/null || return 0
  [ -w "$DATA_DIR" ] || return 0
  if swapon --show 2>/dev/null | grep -q .; then return 0; fi
  info "检测到小内存 VPS，临时创建 ${BUILD_SWAP_MB} MB 构建交换空间。"
  if command -v fallocate >/dev/null 2>&1; then
    fallocate -l "${BUILD_SWAP_MB}M" "$BUILD_SWAP_FILE" 2>/dev/null || true
  fi
  [ -s "$BUILD_SWAP_FILE" ] || dd if=/dev/zero of="$BUILD_SWAP_FILE" bs=1M count="$BUILD_SWAP_MB" status=none 2>/dev/null || true
  chmod 600 "$BUILD_SWAP_FILE" 2>/dev/null || true
  if [ -s "$BUILD_SWAP_FILE" ] && mkswap "$BUILD_SWAP_FILE" >/dev/null 2>&1 && swapon "$BUILD_SWAP_FILE" >/dev/null 2>&1; then
    BUILD_SWAP_ACTIVE=1
    ok '临时构建交换空间已启用。'
  else
    warn '无法创建临时交换空间；将使用单线程低内存编译。'
    rm -f "$BUILD_SWAP_FILE" 2>/dev/null || true
  fi
}
cleanup_build_swap(){
  [ "${BUILD_SWAP_ACTIVE:-0}" = 1 ] || return 0
  swapoff "$BUILD_SWAP_FILE" >/dev/null 2>&1 || true
  rm -f "$BUILD_SWAP_FILE" >/dev/null 2>&1 || true
  BUILD_SWAP_ACTIVE=0
}

install_singbox(){
  if command -v sing-box >/dev/null 2>&1 && sing-box version 2>/dev/null | grep -q 'with_v2ray_api'; then
    ok '已检测到带用户流量统计能力的 sing-box。'
    return 0
  fi
  step '4 / 6' '正在构建带用户流量统计能力的 sing-box（低内存模式）'
  mkdir -p "$DATA_DIR"
  maybe_enable_build_swap
  if ! GOMAXPROCS=1 GOMEMLIMIT=256MiB SINGBOX_VERSION="$SINGBOX_VERSION" SINGBOX_PREFIX="$BIN_DIR" sh "$PREFIX/tools/build-singbox-v2ray.sh"; then
    cleanup_build_swap
    fail 'sing-box 构建失败。请检查 VPS 可用内存、磁盘空间和网络后重试。'
  fi
  cleanup_build_swap
  "$BIN_DIR/sing-box" version | grep -q 'with_v2ray_api' || fail '构建产物未启用用户流量统计能力。'
  ok 'sing-box 已安装，并启用用户流量统计。'
}

detect_public_ipv4(){
  curl -4fsS --max-time 5 https://api.ipify.org 2>/dev/null || hostname -I 2>/dev/null | awk '{print $1}' || true
}

configure_certificate(){
  step '2 / 6' '配置 HTTPS 证书'
  if [ -z "$CERT_TYPE" ]; then
    choice=$(read_value '证书类型：1) 域名  2) IP  [默认：1]：' '1')
    case "$choice" in 1|domain|DOMAIN) CERT_TYPE=domain;; 2|ip|IP) CERT_TYPE=ip;; *) warn '请输入 1 或 2。'; configure_certificate; return;; esac
  fi
  case "$CERT_TYPE" in
    domain)
      while :; do
        [ -n "$CERT_SUBJECT" ] || CERT_SUBJECT=$(read_value '请输入已解析到本机的域名：' '')
        valid_domain "$CERT_SUBJECT" && break
        warn '域名格式无效。请填写完整域名，例如 panel.example.com。'
        CERT_SUBJECT=
      done
      ;;
    ip)
      warn 'IP 证书有效期较短，KotaUI 会启用每 6 小时检查一次的自动续签。'
      detected=$(detect_public_ipv4)
      while :; do
        [ -n "$CERT_SUBJECT" ] || CERT_SUBJECT=$(read_value "请输入公网 IPv4 [默认：${detected:-请手动填写}]：" "$detected")
        valid_ipv4 "$CERT_SUBJECT" && break
        warn 'IPv4 格式无效，请重新输入。'
        CERT_SUBJECT=
      done
      ;;
    *) fail '证书类型仅支持 domain 或 ip。';;
  esac
  if [ -z "$CERT_EMAIL" ] && [ "$NONINTERACTIVE" != 1 ]; then
    CERT_EMAIL=$(read_value '证书通知邮箱（可直接回车跳过）：' '')
  fi
  info "正在为 $CERT_SUBJECT 申请证书，请保持 TCP 80 可访问。"
  mkdir -p "$DATA_DIR/certs"
  set -- certonly --standalone --non-interactive --agree-tos --keep-until-expiring -d "$CERT_SUBJECT"
  [ -n "$CERT_EMAIL" ] && set -- "$@" --email "$CERT_EMAIL" || set -- "$@" --register-unsafely-without-email
  [ "$CERT_TYPE" = ip ] && set -- "$@" --cert-profile shortlived
  certbot "$@"
  [ -r "/etc/letsencrypt/live/$CERT_SUBJECT/fullchain.pem" ] || fail '未找到签发后的证书文件。'
  ln -sfn "/etc/letsencrypt/live/$CERT_SUBJECT/fullchain.pem" "$DATA_DIR/certs/fullchain.pem"
  ln -sfn "/etc/letsencrypt/live/$CERT_SUBJECT/privkey.pem" "$DATA_DIR/certs/privkey.pem"
  umask 077
  printf 'KOTAUI_CERT_SUBJECT=%s\nKOTAUI_CERT_TYPE=%s\n' "$CERT_SUBJECT" "$CERT_TYPE" > "$DATA_DIR/certificate.env"
  ok '证书已签发并链接到 KotaUI。'
}

configure_panel(){
  step '3 / 6' '配置面板访问与管理员账号'
  while :; do
    PANEL_PORT=$(read_value '面板端口 [默认：1989]：' "$PANEL_PORT")
    valid_port "$PANEL_PORT" && [ "$PANEL_PORT" != "$SUB_PORT" ] && break
    warn "端口必须在 1024-65535 之间，且不能与订阅端口 $SUB_PORT 相同。"
  done
  while :; do
    PANEL_PATH=$(read_value '面板路径 [默认：ptf]：' "$PANEL_PATH")
    valid_path "$PANEL_PATH" && break
    warn '路径仅允许 2-32 位字母、数字、下划线或连字符，且无需填写 /。'
  done
  while :; do
    ADMIN_USER=$(read_value '面板管理员账号 [默认：admin]：' "${ADMIN_USER:-admin}")
    valid_user "$ADMIN_USER" && break
    warn '账号仅允许 3-32 位字母、数字、下划线或连字符。'
    ADMIN_USER=
  done
  while :; do
    if [ -n "$ADMIN_PASSWORD" ]; then password=$ADMIN_PASSWORD; else password=$(read_value '面板管理员密码（至少 12 位）：' '' 1); fi
    [ "${#password}" -ge 12 ] || { warn '密码至少需要 12 位。'; ADMIN_PASSWORD=; continue; }
    if [ "$NONINTERACTIVE" = 1 ]; then ADMIN_PASSWORD=$password; break; fi
    confirm=$(read_value '再次输入管理员密码：' '' 1)
    [ "$password" = "$confirm" ] || { warn '两次密码不一致，请重试。'; ADMIN_PASSWORD=; continue; }
    ADMIN_PASSWORD=$password
    break
  done
  ok '面板访问参数已保存。'
}

copy_source(){
  step '1 / 6' '检查系统并准备运行环境'
  need_root
  ARCH=$(uname -m)
  case "$ARCH" in x86_64|aarch64) ;; *) fail "暂不支持架构：$ARCH";; esac
  [ -r /etc/os-release ] || fail '无法识别操作系统。'
  . /etc/os-release
  case "${ID:-}" in alpine|debian|ubuntu) ;; *) fail "仅支持 Alpine、Debian 和 Ubuntu，当前为：${ID:-unknown}";; esac
  install_runtime
  command -v node >/dev/null 2>&1 || fail 'Node.js 安装失败。'
  NODE_MAJOR=$(node -p 'process.versions.node.split(".")[0]')
  [ "$NODE_MAJOR" -ge 18 ] 2>/dev/null || fail "Node.js 版本过低：$NODE_MAJOR。"
  mkdir -p "$PREFIX" "$DATA_DIR" "$BIN_DIR"
  chmod 700 "$DATA_DIR"
  if [ -n "${KOTAUI_SOURCE_DIR:-}" ]; then
    SRC=$KOTAUI_SOURCE_DIR
    [ -f "$SRC/server/index.mjs" ] || fail "源码目录无效：$SRC"
    rm -rf "$PREFIX/server" "$PREFIX/public" "$PREFIX/service" "$PREFIX/proto" "$PREFIX/tools"
    cp -R "$SRC/server" "$SRC/public" "$SRC/service" "$SRC/proto" "$SRC/tools" "$PREFIX/"
    cp "$SRC/package.json" "$PREFIX/package.json"
    [ -f "$SRC/package-lock.json" ] && cp "$SRC/package-lock.json" "$PREFIX/package-lock.json" || true
  else
    TMP=$(mktemp -d)
    trap 'cleanup_build_swap; rm -rf "$TMP"' EXIT INT TERM
    git clone --depth 1 "$REPO_URL" "$TMP/src" >/dev/null 2>&1 || fail '无法下载 KotaUI 源码。'
    KOTAUI_SOURCE_DIR="$TMP/src" copy_source
    return
  fi
  ( cd "$PREFIX" && if [ -f package-lock.json ]; then npm ci --omit=dev --no-audit --no-fund; else npm install --omit=dev --no-audit --no-fund; fi ) >/dev/null
  ok "环境已就绪：$ID / $ARCH。"
}

write_runtime_files(){
  umask 077
  mkdir -p "$PREFIX/bin" "$DATA_DIR/sing-box"
  printf 'KOTAUI_ADMIN_USER=%s\nKOTAUI_ADMIN_PASSWORD=%s\n' "$ADMIN_USER" "$ADMIN_PASSWORD" > "$DATA_DIR/admin.env"
  printf 'KOTAUI_PORT=%s\nKOTAUI_PANEL_PATH=/%s\nKOTAUI_SUBSCRIPTION_PORT=%s\nKOTAUI_STATS_PORT=%s\nKOTAUI_DOMAIN=%s\nKOTAUI_HOST=0.0.0.0\nKOTAUI_MANAGE_SERVICES=1\nKOTAUI_DATA_DIR=%s\nKOTAUI_ADMIN_ENV=%s/admin.env\nKOTAUI_SINGBOX_CONFIG=%s/sing-box/config.json\nKOTAUI_TLS_CERT=%s/certs/fullchain.pem\nKOTAUI_TLS_KEY=%s/certs/privkey.pem\nNODE_OPTIONS=--max-old-space-size=96\n' "$PANEL_PORT" "$PANEL_PATH" "$SUB_PORT" "$STATS_PORT" "$CERT_SUBJECT" "$DATA_DIR" "$DATA_DIR" "$DATA_DIR" "$DATA_DIR" "$DATA_DIR" > "$DATA_DIR/runtime.env"
  cat > "$PREFIX/bin/kotaui-run" <<EOF
#!/bin/sh
set -eu
set -a
. "$DATA_DIR/runtime.env"
set +a
exec /usr/bin/node --max-old-space-size=96 "$PREFIX/server/index.mjs"
EOF
  cat > "$PREFIX/bin/kotaui-singbox-check" <<EOF
#!/bin/sh
set -eu
set -a
. "$DATA_DIR/runtime.env"
set +a
exec "$BIN_DIR/sing-box" check -c "$DATA_DIR/sing-box/config.json"
EOF
  cat > "$PREFIX/bin/kotaui-singbox-run" <<EOF
#!/bin/sh
set -eu
set -a
. "$DATA_DIR/runtime.env"
set +a
exec "$BIN_DIR/sing-box" run -c "$DATA_DIR/sing-box/config.json"
EOF
  printf '{"log":{"level":"info"},"inbounds":[],"outbounds":[{"type":"direct","tag":"direct"}],"experimental":{"v2ray_api":{"listen":"127.0.0.1:%s","stats":{"enabled":true,"inbounds":[],"outbounds":[],"users":[]}}}}\n' "$STATS_PORT" > "$DATA_DIR/sing-box/config.json"
  chmod 700 "$PREFIX/bin/kotaui-run" "$PREFIX/bin/kotaui-singbox-check" "$PREFIX/bin/kotaui-singbox-run"
  chmod 600 "$DATA_DIR/admin.env" "$DATA_DIR/runtime.env" "$DATA_DIR/sing-box/config.json"
}

install_services(){
  step '5 / 6' '注册低资源服务与自动续签'
  write_runtime_files
  install -m 700 "$PREFIX/service/kota-cert-renew.sh" "$BIN_DIR/kota-cert-renew"
  install -m 700 "$PREFIX/service/kota-menu.sh" "$BIN_DIR/kota"
  install -m 644 "$PREFIX/service/kotaui.service" /etc/systemd/system/kotaui.service 2>/dev/null || true
  install -m 644 "$PREFIX/service/kotaui-singbox.service" /etc/systemd/system/kotaui-singbox.service 2>/dev/null || true
  install -m 644 "$PREFIX/service/kota-cert-renew.service" /etc/systemd/system/kota-cert-renew.service 2>/dev/null || true
  install -m 644 "$PREFIX/service/kota-cert-renew.timer" /etc/systemd/system/kota-cert-renew.timer 2>/dev/null || true
  if [ "$ID" = alpine ]; then
    mkdir -p /etc/init.d /etc/conf.d /etc/periodic/6hourly
    install -m 755 "$PREFIX/service/kotaui.openrc" /etc/init.d/kotaui
    install -m 755 "$PREFIX/service/kotaui-singbox.openrc" /etc/init.d/kotaui-singbox
    printf 'KOTAUI_RUNTIME_ENV="%s/runtime.env"\n' "$DATA_DIR" > /etc/conf.d/kotaui
    cp "$BIN_DIR/kota-cert-renew" /etc/periodic/6hourly/kota-cert-renew
    chmod 700 /etc/periodic/6hourly/kota-cert-renew
    rc-update add kotaui-singbox default >/dev/null 2>&1 || true
    rc-update add kotaui default >/dev/null 2>&1 || true
    rc-service kotaui-singbox start >/dev/null
    rc-service kotaui start >/dev/null
  else
    systemctl daemon-reload
    systemctl enable --now kotaui-singbox.service
    systemctl enable --now kotaui.service
    systemctl enable --now kota-cert-renew.timer
  fi
  ok 'KotaUI、sing-box 与证书自动续签服务均已启用。'
}

configure_firewall(){
  [ "$CONFIGURE_FIREWALL" = 1 ] || return 0
  if command -v ufw >/dev/null 2>&1; then
    ufw allow "$PANEL_PORT/tcp" >/dev/null
    ufw allow "$SUB_PORT/tcp" >/dev/null
    ok "已开放面板 TCP $PANEL_PORT 与订阅 TCP $SUB_PORT。"
  else
    warn '未检测到 UFW，未自动修改防火墙；请手动放行面板、订阅和节点端口。'
  fi
}

final_screen(){
  clear_screen
  printf '%s\n' '╔════════════════════ 安装完成 · 请保存以下信息 ════════════════════╗'
  printf '%s\n' '║                                                                    ║'
  printf '%s\n' '║  KotaUI 管理地址                                                   ║'
  printf '║  https://%s:%s/%s\n' "$CERT_SUBJECT" "$PANEL_PORT" "$PANEL_PATH"
  printf '%s\n' '║                                                                    ║'
  printf '║  管理员账号：%s\n' "$ADMIN_USER"
  printf '║  管理员密码：%s\n' "$ADMIN_PASSWORD"
  printf '%s\n' '║                                                                    ║'
  printf '%s\n' '║  证书状态：已签发，自动续签已启用                                  ║'
  printf '%s\n' '║  服务状态：KotaUI 与 sing-box 已启动                               ║'
  printf '%s\n' '║                                                                    ║'
  printf '%s\n' '║  重要：请立即保存地址、账号和密码。初始密码仅在此显示一次。       ║'
  printf '%s\n' '╚════════════════════════════════════════════════════════════════════╝'
  printf '\n使用 kota 可随时打开本机运维菜单。\n'
}

welcome
copy_source
configure_certificate
configure_panel
install_singbox
install_services
configure_firewall
step '6 / 6' '完成服务健康检查'
attempt=0
until "$BIN_DIR/kota" check >/dev/null 2>&1; do
  attempt=$((attempt + 1))
  [ "$attempt" -lt 15 ] || fail 'KotaUI 健康检查失败，请执行 kota 查看状态和日志。'
  sleep 1
done
ok '健康检查通过。'
final_screen
