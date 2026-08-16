#!/bin/sh
set -eu

ROOTFS=/tmp/kotaui-go-alpine
SOURCE=/home/ubuntu/KotaUI
CERT_TYPE=${SIM_CERT_TYPE:-domain}
CERT_SUBJECT=${SIM_CERT_SUBJECT:-panel.example.test}
PROMPT_CERT=${SIM_PROMPT_CERT:-}
EXPECTED_CERT_TYPE=$CERT_TYPE
case "$PROMPT_CERT" in ip) EXPECTED_CERT_TYPE=ip;; domain) EXPECTED_CERT_TYPE=domain;; esac
export CERT_TYPE CERT_SUBJECT PROMPT_CERT EXPECTED_CERT_TYPE

rm -rf "$ROOTFS/opt/kotaui-source" "$ROOTFS/opt/kotaui" "$ROOTFS/var/lib/kotaui" "$ROOTFS/run/kotaui-sim"
mkdir -p "$ROOTFS/opt/kotaui-source" "$ROOTFS/usr/local/bin"
(cd "$SOURCE" && tar --exclude=.git --exclude=bin -cf - .) | tar -C "$ROOTFS/opt/kotaui-source" -xf -
install -m 755 "$SOURCE/test/fake-rc-service.sh" "$ROOTFS/usr/local/bin/rc-service"
printf '%s\n' '#!/bin/sh' 'exit 0' > "$ROOTFS/usr/local/bin/rc-update"
printf '%s\n' '#!/bin/sh' 'set -eu' 'case "${1:-}" in --help) echo "  --ip-address IP_ADDRESS"; exit 0;; renew) exit 0;; esac' 'subject=""; ip=0; profile=""; email=""' 'while [ "$#" -gt 0 ]; do case "$1" in -d) subject="$2"; shift 2;; -m) email="$2"; shift 2;; --ip-address) subject="$2"; ip=1; shift 2;; --preferred-profile) profile="$2"; shift 2;; *) shift;; esac; done' '[ -n "$subject" ] || exit 1' '[ "$email" != *"@example.com" ] || exit 1' 'case "$email" in kotaui-*@gmail.com) ;; *) exit 1;; esac' '[ "$ip" -eq 0 ] || [ "$profile" = shortlived ] || exit 1' 'mkdir -p "/etc/letsencrypt/live/$subject"' 'openssl req -x509 -newkey rsa:2048 -nodes -keyout "/etc/letsencrypt/live/$subject/privkey.pem" -out "/etc/letsencrypt/live/$subject/fullchain.pem" -subj "/CN=$subject" -days 7 >/dev/null 2>&1' > "$ROOTFS/usr/local/bin/certbot"
chmod 755 "$ROOTFS/usr/local/bin/rc-update" "$ROOTFS/usr/local/bin/certbot"

unshare --user --map-root-user --mount --pid --fork --mount-proc /bin/sh -c '
  set -eu
  mount --bind /tmp/kotaui-go-alpine /tmp/kotaui-go-alpine
  mount -t proc proc /tmp/kotaui-go-alpine/proc
  chroot /tmp/kotaui-go-alpine /bin/sh -c "
    set -eu
    export PATH=/usr/local/bin:/usr/local/sbin:/usr/sbin:/usr/bin:/sbin:/bin
    export KOTAUI_SOURCE_DIR=/opt/kotaui-source
    export KOTAUI_PREFIX=/opt/kotaui
    export KOTAUI_DATA_DIR=/var/lib/kotaui
    export KOTAUI_BIN_DIR=/usr/local/bin
    export KOTAUI_NONINTERACTIVE=1
    export KOTAUI_CERT_TYPE=$CERT_TYPE
    export KOTAUI_CERT_SUBJECT=$CERT_SUBJECT
    export KOTAUI_PANEL_PORT=1989
    export KOTAUI_PANEL_PATH=ptf
    export KOTAUI_ADMIN_USER=admin
    export KOTAUI_ADMIN_PASSWORD=alpine-go-test-password
    case \"$PROMPT_CERT\" in
      ip) printf \"2\\n203.0.113.10\\n\" | env -u KOTAUI_CERT_TYPE -u KOTAUI_CERT_SUBJECT sh /opt/kotaui-source/install.sh ;;
      domain) printf \"1\\npanel.example.test\\n\" | env -u KOTAUI_CERT_TYPE -u KOTAUI_CERT_SUBJECT sh /opt/kotaui-source/install.sh ;;
      *) sh /opt/kotaui-source/install.sh ;;
    esac
    test -x /opt/kotaui/kotaui
    test -x /usr/local/bin/kota
    test -f /var/lib/kotaui/runtime.env
    grep -q KOTAUI_PANEL_PORT=1989 /var/lib/kotaui/runtime.env
    grep -q KOTAUI_PANEL_PATH=/ptf /var/lib/kotaui/runtime.env
    grep -q KOTAUI_CERT_TYPE=$EXPECTED_CERT_TYPE /var/lib/kotaui/runtime.env
    /usr/local/bin/kota check
    echo ALPINE_GO_INSTALL_SIMULATION_PASS
  "
'
