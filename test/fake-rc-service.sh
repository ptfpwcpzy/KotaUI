#!/bin/sh
set -eu

service=${1:?service required}
action=${2:-status}
run_dir=/run/kotaui-sim
mkdir -p "$run_dir"
pid_file="$run_dir/${service}.pid"

start_service(){
  [ -r "$pid_file" ] && kill -0 "$(cat "$pid_file")" 2>/dev/null && return 0
  case "$service" in
    kotaui) nohup /opt/kotaui/bin/kotaui-run >"$run_dir/kotaui.log" 2>&1 & ;;
    kotaui-singbox) nohup /opt/kotaui/bin/kotaui-singbox-run >"$run_dir/kotaui-singbox.log" 2>&1 & ;;
    *) exit 0 ;;
  esac
  echo $! > "$pid_file"
}
stop_service(){
  [ -r "$pid_file" ] || return 0
  kill "$(cat "$pid_file")" 2>/dev/null || true
  rm -f "$pid_file"
}
case "$action" in
  start) start_service ;;
  stop) stop_service ;;
  restart) stop_service; start_service ;;
  status) [ -r "$pid_file" ] && kill -0 "$(cat "$pid_file")" 2>/dev/null ;;
  *) exit 0 ;;
esac
