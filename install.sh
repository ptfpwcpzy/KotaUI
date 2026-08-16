#!/bin/sh
set -eu

PREFIX=${KOTAUI_PREFIX:-/opt/kotaui}
DATA_DIR=${KOTAUI_DATA_DIR:-/var/lib/kotaui}
BIN_DIR=${KOTAUI_BIN_DIR:-/usr/local/bin}
REPO_URL=${KOTAUI_REPO_URL:-https://github.com/ptfpwcpzy/KotaUItest.git}
PANEL_PORT=${KOTAUI_PANEL_PORT:-1108}
SUB_PORT=${KOTAUI_SUBSCRIPTION_PORT:-1109}
STATS_PORT=${KOTAUI_STATS_PORT:-9090}
CONFIGURE_FIREWALL=${KOTAUI_CONFIGURE_FIREWALL:-0}
MANAGE_FIREWALL=${KOTAUI_MANAGE_FIREWALL:-$CONFIGURE_FIREWALL}
INSTALL_SINGBOX=${KOTAUI_INSTALL_SINGBOX:-1}
BUILD_SINGBOX_V2RAY_API=${KOTAUI_BUILD_SINGBOX_V2RAY_API:-1}
SINGBOX_VERSION=${KOTAUI_SINGBOX_VERSION:-v1.13.18}
ISSUE_CERT=${KOTAUI_ISSUE_CERT:-0}
CERT_DOMAIN=${KOTAUI_CERT_DOMAIN:-}
CERT_EMAIL=${KOTAUI_CERT_EMAIL:-}
ADMIN_USER=${KOTAUI_ADMIN_USER:-admin}
ADMIN_PASSWORD=${KOTAUI_ADMIN_PASSWORD:-}
log(){ printf '[kota] %s\n' "$*"; }
fail(){ printf '[kota] ERROR: %s\n' "$*" >&2; exit 1; }
[ "$(id -u)" -eq 0 ] || fail '请以 root 身份运行安装脚本。'
ARCH=$(uname -m)
case "$ARCH" in x86_64|aarch64) ;; *) fail "暂不支持架构: $ARCH";; esac
if [ -r /etc/os-release ]; then . /etc/os-release; else fail '无法识别操作系统。'; fi
case "${ID:-}" in alpine|debian|ubuntu) ;; *) fail "暂不支持系统: ${ID:-unknown}";; esac
valid_port(){ case "$1" in ''|*[!0-9]*) return 1;; esac; [ "$1" -ge 1024 ] && [ "$1" -le 65535 ]; }
valid_port "$PANEL_PORT" || fail "面板端口无效: $PANEL_PORT"
case "$ADMIN_USER" in ''|*[!A-Za-z0-9_-]*) fail '管理员账号只能包含字母、数字、下划线和连字符。';; esac
if [ -z "$ADMIN_PASSWORD" ]; then ADMIN_PASSWORD=$(openssl rand -base64 24 | tr -dc 'A-Za-z0-9_-' | cut -c1-20); fi
[ "${#ADMIN_PASSWORD}" -ge 12 ] || fail '管理员密码至少需要 12 个字符。'
valid_port "$SUB_PORT" || fail "订阅端口无效: $SUB_PORT"
[ "$PANEL_PORT" != "$SUB_PORT" ] || fail '面板端口和订阅端口不能相同。'
install_runtime(){
  case "${ID:-}" in
    alpine) apk add --no-cache nodejs npm git openssl certbot curl ca-certificates >/dev/null ;;
    debian|ubuntu) export DEBIAN_FRONTEND=noninteractive; apt-get update -qq; apt-get install -y -qq nodejs npm git openssl certbot curl ca-certificates gnupg systemd-standalone-sysusers >/dev/null ;;
  esac
}
install_singbox(){
  [ "$INSTALL_SINGBOX" = 1 ] || return 0
  if [ "$BUILD_SINGBOX_V2RAY_API" = 1 ]; then
    command -v go >/dev/null 2>&1 || case "${ID:-}" in alpine) apk add --no-cache go >/dev/null ;; debian|ubuntu) export DEBIAN_FRONTEND=noninteractive; apt-get update -qq; apt-get install -y -qq golang >/dev/null ;; esac
    SINGBOX_VERSION="$SINGBOX_VERSION" SINGBOX_PREFIX=/usr/local/bin sh "$PREFIX/tools/build-singbox-v2ray.sh"
    return 0
  fi
  command -v sing-box >/dev/null 2>&1 && return 0
  case "${ID:-}" in
    alpine) apk add --no-cache sing-box >/dev/null || fail 'Alpine 软件源未提供 sing-box，请启用官方 edge/community 源。' ;;
    debian|ubuntu)
      install -d -m 755 /etc/apt/keyrings
      curl -fsSL https://sing-box.app/gpg.key -o /etc/apt/keyrings/sagernet.asc
      chmod a+r /etc/apt/keyrings/sagernet.asc
      printf '%s\n' 'Types: deb' 'URIs: https://deb.sagernet.org/' 'Suites: *' 'Components: *' 'Enabled: yes' 'Signed-By: /etc/apt/keyrings/sagernet.asc' > /etc/apt/sources.list.d/sagernet.sources
      export DEBIAN_FRONTEND=noninteractive
      apt-get update -qq
      apt-get install -y -qq sing-box >/dev/null
      ;;
  esac
  command -v sing-box >/dev/null 2>&1 || fail 'sing-box 安装失败。'
}

