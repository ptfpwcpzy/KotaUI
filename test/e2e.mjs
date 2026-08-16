import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { spawn } from 'node:child_process';
const root = path.resolve(import.meta.dirname, '..');
const dataDir = fs.mkdtempSync(path.join(os.tmpdir(), 'kotaui-e2e-'));
const port = 19208;
const child = spawn(process.execPath, ['server/index.mjs'], { cwd: root, env: { ...process.env, KOTAUI_DATA_DIR: dataDir, KOTAUI_PORT: String(port), KOTAUI_ADMIN_USER: 'admin', KOTAUI_ADMIN_PASSWORD: 'test-password' }, stdio: 'ignore' });
try {
  for (let i = 0; i < 30; i++) { try { const probe = await fetch(`http://127.0.0.1:${port}/api/health`, { headers: { connection: 'close' } }); await probe.text(); if (probe.ok) break; } catch {} await new Promise(r => setTimeout(r, 100)); }
  const login = await fetch(`http://127.0.0.1:${port}/api/auth/login`, { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ username: 'admin', password: 'test-password' }) }); assert.equal(login.status, 200); const cookie = login.headers.get('set-cookie').split(';')[0];
  const page = await fetch(`http://127.0.0.1:${port}/`); assert.equal(page.status, 200); assert.match(await page.text(), /KotaUI/);
  const state = await fetch(`http://127.0.0.1:${port}/api/state`, { headers: { cookie } }); assert.equal(state.status, 200); assert.ok((await state.json()).inbounds);
  const missing = await fetch(`http://127.0.0.1:${port}/kota-sub/missing`); assert.equal(missing.status, 404);
  console.log('E2E smoke passed');
} finally { child.kill('SIGTERM'); fs.rmSync(dataDir, { recursive: true, force: true }); }
