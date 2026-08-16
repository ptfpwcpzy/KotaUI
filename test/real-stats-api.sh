#!/bin/sh
set -eu
rm -rf /tmp/kota-stats-data
mkdir -p /tmp/kota-stats-data
KOTAUI_DATA_DIR=/tmp/kota-stats-data KOTAUI_PORT=1108 KOTAUI_STATS_PORT=9090 KOTAUI_ADMIN_USER=admin KOTAUI_ADMIN_PASSWORD=test-password node /opt/kotaui/server/index.mjs >/tmp/kota-stats-ui.log 2>&1 &
UI_PID=$!
trap 'kill "$UI_PID" "$SB_PID" 2>/dev/null || true' EXIT
sleep 2
/usr/local/bin/sing-box-v2 run -c /tmp/kota-stats-data/sing-box/config.json >/tmp/kota-stats-singbox.log 2>&1 &
SB_PID=$!
sleep 2
node --input-type=module <<'EOF'
import assert from 'node:assert/strict';
import { queryStats, collectUserTraffic } from '/opt/kotaui/server/lib/stats-client.mjs';
const stats = await queryStats('127.0.0.1:9090', 'user>>>');
assert.ok(Array.isArray(stats));
const users = await collectUserTraffic('127.0.0.1:9090', ['alice']);
assert.deepEqual(users.alice, { uploadBytes: 0, downloadBytes: 0 });
console.log('REAL_STATS_API_PASS');
EOF
