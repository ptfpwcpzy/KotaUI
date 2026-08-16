import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { spawn } from 'node:child_process';

const dataDir = fs.mkdtempSync(path.join(os.tmpdir(), 'kotaui-test-'));
const port = 18108;
const proc = spawn(process.execPath, ['server/index.mjs'], { cwd: path.resolve(import.meta.dirname, '..'), env: { ...process.env, KOTAUI_DATA_DIR: dataDir, KOTAUI_PORT: String(port), KOTAUI_ADMIN_USER: 'admin', KOTAUI_ADMIN_PASSWORD: 'test-password' }, stdio: 'ignore' });
const base = `http://127.0.0.1:${port}`;
let cookie = '';
async function wait(){ for(let i=0;i<30;i++){ try { const r=await fetch(`${base}/api/health`); await r.text(); if(r.ok)return; } catch {} await new Promise(r=>setTimeout(r,100)); } throw Error('server did not start'); }
async function req(url, options={}){ const headers={'content-type':'application/json',...(cookie?{cookie}:{}),...(options.headers||{})}; const r=await fetch(base+url,{...options,headers}); const text=await r.text(); let body={}; try{body=JSON.parse(text)}catch{body={text}} return { status:r.status, body, headers:r.headers }; }

test.before(async()=>{ await wait(); const login=await req('/api/auth/login',{method:'POST',body:JSON.stringify({username:'admin',password:'test-password'})}); assert.equal(login.status,200); cookie=login.headers.get('set-cookie').split(';')[0]; });
test.after(()=>proc.kill('SIGTERM'));

test('authentication protects state', async()=>{ const saved=cookie; cookie=''; const denied=await req('/api/state'); assert.equal(denied.status,401); cookie=saved; const allowed=await req('/api/state'); assert.equal(allowed.status,200); });
test('health and IP certificate warning', async()=>{ const x=await req('/api/health'); assert.equal(x.status,200); assert.equal(x.body.ok,true); const cert=await req('/api/certificates/preview',{method:'POST',body:JSON.stringify({domain:'203.0.113.10'})}); assert.equal(cert.body.isIp,true); assert.match(cert.body.warning,/有效期/); });
test('three supported inbound types', async()=>{
  const reality=await req('/api/inbounds',{method:'POST',body:JSON.stringify({name:'reality-edge',type:'reality',port:18443,handshakeServer:'www.example.com',handshakePort:443,privateKey:'test-private-key',shortId:'0123456789abcdef'})}); assert.equal(reality.status,201);
  const hy2=await req('/api/inbounds',{method:'POST',body:JSON.stringify({name:'hy2-edge',type:'hysteria2',port:18444,tlsServerName:'example.com',upMbps:100,downMbps:500})}); assert.equal(hy2.status,201);
  const ss=await req('/api/inbounds',{method:'POST',body:JSON.stringify({name:'ss-edge',type:'shadowsocks2022',port:18445,method:'2022-blake3-aes-128-gcm',network:'tcp,udp'})}); assert.equal(ss.status,201);
  const state=await req('/api/state'); assert.deepEqual(state.body.inbounds.map(x=>x.type),['reality','hysteria2','shadowsocks2022']);
});
test('client validation and subscription', async()=>{ const invalid=await req('/api/clients',{method:'POST',body:JSON.stringify({username:'中文'})}); assert.equal(invalid.status,400); const client=await req('/api/clients',{method:'POST',body:JSON.stringify({username:'alice_01',note:'测试'})}); assert.equal(client.status,201); const duplicate=await req('/api/clients',{method:'POST',body:JSON.stringify({username:'alice_01'})}); assert.equal(duplicate.status,409); });
test('config generation and update rollback', async()=>{ const validation=await req('/api/config/validate',{method:'POST',body:'{}'}); assert.equal(validation.status,200); assert.ok(validation.body.configFile); const success=await req('/api/updates/run',{method:'POST',body:JSON.stringify({scope:'all',uiVersion:'9.9.9'})}); assert.equal(success.body.ok,true); assert.equal(success.body.versions.ui,'9.9.9'); const failed=await req('/api/updates/run',{method:'POST',body:JSON.stringify({scope:'all',uiVersion:'10.0.0',simulateFailure:true})}); assert.equal(failed.body.ok,false); assert.equal(failed.body.rolledBack,true); assert.equal(failed.body.versions.ui,'9.9.9'); });
