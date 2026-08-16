#!/bin/sh
set -eu

service=${1:?service required}
action=${2:-status}
run_dir=/run/kotaui-sim
mkdir -p "$run_dir"
pid_file="$run_dir/${service}.pid"

start(){
  if [ -r "$pid_file" ] && kill -0 "$(cat "$pid_file")" 2>/dev/null; then return 0; fi
  case "$service" in
    kotaui) nohup /opt/kotaui/kotaui >"$run_dir/kotaui.log" 2>&1 & ;;
    kotaui-singbox) nohup "${KOTAUI_SINGBOX_BIN:-/usr/bin/sing-box}" run -c "${KOTAUI_SINGBOX_CONFIG:-/var/lib/kotaui/sing-box/config.json}" >"$run_dir/kotaui-singbox.log" 2>&1 & ;;
    *) exit 0 ;;
  esac
  echo $! > "$pid_file"
}
stop(){ [ -r "$pid_file" ] && kill "$(cat "$pid_file")" 2>/dev/null || true; rm -f "$pid_file"; }
case "$action" in
  start) start ;;
  stop) stop ;;
  restart) stop; start ;;
  status) [ -r "$pid_file" ] && kill -0 "$(cat "$pid_file")" 2>/dev/null ;;
  *) exit 0 ;;
esac
