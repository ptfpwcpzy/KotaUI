#!/bin/sh
# POSIX-compatible local maintenance menu for KotaUI.
set -eu

PREFIX=${KOTAUI_PREFIX:-/opt/kotaui}
DATA_DIR=${KOTAUI_DATA_DIR:-/var/lib/kotaui}
BIN_DIR=${KOTAUI_BIN_DIR:-/usr/local/bin}
RUNTIME_ENV="$DATA_DIR/runtime.env"
INTERACTIVE_MENU=0

[ "$(id -u)" -eq 0 ] || { printf '%s\n' 'kota 必须以 root 身份运行。' >&2; exit 1; }
[ -r "$RUNTIME_ENV" ] || { printf '%s\n' '未发现 KotaUI 运行配置，请确认已完成安装。' >&2; exit 1; }
set -a
. "$RUNTIME_ENV"
set +a

clear_screen(){ command -v clear >/dev/null 2>&1 && clear || printf '\033c'; }
line(){ printf '%s\n' '────────────────────────────────────────────────────────────'; }
title(){ clear_screen; printf '%s\n' '╔════════════════════ KotaUI 本机管理 ════════════════════╗'; printf '%s\n' '║  轻量 sing-box 管理面板                                  ║'; printf '%s\n' '╚══════════════════════════════════════════════════════════╝'; }
pause(){ [ "$INTERACTIVE_MENU" = 1 ] || return 0; printf '\n按 Enter 返回菜单…'; IFS= read -r _ || true; }
ask(){ printf '%s' "$1"; IFS= read -r answer || true; printf '%s' "$answer"; }
manager(){ if command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; then printf '%s' systemd; else printf '%s' openrc; fi; }
svc(){
  unit=$1; action=$2
  if [ "$(manager)" = systemd ]; then systemctl "$action" "$unit.service"; else rc-service "$unit" "$action"; fi
}
svc_active(){
  unit=$1
  if [ "$(manager)" = systemd ]; then systemctl is-active --quiet "$unit.service"; else rc-service "$unit" status >/dev/null 2>&1; fi
}
service_state(){ if svc_active "$1"; then printf '运行中'; else printf '未运行'; fi; }
api_url(){ printf 'https://127.0.0.1:%s/api/health' "$KOTAUI_PORT"; }
certificate_expiry(){
  [ -r "$KOTAUI_TLS_CERT" ] || { printf '未找到'; return; }
  openssl x509 -enddate -noout -in "$KOTAUI_TLS_CERT" 2>/dev/null | sed 's/^notAfter=//' || printf '无法读取'
}

show_status(){
  title
  printf '管理地址： https://%s:%s%s\n' "$KOTAUI_DOMAIN" "$KOTAUI_PORT" "$KOTAUI_PANEL_PATH"
  printf '面板服务： %s\n' "$(service_state kotaui)"
  printf '核心服务： %s\n' "$(service_state kotaui-singbox)"
  printf '证书到期： %s\n' "$(certificate_expiry)"
  printf '面板进程内存： '
  ps -C node -o rss= 2>/dev/null | awk '{sum+=$1} END {if(sum) printf "%.1f MB\n",sum/1024; else print "未获取"}'
  printf '系统内存： '
  awk '/MemTotal/ {total=$2} /MemAvailable/ {avail=$2} END {if(total) printf "已用 %.1f / %.1f MB\n",(total-avail)/1024,total/1024}' /proc/meminfo 2>/dev/null || true
  printf '运行时长： '
  awk '{d=int($1/86400); h=int(($1%86400)/3600); m=int(($1%3600)/60); printf "%d 天 %d 小时 %d 分钟\n",d,h,m}' /proc/uptime 2>/dev/null || true
  line
  printf '提示：终端凭据、证书私钥与流量统计 API 均不对公网开放。\n'
  pause
}

service_menu(){
  title
  printf '%s\n' '1) 启动所有服务'
  printf '%s\n' '2) 停止所有服务'
  printf '%s\n' '3) 重启所有服务'
  printf '%s\n' '0) 返回'
  choice=$(ask '请选择：')
  case "$choice" in
    1) svc start kotaui-singbox; svc start kotaui; printf '已启动 KotaUI 与 sing-box。\n';;
    2) svc stop kotaui; svc stop kotaui-singbox; printf '已停止 KotaUI 与 sing-box。\n';;
    3) svc restart kotaui-singbox; svc restart kotaui; printf '已重启 KotaUI 与 sing-box。\n';;
    0) return;;
    *) printf '无效选择。\n';;
  esac
  pause
}