command -v node >/dev/null 2>&1 || install_runtime
command -v npm >/dev/null 2>&1 || install_runtime
command -v git >/dev/null 2>&1 || install_runtime
command -v openssl >/dev/null 2>&1 || install_runtime
command -v node >/dev/null 2>&1 || fail 'Node.js 安装失败。'
command -v npm >/dev/null 2>&1 || fail 'npm 安装失败。'
NODE_MAJOR=$(node -p 'process.versions.node.split(".")[0]')
[ "$NODE_MAJOR" -ge 18 ] 2>/dev/null || fail "Node.js 版本过低: $NODE_MAJOR"

mkdir -p "$PREFIX/server" "$PREFIX/public" "$PREFIX/service" "$PREFIX/proto" "$DATA_DIR" "$BIN_DIR"
umask 077
printf 'KOTAUI_ADMIN_USER=%s\nKOTAUI_ADMIN_PASSWORD=%s\n' "$ADMIN_USER" "$ADMIN_PASSWORD" > "$DATA_DIR/admin.env"
chmod 600 "$DATA_DIR/admin.env"
if [ -n "${KOTAUI_SOURCE_DIR:-}" ]; then
  SRC=$KOTAUI_SOURCE_DIR
  [ -f "$SRC/server/index.mjs" ] || fail "源码目录无效: $SRC"
  cp -R "$SRC/server/." "$PREFIX/server/"
  cp -R "$SRC/public/." "$PREFIX/public/"
  cp -R "$SRC/service/." "$PREFIX/service/"
  cp -R "$SRC/proto/." "$PREFIX/proto/"
  mkdir -p "$PREFIX/tools"; cp -R "$SRC/tools/." "$PREFIX/tools/"
  cp "$SRC/package.json" "$PREFIX/package.json"
else
  command -v git >/dev/null 2>&1 || fail '未发现 git，无法下载源码。'
  TMP=$(mktemp -d)
  trap 'rm -rf "$TMP"' EXIT
  git clone --depth 1 "$REPO_URL" "$TMP/src" >/dev/null 2>&1
  cp -R "$TMP/src/server/." "$PREFIX/server/"
  cp -R "$TMP/src/public/." "$PREFIX/public/"
  cp -R "$TMP/src/service/." "$PREFIX/service/"
  cp -R "$TMP/src/proto/." "$PREFIX/proto/"
  mkdir -p "$PREFIX/tools"; cp -R "$TMP/src/tools/." "$PREFIX/tools/"
  cp "$TMP/src/package.json" "$PREFIX/package.json"
