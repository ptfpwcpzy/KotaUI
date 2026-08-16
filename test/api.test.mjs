import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { spawn } from 'node:child_process';

const dataDir = fs.mkdtempSync(path.join(os.tmpdir(), 'kotaui-test-'));
const port = 18108;
const proc = spawn(process.execPath, ['server/index.mjs'], { cwd: path.resolve(import.meta.dirname, '..'), env: { ...process.env, KOTAUI_DATA_DIR: dataDir, KOTAUI_PORT: String(port) }, stdio: 'ignore' });
const base = `http://127.0.0.1:${port}`;
async function wait(){ for(let i=0;i<30;i++){ try { const r=await fetch(`${base}/api/health`); if(r.ok)return; } catch {} await new Promise(r=>setTimeout(r,100)); } throw Error('server did not start'); }
async function req(url, options){ const r=await fetch(base+url,{headers:{'content-type':'application/json'},...options}); return { status:r.status, body:await r.json() }; }

test.before(wait);
test.after(()=>proc.kill('SIGTERM'));

test('health endpoint is available', async()=>{ const x=await req('/api/health'); assert.equal(x.status,200); assert.equal(x.body.ok,true); });
test('inbound and client lifecycle', async()=>{
  const inbound=await req('/api/inbounds',{method:'POST',body:JSON.stringify({name:'edge',type:'vless',port:443})}); assert.equal(inbound.status,201);
  const invalid=await req('/api/clients',{method:'POST',body:JSON.stringify({username:'中文'})}); assert.equal(invalid.status,400);
  const client=await req('/api/clients',{method:'POST',body:JSON.stringify({username:'alice_01',note:'测试',inbounds:[inbound.body.id]})}); assert.equal(client.status,201);
  const duplicate=await req('/api/clients',{method:'POST',body:JSON.stringify({username:'alice_01'})}); assert.equal(duplicate.status,409);
  const sub=await req('/kota-sub/alice_01'); assert.equal(sub.status,200); assert.equal(sub.body.outbounds.length,1); assert.equal(sub.body.outbounds[0].server_port,443);
  const removed=await req('/api/inbounds/'+inbound.body.id,{method:'DELETE'}); assert.equal(removed.status,200);
  const state=await req('/api/state'); assert.equal(state.body.clients[0].inbounds.length,0);
});
