#!/bin/sh
set -eu

# ---------------- 样式 ----------------
if [ -t 2 ] && [ -z "${NO_COLOR:-}" ]; then
  TTY=1
  _e=$(printf '\033')
  C_RST="${_e}[0m"; C_BOLD="${_e}[1m"; C_DIM="${_e}[2m"
  C_GREEN="${_e}[32m"; C_CYAN="${_e}[36m"; C_YELLOW="${_e}[33m"; C_RED="${_e}[31m"
  C_B1="${_e}[38;5;87m"; C_B2="${_e}[38;5;81m"; C_B3="${_e}[38;5;75m"
  C_B4="${_e}[38;5;99m"; C_B5="${_e}[38;5;141m"; C_B6="${_e}[38;5;177m"
  unset _e
else
  C_RST=''; C_BOLD=''; C_DIM=''
  C_GREEN=''; C_CYAN=''; C_YELLOW=''; C_RED=''
  C_B1=''; C_B2=''; C_B3=''; C_B4=''; C_B5=''; C_B6=''
  TTY=0
fi

# ---------------- Banner ----------------
banner_k='██   ██
██  ██ 
████   
████   
██  ██ 
██   ██'
banner_o=' ██████ 
██    ██
██    ██
██    ██
██    ██
 ██████ '
banner_t='████████
   ██   
   ██   
   ██   
   ██   
   ██   '
banner_a='   ██   
  ████  
 ██  ██ 
████████
██    ██
██    ██'
banner_u='██    ██
██    ██
██    ██
██    ██
██    ██
 ██████ '
banner_i='████████
   ██   
   ██   
   ██   
   ██   
████████'
banner_art(){
  case "$1" in
    k) printf '%s\n' "$banner_k";;
    o) printf '%s\n' "$banner_o";;
    t) printf '%s\n' "$banner_t";;
    a) printf '%s\n' "$banner_a";;
    u) printf '%s\n' "$banner_u";;
    i) printf '%s\n' "$banner_i";;
  esac
}
print_banner(){
  cols=$(tput cols 2>/dev/null || echo 80)
  case "$cols" in ''|*[!0-9]*) cols=80;; esac
  indent=2
  if [ "$cols" -gt 63 ]; then indent=$(( (cols - 59) / 2 )); fi
  pad=$(printf '%*s' "$indent" '')
  row=1
  while [ "$row" -le 6 ]; do
    line="$pad"
    for L in k o t a u i; do
      ln=$(banner_art "$L" | sed -n "${row}p")
      case "$L" in
        k) c=$C_B1;; o) c=$C_B2;; t) c=$C_B3;;
        a) c=$C_B4;; u) c=$C_B5;; i) c=$C_B6;;
      esac
      line="${line}${c}${ln}${C_RST}  "
    done
    printf '%s\n' "$line" >&2
    row=$((row + 1))
  done
  printf '%s%s               轻量 sing-box 管理面板%s\n' "$pad" "$C_DIM" "$C_RST" >&2
  printf '%s%s                 作者 · 那么羡慕你%s\n' "$pad" "$C_DIM" "$C_RST" >&2
}

# ---------------- 步骤 / 任务 ----------------
clear_screen(){ command -v clear >/dev/null 2>&1 && clear || printf '\033c'; }
fail(){ printf '  %s✗ %s%s\n' "$C_RED" "$1" "$C_RST" >&2; exit 1; }
ask(){ printf '%s' "$1" >&2; IFS= read -r answer || true; printf '%s' "$answer"; }

# 交互式步骤：开始 / 完成（含结果摘要）
istep(){ printf '\n  %s→%s %s%s %s%s\n' "$C_CYAN" "$C_RST" "$C_BOLD" "$1" "$2" "$C_RST" >&2; }
idone(){ printf '  %s✓%s %s%s %s%s %s· %s%s\n' "$C_GREEN" "$C_RST" "$C_BOLD" "$1" "$2" "$C_RST" "$C_DIM" "$3" "$C_RST" >&2; }
warn(){ printf '    %s%s%s\n' "$C_YELLOW" "$1" "$C_RST" >&2; }