fi
install_singbox
chmod 700 "$DATA_DIR"
( cd "$PREFIX" && npm install --omit=dev --no-audit --no-fund >/dev/null )
if [ "$ISSUE_CERT" = 1 ]; then
  [ -n "$CERT_DOMAIN" ] || fail 'KOTAUI_ISSUE_CERT=1 时必须设置 KOTAUI_CERT_DOMAIN。'
  mkdir -p "$DATA_DIR/certs"
  CERT_ARGS="certonly --standalone --non-interactive --agree-tos --keep-until-expiring -d $CERT_DOMAIN"
  if [ -n "$CERT_EMAIL" ]; then CERT_ARGS="$CERT_ARGS --email $CERT_EMAIL"; else CERT_ARGS="$CERT_ARGS --register-unsafely-without-email"; fi
  case "$CERT_DOMAIN" in *[!0-9.]*|*:*) ;; *) CERT_ARGS="$CERT_ARGS --cert-profile shortlived";; esac
  log "申请 ACME 证书，standalone 校验需要临时占用 TCP 80。"
  # 只为证书校验临时放行 80，完成后撤销临时规则。
  if [ "$CONFIGURE_FIREWALL" = 1 ] && command -v ufw >/dev/null 2>&1; then ufw allow 80/tcp >/dev/null; fi
  sh -c "certbot $CERT_ARGS"
  ln -sfn "/etc/letsencrypt/live/$CERT_DOMAIN/fullchain.pem" "$DATA_DIR/certs/fullchain.pem"
  ln -sfn "/etc/letsencrypt/live/$CERT_DOMAIN/privkey.pem" "$DATA_DIR/certs/privkey.pem"
  umask 077; printf 'KOTAUI_CERT_DOMAIN=%s\\n' "$CERT_DOMAIN" > "$DATA_DIR/certificate.env"
  if [ "$CONFIGURE_FIREWALL" = 1 ] && command -v ufw >/dev/null 2>&1; then ufw delete allow 80/tcp >/dev/null || true; fi
fi
install -m 700 "$PREFIX/service/kota-cert-renew.sh" /usr/local/bin/kota-cert-renew
if [ "${ID:-}" = alpine ]; then
  mkdir -p /etc/periodic/6hourly
  cp /usr/local/bin/kota-cert-renew /etc/periodic/6hourly/kota-cert-renew
  chmod 700 /etc/periodic/6hourly/kota-cert-renew
fi
# 让服务进程读取指定面板端口，并保留统计 API 仅监听回环地址。
if [ "${ID:-}" = alpine ]; then
  mkdir -p /etc/init.d
  sed "s#environment=\"NODE_ENV=production KOTAUI_ADMIN_ENV=/var/lib/kotaui/admin.env KOTAUI_DATA_DIR=/var/lib/kotaui KOTAUI_SINGBOX_CONFIG=/var/lib/kotaui/sing-box/config.json\"#environment=\"NODE_ENV=production KOTAUI_ADMIN_ENV=/var/lib/kotaui/admin.env KOTAUI_PORT=$PANEL_PORT KOTAUI_SUBSCRIPTION_PORT=$SUB_PORT KOTAUI_STATS_PORT=$STATS_PORT KOTAUI_MANAGE_FIREWALL=$MANAGE_FIREWALL KOTAUI_DATA_DIR=/var/lib/kotaui KOTAUI_SINGBOX_CONFIG=/var/lib/kotaui/sing-box/config.json\"#" "$PREFIX/service/kotaui.openrc" > /etc/init.d/kotaui
  chmod 755 /etc/init.d/kotaui
  rc-update add kotaui default >/dev/null 2>&1 || true
else
  sed "s#Environment=NODE_ENV=production#Environment=NODE_ENV=production\\nEnvironment=KOTAUI_PORT=$PANEL_PORT\\nEnvironment=KOTAUI_SUBSCRIPTION_PORT=$SUB_PORT\\nEnvironment=KOTAUI_MANAGE_FIREWALL=$MANAGE_FIREWALL#" "$PREFIX/service/kotaui.service" > /etc/systemd/system/kotaui.service
  systemctl daemon-reload >/dev/null 2>&1 || true
  systemctl enable kotaui >/dev/null 2>&1 || true
  cp "$PREFIX/service/kota-cert-renew.service" /etc/systemd/system/kota-cert-renew.service
  cp "$PREFIX/service/kota-cert-renew.timer" /etc/systemd/system/kota-cert-renew.timer
  systemctl daemon-reload >/dev/null 2>&1 || true
  systemctl enable kota-cert-renew.timer >/dev/null 2>&1 || true
