#!/bin/sh
set -eu
VERSION=${SINGBOX_VERSION:-v1.13.18}
PREFIX=${SINGBOX_PREFIX:-/usr/local/bin}
TMP=${TMPDIR:-/tmp}/kota-singbox-build-$$
trap 'rm -rf "$TMP"' EXIT
command -v go >/dev/null 2>&1 || { echo '需要 Go 编译器。' >&2; exit 1; }
command -v git >/dev/null 2>&1 || { echo '需要 git。' >&2; exit 1; }
git clone --depth 1 --branch "$VERSION" https://github.com/SagerNet/sing-box.git "$TMP/src" >/dev/null 2>&1
cd "$TMP/src"
GOTOOLCHAIN=auto CGO_ENABLED=0 go build -buildvcs=false -tags 'with_v2ray_api with_utls with_quic' -trimpath -ldflags '-s -w' -o "$TMP/sing-box" ./cmd/sing-box
mkdir -p "$PREFIX"
install -m 755 "$TMP/sing-box" "$PREFIX/sing-box"
"$PREFIX/sing-box" version | grep -q 'with_v2ray_api' || { echo '构建产物缺少 with_v2ray_api 标签。' >&2; exit 1; }