# 长步骤：开始（不换行，首个任务刷新会复用本行）/ 结束（清行并打印 ✓ + 用时）
S_NAME=''; S_START=0
step_begin(){
  S_NAME="$1 $2"; S_START=$(date +%s)
  if [ "$TTY" = 1 ]; then
    printf '  %s→%s %s%s%s' "$C_CYAN" "$C_RST" "$C_BOLD" "$S_NAME" "$C_RST" >&2
  else
    printf '  → %s\n' "$S_NAME" >&2
  fi
}
step_end(){
  el=$(($(date +%s) - S_START))
  if [ "$TTY" = 1 ]; then
    printf '\r\033[2K  %s✓%s %s%s%s %s(用时 %ss)%s\n' "$C_GREEN" "$C_RST" "$C_BOLD" "$S_NAME" "$C_RST" "$C_DIM" "$el" "$C_RST" >&2
  else
    printf '  ✓ %s (用时 %ss)\n' "$S_NAME" "$el" >&2
  fi
}
sub_ok(){ printf '    %s✓%s %s\n' "$C_GREEN" "$C_RST" "$1" >&2; }
sub_note(){ printf '    %s%s%s\n' "$C_DIM" "$1" "$C_RST" >&2; }

progress(){
  n=${1%%/*}
  filled=$((n * 3)); bar=""; i=0
  while [ "$i" -lt 18 ]; do
    if [ "$i" -lt "$filled" ]; then bar="${bar}█"; else bar="${bar}░"; fi
    i=$((i + 1))
  done
  printf '\n  %s安装进度%s %s%s%s %s%s%s\n' "$C_DIM" "$C_RST" "$C_CYAN" "$bar" "$C_RST" "$C_BOLD" "$1" "$C_RST" >&2
}

# 长任务：后台执行，前台 spinner + 已用时间；失败时打印尾部日志并保留日志文件
task(){
  desc=$1; shift
  log=$(mktemp); rc=""; start=$(date +%s)
  if [ "$TTY" = 0 ]; then printf '    … %s\n' "$desc" >&2; fi
  "$@" >"$log" 2>&1 &
  pid=$!
  if [ "$TTY" = 1 ]; then
  i=0
  while kill -0 "$pid" 2>/dev/null; do
    i=$((i + 1))
    case $((i % 4)) in
      0) f='|';; 1) f='/';; 2) f='-';;
      3) f='\';; 
    esac
    el=$(($(date +%s) - start))
    printf '\r\033[2K    %s%s%s %s %s(%ss)%s' "$C_YELLOW" "$f" "$C_RST" "$desc" "$C_DIM" "$el" "$C_RST" >&2
    sleep 0.3
  done
  fi
  wait "$pid" || rc=$?
  rc=${rc:-0}
  el=$(($(date +%s) - start))
  if [ "$rc" -eq 0 ]; then
    if [ "$TTY" = 1 ]; then
      printf '\r\033[2K    %s✓%s %s %s(用时 %ss)%s\n' "$C_GREEN" "$C_RST" "$desc" "$C_DIM" "$el" "$C_RST" >&2
    else
      printf '    ✓ %s (用时 %ss)\n' "$desc" "$el" >&2
    fi
    rm -f "$log"
  else
    if [ "$TTY" = 1 ]; then
      printf '\r\033[2K    %s✗%s %s 失败 %s(用时 %ss)%s\n' "$C_RED" "$C_RST" "$desc" "$C_DIM" "$el" "$C_RST" >&2
    else
      printf '    ✗ %s 失败 (用时 %ss)\n' "$desc" "$el" >&2
    fi
    tail -n 20 "$log" >&2
    printf '    %s完整日志保留在：%s%s\n' "$C_DIM" "$log" "$C_RST" >&2
    return "$rc"
  fi
}

# ---------------- 基础 ----------------
prepare_go_cache() {
  install -d -m 700 /var/tmp/kotaui-build
  export TMPDIR=/var/tmp/kotaui-build
  export GOTMPDIR=/var/tmp/kotaui-build
  if [ -z "${HOME:-}" ] || [ "$HOME" = / ]; then export HOME=/root; fi
  # Alpine often mounts /var/tmp on tmpfs. A module cache there is discarded,
  # so every update downloads modules and recompiles. Reuse the persistent
  # cache under $HOME instead.
  unset GOPATH GOMODCACHE GOCACHE
  install -d -m 700 "$HOME/go/pkg/mod" "$HOME/.cache/go-build"
}

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

welcome(){
  clear_screen
  print_banner
  printf '\n  本次安装将依次完成：\n' >&2
  printf '    %s1/6%s 选择证书类型（域名 / IP）\n' "$C_BOLD" "$C_RST" >&2
  printf '    %s2/6%s 设置面板端口与访问路径\n' "$C_BOLD" "$C_RST" >&2
  printf '    %s3/6%s 设置管理员账号与密码\n' "$C_BOLD" "$C_RST" >&2
  printf '    %s4/6%s 安装运行环境\n' "$C_BOLD" "$C_RST" >&2
  printf '    %s5/6%s 构建面板与核心\n' "$C_BOLD" "$C_RST" >&2
  printf '    %s6/6%s 申请证书、启动服务\n' "$C_BOLD" "$C_RST" >&2
  printf '\n  %s适用 Alpine、Debian、Ubuntu；建议单核 512MB 以上内存。\n' "$C_DIM" >&2
  printf '  请确认域名或 IP 已指向本服务器，且 TCP 80 端口可访问。%s\n' "$C_RST" >&2
  printf '\n  %s作者那么羡慕你，仅供学习自用，请勿随意传播。%s\n' "$C_DIM" "$C_RST" >&2
}

validate_port(){ case "$1" in ''|*[!0-9]*) return 1;; esac; [ "$1" -ge 1024 ] && [ "$1" -le 65535 ]; }
validate_path(){ case "$1" in ''|*[!A-Za-z0-9_-]*) return 1;; esac; [ "${#1}" -le 48 ]; }

go_meets_requirement(){
	output=$("$1" version 2>/dev/null || true)
	minor=$(printf '%s' "$output" | sed -n 's/.* go1\.\([0-9][0-9]*\)\..*/\1/p')
	[ -n "$minor" ] && [ "$minor" -ge 22 ]
}

prepare_go_toolchain(){
	if [ -n "$GO_BIN" ] && command -v "$GO_BIN" >/dev/null 2>&1 && go_meets_requirement "$GO_BIN"; then return 0; fi
	if command -v go >/dev/null 2>&1 && go_meets_requirement go; then GO_BIN=$(command -v go); return 0; fi
	case "$(uname -m)" in
		x86_64|amd64) GO_ARCH=amd64; GO_SHA256=4fa4f869b0f7fc6bb1eb2660e74657fbf04cdd290b5aef905585c86051b34d43;;
		aarch64|arm64) GO_ARCH=arm64; GO_SHA256=fd017e647ec28525e86ae8203236e0653242722a7436929b1f775744e26278e7;;
		*) fail "当前 CPU 架构 $(uname -m) 未提供受控 Go 工具链。";;
	esac
	command -v sha256sum >/dev/null 2>&1 || fail '未找到 sha256sum，无法校验 Go 工具链。'
	archive=$(mktemp)
	task "下载 Go ${GO_VERSION} 工具链" curl -fsSL --retry 3 --connect-timeout 10 "https://go.dev/dl/${GO_VERSION}.linux-${GO_ARCH}.tar.gz" -o "$archive" \
		|| { rm -f "$archive"; fail '下载 Go 工具链失败。'; }
	if ! printf '%s  %s\n' "$GO_SHA256" "$archive" | sha256sum -c - >/dev/null; then rm -f "$archive"; fail 'Go 工具链校验失败。'; fi
	parent=$(dirname "$GO_TOOLCHAIN_DIR")
	install -d -m 755 "$parent"
	rm -rf "$GO_TOOLCHAIN_DIR" "$parent/go"
	task "解压 Go 工具链" tar -C "$parent" -xzf "$archive" \
		|| { rm -f "$archive"; fail '解压 Go 工具链失败。'; }
	rm -f "$archive"
	if [ "$GO_TOOLCHAIN_DIR" != "$parent/go" ]; then mv "$parent/go" "$GO_TOOLCHAIN_DIR"; fi
	GO_BIN="$GO_TOOLCHAIN_DIR/bin/go"
	go_meets_requirement "$GO_BIN" || fail 'Go 工具链版本不符合项目要求。'
	sub_ok "Go 工具链 ${GO_VERSION} 就绪。"
}

