#!/bin/sh
set -eu
ROOTFS=/tmp/kotaui-alpine-rootfs
SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
SIM_CERT_TYPE=${SIM_CERT_TYPE:-domain}
SIM_CERT_SUBJECT=${SIM_CERT_SUBJECT:-panel.example.test}
export SIM_CERT_TYPE SIM_CERT_SUBJECT
install -m 755 "$SCRIPT_DIR/fake-rc-service.sh" "$ROOTFS/usr/local/bin/rc-service"

unshare --user --map-root-user --mount --pid --fork --mount-proc /bin/sh -c '
  set -eu
  mount --bind /tmp/kotaui-alpine-rootfs /tmp/kotaui-alpine-rootfs
  mkdir -p /tmp/kotaui-alpine-rootfs/proc
  mount -t proc proc /tmp/kotaui-alpine-rootfs/proc
  chroot /tmp/kotaui-alpine-rootfs /bin/sh -c "
    set -eu
    export PATH=/usr/local/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
    export KOTAUI_SOURCE_DIR=/opt/kotaui-source
    export KOTAUI_PREFIX=/opt/kotaui
    export KOTAUI_DATA_DIR=/var/lib/kotaui
    export KOTAUI_BIN_DIR=/usr/local/bin
    export KOTAUI_NONINTERACTIVE=1
    export KOTAUI_CERT_TYPE=$SIM_CERT_TYPE
    export KOTAUI_CERT_SUBJECT=$SIM_CERT_SUBJECT
    export KOTAUI_CERT_EMAIL=test@example.test
    export KOTAUI_PANEL_PORT=1989
    export KOTAUI_PANEL_PATH=ptf
    export KOTAUI_ADMIN_USER=admin
    export KOTAUI_ADMIN_PASSWORD=alpine-test-password
    export KOTAUI_CREATE_BUILD_SWAP=0
    export KOTAUI_BUILD_JOBS=1
    export KOTAUI_BUILD_MEMORY=256MiB
    /opt/kotaui-source/install.sh
    test -x /usr/local/bin/kota
    test -x /opt/kotaui/bin/kotaui-run
    test -f /var/lib/kotaui/runtime.env
    test -f /var/lib/kotaui/certificate.env
    test -L /var/lib/kotaui/certs/fullchain.pem
    grep -q KOTAUI_PANEL_PATH=/ptf /var/lib/kotaui/runtime.env
    grep -q KOTAUI_MANAGE_SERVICES=1 /var/lib/kotaui/runtime.env
    /usr/local/bin/kota status </dev/null >/tmp/kota-status.txt 2>&1 || true
    grep -q KotaUI /tmp/kota-status.txt
    echo ALPINE_INSTALL_SIMULATION_PASS
  "
'
