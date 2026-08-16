#!/bin/sh
set -eu
ENV_FILE=${KOTAUI_CERT_ENV:-/var/lib/kotaui/certificate.env}
[ -r "$ENV_FILE" ] || exit 0
. "$ENV_FILE"
command -v certbot >/dev/null 2>&1 || exit 1
certbot renew --quiet --deploy-hook 'systemctl restart kotaui 2>/dev/null || rc-service kotaui restart 2>/dev/null || true'