choose_certificate(){
  istep "1/6" "选择证书类型"
  if [ -z "$CERT_TYPE" ]; then
    printf '    1) 域名证书（推荐）\n    2) IP 证书（有效期6天，严重依赖自动续签）\n' >&2
    choice=$(ask '    请选择 [默认 1：域名]： ')
    case "${choice:-1}" in 1) CERT_TYPE=domain;; 2) CERT_TYPE=ip;; *) fail '证书类型无效。';; esac
  fi
  if [ "$CERT_TYPE" = domain ]; then
    while [ -z "$CERT_SUBJECT" ]; do CERT_SUBJECT=$(ask '    请输入域名： '); [ -n "$CERT_SUBJECT" ] || warn '域名不能为空。'; done
  elif [ "$CERT_TYPE" = ip ]; then
    detected=$(curl -4fsS --max-time 5 https://api.ipify.org 2>/dev/null || true)
    if [ -z "$CERT_SUBJECT" ]; then CERT_SUBJECT=$(ask "    请输入 IP [默认：${detected:-请手动输入}]： "); CERT_SUBJECT=${CERT_SUBJECT:-$detected}; fi
    [ -n "$CERT_SUBJECT" ] || fail '无法自动识别公网 IP，请手动输入。'
  else fail '证书类型必须为 domain 或 ip。'; fi
  if [ -z "$CERT_EMAIL" ]; then
    token=$(od -An -N8 -tx1 /dev/urandom 2>/dev/null | tr -d ' \n' || true)
    token=${token:-$(date +%s)}
    CERT_EMAIL="kotaui-${token}@gmail.com"
  fi
  if [ "$CERT_TYPE" = domain ]; then idone "1/6" "选择证书类型" "域名证书 · $CERT_SUBJECT";
  else idone "1/6" "选择证书类型" "IP 证书 · $CERT_SUBJECT"; fi
}

