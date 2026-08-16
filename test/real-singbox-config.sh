#!/bin/sh
set -eu
mkdir -p /tmp/kota-cert /tmp/kota-data
openssl req -x509 -newkey rsa:2048 -nodes -days 2 -subj '/CN=example.com' -keyout /tmp/kota-cert/key.pem -out /tmp/kota-cert/cert.pem >/dev/null 2>&1
KEYPAIR=$(/usr/local/bin/sing-box-v2 generate reality-keypair)
PRIVATE_KEY=$(printf '%s\n' "$KEYPAIR" | awk '/PrivateKey:/{print $2}')
rm -f /tmp/kota-data/state.json /tmp/kota-data/sing-box/config.json
KOTAUI_DATA_DIR=/tmp/kota-data KOTAUI_PORT=1108 KOTAUI_STATS_PORT=9090 KOTAUI_ADMIN_USER=admin KOTAUI_ADMIN_PASSWORD=test-password node /opt/kotaui/server/index.mjs >/tmp/kota-server.log 2>&1 &
PID=$!
trap 'kill "$PID" 2>/dev/null || true' EXIT
sleep 2
COOKIE=$(curl -fsS -D - -o /tmp/kota-login.json -X POST http://127.0.0.1:1108/api/auth/login -H 'Content-Type: application/json' -d '{"username":"admin","password":"test-password"}' | awk -F': ' '/[Ss]et-[Cc]ookie:/{print $2}' | tr -d '\r')
post_inbound(){ curl -fsS -X POST http://127.0.0.1:1108/api/inbounds -H "Cookie: $COOKIE" -H 'Content-Type: application/json' -d "$1" >/dev/null; }
post_inbound "{\"name\":\"reality\",\"type\":\"reality\",\"port\":24001,\"handshakeServer\":\"www.example.com\",\"handshakePort\":443,\"privateKey\":\"$PRIVATE_KEY\",\"shortId\":\"0123456789abcdef\"}"
post_inbound '{"name":"hy2","type":"hysteria2","port":24002,"tlsServerName":"example.com","certificatePath":"/tmp/kota-cert/cert.pem","keyPath":"/tmp/kota-cert/key.pem","upMbps":100,"downMbps":100}'
post_inbound '{"name":"ss2022","type":"shadowsocks2022","port":24003,"method":"2022-blake3-aes-128-gcm","password":"AAAAAAAAAAAAAAAAAAAAAA==","network":""}'
/usr/local/bin/sing-box-v2 check -c /tmp/kota-data/sing-box/config.json
printf '%s\n' 'REAL_SINGBOX_CONFIG_PASS'