show_logs(){
  title
  printf '%s\n' '1) 面板日志（最近 100 行）'
  printf '%s\n' '2) sing-box 日志（最近 100 行）'
  printf '%s\n' '0) 返回'
  choice=$(ask '请选择：')
  case "$choice" in
    1) if [ "$(manager)" = systemd ]; then journalctl -u kotaui.service -n 100 --no-pager; else tail -n 100 /var/log/kotaui.log 2>/dev/null || true; fi;;
    2) if [ "$(manager)" = systemd ]; then journalctl -u kotaui-singbox.service -n 100 --no-pager; else tail -n 100 /var/log/kotaui-singbox.log 2>/dev/null || true; fi;;
    0) return;;
    *) printf '无效选择。\n';;
  esac
  pause
}

health_check(){
  title
  result=0
  printf '面板 API： '
  if curl -kfsS --max-time 5 "$(api_url)" >/dev/null 2>&1; then printf '正常\n'; else printf '异常\n'; result=1; fi
  printf 'sing-box 配置： '
  if "$BIN_DIR/sing-box" check -c "$KOTAUI_SINGBOX_CONFIG" >/dev/null 2>&1; then printf '正常\n'; else printf '异常\n'; result=1; fi
  printf '核心服务： %s\n' "$(service_state kotaui-singbox)"
  printf 'TLS 证书： '
  if [ -r "$KOTAUI_TLS_CERT" ] && openssl x509 -checkend 0 -noout -in "$KOTAUI_TLS_CERT" >/dev/null 2>&1; then printf '有效\n'; else printf '无效或缺失\n'; result=1; fi
  printf '统计 API： '
  stats_port_hex=$(printf '%04X' "$KOTAUI_STATS_PORT")
  if (command -v ss >/dev/null 2>&1 && ss -ltn 2>/dev/null | grep -q "127.0.0.1:${KOTAUI_STATS_PORT}") || (command -v netstat >/dev/null 2>&1 && netstat -lnt 2>/dev/null | grep -q "127.0.0.1:${KOTAUI_STATS_PORT}") || awk -v port="$stats_port_hex" 'NR>1 && substr($2,1,8)=="0100007F" && substr($2,length($2)-3)==port {found=1} END {exit !found}' /proc/net/tcp 2>/dev/null; then printf '仅本地监听\n'; else printf '未检测到监听\n'; result=1; fi
  line
  if [ "$result" -eq 0 ]; then
    printf '健康检查通过。\n'
    pause
    return 0
  fi
  printf '检测到异常，请查看日志后处理。\n'
  pause
  return 1
}

certificate_menu(){
  title
  printf '当前证书主体： %s\n' "${KOTAUI_DOMAIN:-未设置}"
  printf '证书到期： %s\n' "$(certificate_expiry)"
  printf '%s\n' '1) 立即执行续签检查'
  printf '%s\n' '2) 查看自动续签状态'
  printf '%s\n' '0) 返回'
  choice=$(ask '请选择：')
  case "$choice" in
    1) "$BIN_DIR/kota-cert-renew" && printf '续签检查完成。\n' || printf '续签检查失败，请查看证书服务日志。\n';;
    2) if [ "$(manager)" = systemd ]; then systemctl status kota-cert-renew.timer --no-pager; else printf 'Alpine 已设置每 6 小时自动运行。\n'; fi;;
    0) return;;
    *) printf '无效选择。\n';;
  esac
  pause
}

backup_restore(){
  title
  printf '%s\n' '1) 创建不含 TLS 私钥的备份'
  printf '%s\n' '2) 恢复备份'
  printf '%s\n' '0) 返回'
  choice=$(ask '请选择：')
  case "$choice" in
    1)
      archive="/root/kotaui-backup-$(date +%Y%m%d-%H%M%S).tar.gz"
      tar -C "$DATA_DIR" -czf "$archive" state.json runtime.env certificate.env 2>/dev/null || tar -C "$DATA_DIR" -czf "$archive" state.json runtime.env
      chmod 600 "$archive"
      printf '备份已创建：%s\n' "$archive"
      ;;
    2)
      archive=$(ask '请输入备份文件绝对路径：')
      [ -r "$archive" ] || { printf '无法读取该备份文件。\n'; pause; return; }
      printf '恢复将覆盖当前面板状态。输入 RESTORE-KOTAUI 继续：'
      IFS= read -r confirm || true
      [ "$confirm" = RESTORE-KOTAUI ] || { printf '已取消。\n'; pause; return; }
      tar -C "$DATA_DIR" -xzf "$archive" state.json runtime.env certificate.env
      svc restart kotaui-singbox || true
      svc restart kotaui || true
      printf '备份已恢复，服务已重启。\n'
      ;;
    0) return;;
    *) printf '无效选择。\n';;
  esac
  pause
}