choose_panel(){
  istep "2/6" "设置面板访问参数"
  while :; do
    [ -n "$PANEL_PORT" ] || PANEL_PORT=$(ask '    自定义面板端口 [默认 1989]： ')
    PANEL_PORT=${PANEL_PORT:-1989}; validate_port "$PANEL_PORT" && break
    warn '端口必须是 1024–65535 的数字。'; PANEL_PORT=
  done
  while :; do
    [ -n "$PANEL_PATH" ] || PANEL_PATH=$(ask '    自定义面板路径 [默认 ptf]： ')
    PANEL_PATH=${PANEL_PATH:-ptf}; PANEL_PATH=$(printf '%s' "$PANEL_PATH" | tr -d '/')
    validate_path "$PANEL_PATH" && break
    warn '路径只允许字母、数字、下划线或连字符。'; PANEL_PATH=
  done
  idone "2/6" "设置面板访问参数" "${PANEL_PORT} · /${PANEL_PATH}"
}

# 账号/密码规则：3-20 个常规字符（字母、数字及 _ . - @）。
valid_credential(){
  case "$1" in
    ''|*[!A-Za-z0-9_.@-]*) return 1;;
  esac
  [ "${#1}" -ge 3 ] && [ "${#1}" -le 20 ]
}

choose_admin(){
  istep "3/6" "设置管理员账号和密码"
  while :; do
    [ -n "$ADMIN_USER" ] || ADMIN_USER=$(ask '    管理员账号： ')
    if valid_credential "$ADMIN_USER"; then break; fi
    warn '账号需为 3-20 个字符，仅支持字母、数字及 _ . - @。'
    ADMIN_USER=
  done
  while :; do
    [ -n "$ADMIN_PASSWORD" ] || ADMIN_PASSWORD=$(ask '    管理员密码（输入内容会显示，仅输入一次）： ')
    if valid_credential "$ADMIN_PASSWORD"; then break; fi
    warn '密码需为 3-20 个字符，仅支持字母、数字及 _ . - @。'
    ADMIN_PASSWORD=
  done
  idone "3/6" "设置管理员账号和密码" "$ADMIN_USER"
}

_install_deps(){
  . /etc/os-release 2>/dev/null || true
  case "${ID:-}" in
    alpine)
      apk add --no-cache ca-certificates curl git certbot openssl iputils python3 py3-pip py3-virtualenv
      ;;
    debian|ubuntu)
      apt-get update -qq
      DEBIAN_FRONTEND=noninteractive apt-get install -y -qq ca-certificates curl git golang-go certbot openssl iputils-ping python3 python3-venv python3-pip
      ;;
  esac
}

install_packages(){
  . /etc/os-release 2>/dev/null || true
  case "${ID:-}" in alpine|debian|ubuntu) ;; *) fail '当前仅支持 Alpine、Debian 和 Ubuntu。';; esac
  task "安装系统依赖" _install_deps || fail '安装运行环境失败。请执行 df -h 查看磁盘是否已满，清理后再重新安装。'
}

