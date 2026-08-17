#!/bin/sh
set -eu

SOURCE=/home/ubuntu/KotaUI
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT INT TERM
FAKE_BIN="$WORK/bin"
DESTINATION="$WORK/sing-box-v2ray"
mkdir -p "$FAKE_BIN"

cat > "$FAKE_BIN/git" <<'EOF'
#!/bin/sh
set -eu
last=""
for arg in "$@"; do last="$arg"; done
[ "${1:-}" = "clone" ] || exit 64
mkdir -p "$last/cmd/sing-box"
: > "$last/go.mod"
EOF

cat > "$FAKE_BIN/go" <<'EOF'
#!/bin/sh
set -eu
[ -f go.mod ] || { echo 'go.mod file not found in current directory' >&2; exit 1; }
out=""
tags=""
previous=""
for arg in "$@"; do
  if [ "$previous" = "-tags" ]; then tags="$arg"; fi
  if [ "$previous" = "-o" ]; then out="$arg"; break; fi
  previous="$arg"
done
[ "$tags" = "with_v2ray_api,with_utls,with_quic" ] || { echo "缺少 sing-box 兼容特性编译标签" >&2; exit 65; }
[ -n "$out" ] || exit 64
cat > "$out" <<'SH'
#!/bin/sh
[ "${1:-}" = "check" ] && exit 0
exit 0
SH
chmod 755 "$out"
EOF
chmod 755 "$FAKE_BIN/git" "$FAKE_BIN/go"

sudo env PATH="$FAKE_BIN:$PATH" KOTAUI_SINGBOX_SOURCE_URL="fake://sing-box" KOTAUI_SINGBOX_VERSION="test" sh "$SOURCE/service/kota-build-singbox-stats" "$DESTINATION"
test -x "$DESTINATION"
printf '%s\n' 'STATS_CORE_BUILD_PATH_REGRESSION_PASS'
