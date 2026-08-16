import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';

const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'kota-ufw-'));
const log = path.join(dir, 'ufw.log');
const ufw = path.join(dir, 'ufw');
fs.writeFileSync(ufw, `#!/bin/sh\nprintf '%s\\n' "$*" >> ${JSON.stringify(log)}\n`, { mode: 0o700 });
process.env.PATH = `${dir}:${process.env.PATH}`;
process.env.KOTAUI_MANAGE_FIREWALL = '1';
const { openInboundPort, closeInboundPort } = await import('../server/lib/firewall.mjs');

test('firewall opens and closes only required protocol ports', () => {
  const opened = openInboundPort(24001, 'tcp');
  const hy2 = openInboundPort(24002, 'udp');
  const closed = closeInboundPort(24001, 'tcp');
  assert.equal(opened.ok, true);
  assert.equal(hy2.ok, true);
  assert.equal(closed.ok, true);
  const lines = fs.readFileSync(log, 'utf8').trim().split('\n');
  assert.deepEqual(lines, ['allow 24001/tcp', 'allow 24002/udp', 'delete allow 24001/tcp']);
});