prepare_ip_certbot(){
  [ "$CERT_TYPE" = ip ] || return 0
  if "$CERTBOT_BIN" --help all 2>&1 | grep -q -- '--ip-address'; then return 0; fi
  sub_note "系统 Certbot 不支持 IP 证书，正在准备专用版本…"
	install -d -m 755 "$PREFIX"
	venv="$PREFIX/certbot-venv"
	rm -rf "$venv"
  task "创建 Certbot 运行环境" python3 -m venv "$venv" || fail '无法创建 Certbot 运行环境。'
  task "安装新版 Certbot" "$venv/bin/pip" install --quiet --upgrade 'certbot>=5.4' || fail '无法安装支持 IP 证书的 Certbot。'
  CERTBOT_BIN="$venv/bin/certbot"
  "$CERTBOT_BIN" --help all 2>&1 | grep -q -- '--ip-address' || fail '新版 Certbot 未提供 IP 证书功能。'
}

acquire_certificate(){
  cert_dir="/etc/letsencrypt/live/$CERT_SUBJECT"
  if [ -r "$cert_dir/fullchain.pem" ] && [ -r "$cert_dir/privkey.pem" ] && openssl x509 -checkend 86400 -noout -in "$cert_dir/fullchain.pem" >/dev/null 2>&1; then
    sub_note "已有有效证书，本次不再向证书机构申请。"
  else
    prepare_ip_certbot
    if [ "$CERT_TYPE" = domain ]; then
      task "申请域名证书" "$CERTBOT_BIN" certonly --standalone --non-interactive --agree-tos --keep-until-expiring -m "$CERT_EMAIL" -d "$CERT_SUBJECT" \
        || fail '证书申请失败，请检查域名解析与 TCP 80 端口。'
    else
      task "申请 IP 证书" "$CERTBOT_BIN" certonly --standalone --non-interactive --agree-tos --keep-until-expiring --preferred-profile shortlived -m "$CERT_EMAIL" --ip-address "$CERT_SUBJECT" \
        || fail '证书申请失败，请检查 IP 与 TCP 80 端口。'
    fi
  fi
  [ -r "$cert_dir/fullchain.pem" ] && [ -r "$cert_dir/privkey.pem" ] || fail '证书文件未生成。'
  mkdir -p "$DATA_DIR/certs" "$DATA_DIR/sing-box" "$PREFIX/bin"
  ln -sfn "$cert_dir/fullchain.pem" "$DATA_DIR/certs/fullchain.pem"; ln -sfn "$cert_dir/privkey.pem" "$DATA_DIR/certs/privkey.pem"
  sub_ok "证书已就绪并已链接到 KotaUI。"
}

_build_panel(){ (cd "$SOURCE_DIR" && CGO_ENABLED=0 "$GO_BIN" build -trimpath -ldflags='-s -w' -o "$PREFIX/kotaui" ./cmd/kotaui); }

install_program(){
  if [ -z "$SOURCE_DIR" ]; then
    SOURCE_DIR=$(mktemp -d); trap 'rm -rf "$SOURCE_DIR"' EXIT
    task "获取 KotaUI 源码" git clone --depth=1 https://github.com/ptfpwcpzy/KotaUI.git "$SOURCE_DIR" || fail '源码获取失败，请检查网络后重试。'
  fi
  [ -f "$SOURCE_DIR/go.mod" ] || fail '未找到 KotaUI Go 源码。'
  install -d -m 755 "$PREFIX" "$PREFIX/bin" "$DATA_DIR" "$DATA_DIR/sing-box" /usr/local/lib/kotaui
  install -m 755 "$SOURCE_DIR/service/kotaui-update-run" /usr/local/lib/kotaui/kotaui-update-run
  install -m 755 "$SOURCE_DIR/service/kota" "$BIN_DIR/kota"
	install -m 755 "$SOURCE_DIR/service/kota-cert-renew" "$BIN_DIR/kota-cert-renew"
	install -m 755 "$SOURCE_DIR/service/kota-build-singbox-stats" "$PREFIX/bin/kota-build-singbox-stats"
	install -m 755 "$SOURCE_DIR/service/kotaui-wait-singbox-config" /usr/local/lib/kotaui/kotaui-wait-singbox-config
  sub_ok "管理脚本已安装。"
  prepare_go_cache
  prepare_go_toolchain
  task "编译面板核心" _build_panel || fail '面板编译失败。'
  task "构建流量统计核心" env KOTAUI_GO_BIN="$GO_BIN" "$PREFIX/bin/kota-build-singbox-stats" "$PREFIX/sing-box-v2ray" || fail '流量统计核心构建失败。'
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
KOTAUI_ADMIN_USER='$(printf %s "$ADMIN_USER" | sed "s/'/'\\''/g")'
KOTAUI_ADMIN_PASSWORD='$(printf %s "$ADMIN_PASSWORD" | sed "s/'/'\\''/g")'
	KOTAUI_CERTBOT_BIN=$CERTBOT_BIN
	KOTAUI_GO_BIN=$GO_BIN
	KOTAUI_SINGBOX_BIN=$PREFIX/sing-box-v2ray
KOTAUI_SINGBOX_CONFIG=$DATA_DIR/sing-box/config.json
KOTAUI_MANAGE_SINGBOX=1
KOTAUI_STATS_PORT=9090
EOF
  chmod 600 "$DATA_DIR/runtime.env"
  sub_ok "面板配置已生成。"
}