update_menu(){
  title
  printf '%s\n' 'KotaUI 更新由面板“设置 → 更新”执行，以便先完成备份与配置校验。'
  printf '%s\n' '终端侧可在完成更新后使用“健康检查”确认服务状态。'
  printf '%s\n' '当前版本信息：'
  [ -r "$DATA_DIR/state.json" ] && sed -n '1,80p' "$DATA_DIR/state.json" || true
  pause
}

uninstall_kotaui(){
  title
  printf '%s\n' '此操作将停止并删除 KotaUI、sing-box 配置、用户数据和本机管理命令。'
  printf '%s\n' 'Let’s Encrypt 证书默认保留在 /etc/letsencrypt，以避免误删其他服务使用的证书。'
  choice=$(ask '是否先创建不含 TLS 私钥的备份？[Y/n]：')
  case "$choice" in ''|Y|y|yes|YES) archive="/root/kotaui-backup-before-remove-$(date +%Y%m%d-%H%M%S).tar.gz"; tar -C "$DATA_DIR" -czf "$archive" state.json runtime.env certificate.env 2>/dev/null || true; [ -f "$archive" ] && chmod 600 "$archive" && printf '已创建备份：%s\n' "$archive";; esac
  printf '输入 REMOVE-KOTAUI 以确认永久卸载：'
  IFS= read -r confirm || true
  [ "$confirm" = REMOVE-KOTAUI ] || { printf '已取消卸载。\n'; pause; return; }
  if [ "$(manager)" = systemd ]; then
    systemctl disable --now kotaui.service kotaui-singbox.service kota-cert-renew.timer >/dev/null 2>&1 || true
    rm -f /etc/systemd/system/kotaui.service /etc/systemd/system/kotaui-singbox.service /etc/systemd/system/kota-cert-renew.service /etc/systemd/system/kota-cert-renew.timer
    systemctl daemon-reload
  else
    rc-update del kotaui default >/dev/null 2>&1 || true
    rc-update del kotaui-singbox default >/dev/null 2>&1 || true
    rm -f /etc/init.d/kotaui /etc/init.d/kotaui-singbox /etc/periodic/6hourly/kota-cert-renew
  fi
  rm -rf "$PREFIX" "$DATA_DIR"
  rm -f "$BIN_DIR/kota-cert-renew" "$BIN_DIR/kota"
  printf 'KotaUI 已卸载；TLS 证书未删除。\n'
  exit 0
}

menu(){
  INTERACTIVE_MENU=1
  while :; do
    title
    printf '%s\n' '1) 状态概览'
    printf '%s\n' '2) 启动 / 停止 / 重启服务'
    printf '%s\n' '3) 查看日志'
    printf '%s\n' '4) 健康检查'
    printf '%s\n' '5) 证书管理'
    printf '%s\n' '6) 更新管理'
    printf '%s\n' '7) 备份与恢复'
    printf '%s\n' '8) 卸载 KotaUI'
    printf '%s\n' '0) 退出'
    choice=$(ask '请选择：')
    case "$choice" in
      1) show_status;; 2) service_menu;; 3) show_logs;; 4) health_check;; 5) certificate_menu;; 6) update_menu;; 7) backup_restore;; 8) uninstall_kotaui;; 0) exit 0;; *) printf '无效选择。\n'; pause;;
    esac
  done
}

case "${1:-menu}" in
  menu) menu;;
  status|info) show_status;;
  start) svc start kotaui-singbox; svc start kotaui;;
  stop) svc stop kotaui; svc stop kotaui-singbox;;
  restart) svc restart kotaui-singbox; svc restart kotaui;;
  logs) show_logs;;
  check) health_check;;
  cert) certificate_menu;;
  backup) backup_restore;;
  uninstall) uninstall_kotaui;;
  *) printf '%s\n' '用法: kota {menu|status|start|stop|restart|logs|check|cert|backup|uninstall}'; exit 1;;
esac
