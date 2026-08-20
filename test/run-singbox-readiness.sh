#!/bin/sh
set -eu

ROOT=$(CDPATH= cd "$(dirname "$0")/.." && pwd)
WORKDIR=$(mktemp -d)
trap 'rm -rf "$WORKDIR"' EXIT INT TERM

CORE="$WORKDIR/sing-box"
CONFIG="$WORKDIR/config.json"
RUNTIME="$WORKDIR/runtime.env"

cat > "$CORE" <<'EOF'
#!/bin/sh
set -eu
case "${1:-}" in
  check) test "${2:-}" = -c && test -s "${3:-}" ;;
  *) exit 1 ;;
esac
EOF
chmod 755 "$CORE"
cat > "$RUNTIME" <<EOF
KOTAUI_SINGBOX_BIN=$CORE
KOTAUI_SINGBOX_CONFIG=$CONFIG
EOF

KOTAUI_RUNTIME_ENV="$RUNTIME" KOTAUI_CORE_READY_TIMEOUT=4 sh "$ROOT/service/kotaui-wait-singbox-config" &
waiter=$!
sleep 1
printf '%s\n' '{"inbounds":[],"outbounds":[{"type":"direct"}]}' > "$CONFIG"
wait "$waiter"
printf '%s\n' 'SINGBOX_CONFIG_READINESS_REGRESSION_PASS'
