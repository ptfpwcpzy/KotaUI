#!/bin/sh
set -eu

ENV_FILE=${KOTAUI_CERT_ENV:-/var/lib/kotaui/certificate.env}
[ -r "$ENV_FILE" ] || exit 0
. "$ENV_FILE"
command -v certbot >/dev/null 2>&1 || exit 1

reload_kotaui(){
  if command -v systemctl >/dev/null 2>&1; then
    systemctl restart kotaui-singbox.service kotaui.service
  elif command -v rc-service >/dev/null 2>&1; then
    rc-service kotaui-singbox restart
    rc-service kotaui restart
  fi
}

certbot renew --quiet --deploy-hook "$(printf '%s' 'systemctl restart kotaui-singbox.service kotaui.service 2>/dev/null || { rc-service kotaui-singbox restart 2>/dev/null && rc-service kotaui restart 2>/dev/null; } || true')"