_install_services(){
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
    install -m 644 "$SOURCE_DIR/service/kota-update.service" /etc/systemd/system/kota-update.service
    install -m 644 "$SOURCE_DIR/service/kota-cert-renew.service" /etc/systemd/system/kota-cert-renew.service
	install -m 644 "$SOURCE_DIR/service/kota-cert-renew.timer" /etc/systemd/system/kota-cert-renew.timer
    systemctl daemon-reload; systemctl reset-failed kotaui-singbox >/dev/null 2>&1 || true; systemctl enable --now kotaui; systemctl enable --now kotaui-singbox kota-cert-renew.timer
  fi
}

health_check(){
  start=$(date +%s); attempt=0
  if [ "$TTY" = 1 ]; then
    printf '  %s→%s %s执行服务健康检查%s' "$C_CYAN" "$C_RST" "$C_BOLD" "$C_RST" >&2
  else
    printf '  → 执行服务健康检查\n' >&2
  fi
  until "$BIN_DIR/kota" check >/dev/null 2>&1; do
    attempt=$((attempt + 1)); el=$(($(date +%s) - start))
    if [ "$TTY" = 1 ]; then
      printf '\r\033[2K  %s→%s %s执行服务健康检查%s %s(%ss)%s' "$C_CYAN" "$C_RST" "$C_BOLD" "$C_RST" "$C_DIM" "$el" "$C_RST" >&2
    fi
    if [ "$attempt" -ge 15 ]; then [ "$TTY" = 1 ] && printf '\n' >&2; fail 'KotaUI 健康检查失败，请执行 kota 查看状态和日志。'; fi
    sleep 1
  done
  el=$(($(date +%s) - start))
  if [ "$TTY" = 1 ]; then
    printf '\r\033[2K  %s✓%s %s执行服务健康检查%s %s(用时 %ss)%s\n' "$C_GREEN" "$C_RST" "$C_BOLD" "$C_RST" "$C_DIM" "$el" "$C_RST" >&2
  else
    printf '  ✓ 执行服务健康检查 (用时 %ss)\n' "$el" >&2
  fi
}

final_screen(){
  clear_screen
  printf '\n  %s✓%s %sKotaUI 安装完成%s\n' "$C_GREEN" "$C_RST" "$C_BOLD" "$C_RST" >&2
  printf '  %s━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━%s\n' "$C_DIM" "$C_RST" >&2
  printf '  管理地址：  %shttps://%s:%s/%s%s\n' "$C_CYAN" "$CERT_SUBJECT" "$PANEL_PORT" "$PANEL_PATH" "$C_RST" >&2
  printf '  管理员账号：%s\n' "$ADMIN_USER" >&2
  printf '  管理员密码：%s\n' "$ADMIN_PASSWORD" >&2
  printf '\n  %s证书已签发，自动续签已启用。%s\n' "$C_DIM" "$C_RST" >&2
  printf '  %s━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━%s\n' "$C_DIM" "$C_RST" >&2
  printf '  下次管理 KotaUI，请在终端输入 %skota%s 呼出主菜单。\n' "$C_BOLD" "$C_RST" >&2
  printf '  %s作者 · 那么羡慕你%s\n\n' "$C_DIM" "$C_RST" >&2
}

welcome
choose_certificate
choose_panel
choose_admin
progress "4/6"; step_begin "4/6" "安装运行环境"; install_packages; step_end
progress "5/6"; step_begin "5/6" "构建面板与核心"; install_program; step_end
progress "6/6"; step_begin "6/6" "申请证书、启动服务"; acquire_certificate
task "注册系统服务" _install_services || fail '系统服务注册失败。'
step_end
health_check
final_screen
