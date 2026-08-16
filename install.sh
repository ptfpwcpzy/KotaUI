#!/bin/sh
set -eu

PREFIX=${KOTAUI_PREFIX:-/opt/kotaui}
DATA_DIR=${KOTAUI_DATA_DIR:-/var/lib/kotaui}
BIN_DIR=${KOTAUI_BIN_DIR:-/usr/local/bin}
REPO_URL=${KOTAUI_REPO_URL:-https://github.com/ptfpwcpzy/KotaUI.git}

log(){ printf '[kotaui] %s\n' "$*"; }
fail(){ printf '[kotaui] ERROR: %s\n' "$*" >&2; exit 1; }
[ "$(id -u)" -eq 0 ] || fail '请以 root 身份运行安装脚本。'
ARCH=$(uname -m)
case "$ARCH" in x86_64|aarch64) ;; *) fail "暂不支持架构: $ARCH";; esac
if [ -r /etc/os-release ]; then . /etc/os-release; else fail '无法识别操作系统。'; fi
case "${ID:-}" in alpine|debian|ubuntu) ;; *) fail "暂不支持系统: ${ID:-unknown}";; esac
install_runtime(){
  case "${ID:-}" in
    alpine) apk add --no-cache nodejs npm git >/dev/null ;;
    debian|ubuntu) export DEBIAN_FRONTEND=noninteractive; apt-get update -qq; apt-get install -y -qq nodejs npm git >/dev/null ;;
  esac
}
command -v node >/dev/null 2>&1 || install_runtime
command -v git >/dev/null 2>&1 || install_runtime
command -v node >/dev/null 2>&1 || fail 'Node.js 安装失败。'
NODE_MAJOR=$(node -p 'process.versions.node.split(".")[0]')
[ "$NODE_MAJOR" -ge 18 ] 2>/dev/null || fail "Node.js 版本过低: $NODE_MAJOR"

mkdir -p "$PREFIX/server" "$PREFIX/public" "$DATA_DIR"
if [ -n "${KOTAUI_SOURCE_DIR:-}" ]; then
  SRC=$KOTAUI_SOURCE_DIR
  [ -f "$SRC/server/index.mjs" ] || fail "源码目录无效: $SRC"
  cp -R "$SRC/server/." "$PREFIX/server/"
  cp -R "$SRC/public/." "$PREFIX/public/"
  cp "$SRC/package.json" "$PREFIX/package.json"
else
  command -v git >/dev/null 2>&1 || fail '未发现 git，无法下载源码。'
  TMP=$(mktemp -d)
  trap 'rm -rf "$TMP"' EXIT
  git clone --depth 1 "$REPO_URL" "$TMP/src" >/dev/null 2>&1
  cp -R "$TMP/src/server/." "$PREFIX/server/"
  cp -R "$TMP/src/public/." "$PREFIX/public/"
  cp "$TMP/src/package.json" "$PREFIX/package.json"
fi
chmod 700 "$DATA_DIR"
cat > "$BIN_DIR/kota" <<EOF
#!/bin/sh
set -eu
[ "\$(id -u)" -eq 0 ] || { printf '%s\\n' 'kota 必须以 root 身份运行。' >&2; exit 1; }
PREFIX="$PREFIX"
DATA_DIR="$DATA_DIR"
case "\${1:-menu}" in
  info) printf 'KotaUI data: %s\n' "\$DATA_DIR"; printf 'KotaUI install: %s\n' "\$PREFIX"; [ -f "\$DATA_DIR/state.json" ] && sed -n '1,120p' "\$DATA_DIR/state.json" || printf '尚未初始化状态文件。\n' ;;
  start) exec env KOTAUI_DATA_DIR="\$DATA_DIR" node "\$PREFIX/server/index.mjs";;
  check) exec env KOTAUI_DATA_DIR="\$DATA_DIR" node -e "fetch('http://127.0.0.1:1108/api/health').then(r=>r.json()).then(x=>console.log(JSON.stringify(x))).catch(()=>process.exit(1))";;
  uninstall) printf '卸载需要二次确认。输入 REMOVE-KOTAUI 继续: '; read answer; [ "\$answer" = REMOVE-KOTAUI ] || exit 1; rm -rf "\$PREFIX" "\$DATA_DIR" "\$0"; printf 'KotaUI 已卸载。\n';;
  *) printf '%s\n' '用法: kota {info|start|check|uninstall}';;
esac
EOF
chmod 755 "$BIN_DIR/kota"
log "安装完成: $PREFIX"
log "唤醒菜单: kota"
log "启动方式: kota start"
log "状态检查: kota check"