fi
# 只在用户显式启用时修改防火墙；统计 API 9090 永不开放公网。
if [ "$CONFIGURE_FIREWALL" = 1 ]; then
  if command -v ufw >/dev/null 2>&1; then
    ufw allow "$PANEL_PORT/tcp" >/dev/null
    ufw allow "$SUB_PORT/tcp" >/dev/null
    log "已通过 ufw 仅开放面板 TCP $PANEL_PORT 和订阅 TCP $SUB_PORT；节点端口需按入站配置追加。"
  elif command -v nft >/dev/null 2>&1; then
    log '检测到 nftables；未自动改写现有规则，请按实际节点端口加入面板和订阅白名单。'
  else
    log '未检测到 ufw/nftables，未修改系统防火墙。'
  fi
fi
cat > "$BIN_DIR/kota" <<EOF
#!/bin/sh
set -eu
[ "\$(id -u)" -eq 0 ] || { printf '%s\\n' 'kota 必须以 root 身份运行。' >&2; exit 1; }
PREFIX="$PREFIX"
DATA_DIR="$DATA_DIR"
case "\${1:-menu}" in
  info) printf 'KotaUI data: %s\\n' "\$DATA_DIR"; printf 'KotaUI install: %s\\n' "\$PREFIX"; [ -f "\$DATA_DIR/state.json" ] && sed -n '1,160p' "\$DATA_DIR/state.json" || printf '尚未初始化状态文件。\\n' ;;
  start) if command -v systemctl >/dev/null 2>&1 && systemctl is-enabled kotaui >/dev/null 2>&1; then systemctl start kotaui; else rc-service kotaui start 2>/dev/null || exec env KOTAUI_DATA_DIR="\$DATA_DIR" KOTAUI_PORT="$PANEL_PORT" node "\$PREFIX/server/index.mjs"; fi ;;
  stop) if command -v systemctl >/dev/null 2>&1 && systemctl is-enabled kotaui >/dev/null 2>&1; then systemctl stop kotaui; else rc-service kotaui stop 2>/dev/null || true; fi ;;
  restart) if command -v systemctl >/dev/null 2>&1 && systemctl is-enabled kotaui >/dev/null 2>&1; then systemctl restart kotaui; else rc-service kotaui restart 2>/dev/null || true; fi ;;
  check) exec env KOTAUI_DATA_DIR="\$DATA_DIR" KOTAUI_PORT="$PANEL_PORT" node -e "fetch('http://127.0.0.1:$PANEL_PORT/api/health').then(r=>r.json()).then(x=>console.log(JSON.stringify(x))).catch(()=>process.exit(1))" ;;
  logs) if command -v journalctl >/dev/null 2>&1; then journalctl -u kotaui -n 100 --no-pager; else tail -n 100 /var/log/kotaui.log 2>/dev/null || true; fi ;;
  uninstall) printf '卸载需要二次确认。输入 REMOVE-KOTAUI 继续: '; read answer; [ "\$answer" = REMOVE-KOTAUI ] || exit 1; if command -v systemctl >/dev/null 2>&1; then systemctl disable --now kotaui >/dev/null 2>&1 || true; rm -f /etc/systemd/system/kotaui.service; systemctl daemon-reload >/dev/null 2>&1 || true; else rc-update del kotaui default >/dev/null 2>&1 || true; rm -f /etc/init.d/kotaui; fi; rm -rf "\$PREFIX" "\$DATA_DIR" "\$0"; printf 'KotaUI 已卸载。\\n' ;;
  *) printf '%s\\n' '用法: kota {info|start|stop|restart|check|logs|uninstall}';;
esac
EOF
chmod 755 "$BIN_DIR/kota"
if [ "${ID:-}" = alpine ]; then rc-service kotaui start >/dev/null 2>&1 || true; else systemctl restart kotaui >/dev/null 2>&1 || true; fi
log "安装完成: $PREFIX"
log "唤醒菜单: kota"
log "服务端口: $PANEL_PORT；订阅端口: $SUB_PORT"
log "管理员账号: $ADMIN_USER"
log "管理员密码: $ADMIN_PASSWORD"
log "统计 API 仅监听 127.0.0.1:$STATS_PORT"
