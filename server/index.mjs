import http from 'node:http';
import fs from 'node:fs';
import path from 'node:path';
import crypto from 'node:crypto';
import { fileURLToPath } from 'node:url';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const dataDir = process.env.KOTAUI_DATA_DIR || path.join(root, 'data');
const port = Number(process.env.KOTAUI_PORT || 1108);
const host = process.env.KOTAUI_HOST || '127.0.0.1';
fs.mkdirSync(dataDir, { recursive: true, mode: 0o700 });
const dbFile = path.join(dataDir, 'state.json');
const initialState = {
  settings: { panelName: 'KotaUI', domain: '', panelPort: port, panelPath: '/admin', subscriptionPort: 1109, subscriptionPath: '/kota-sub' },
  inbounds: [],
  clients: []
};
function load() {
  try { return JSON.parse(fs.readFileSync(dbFile, 'utf8')); } catch { save(initialState); return structuredClone(initialState); }
}
function save(state) { const tmp = `${dbFile}.tmp`; fs.writeFileSync(tmp, JSON.stringify(state, null, 2), { mode: 0o600 }); fs.renameSync(tmp, dbFile); }
let state = load();
const send = (res, code, body, type='application/json') => { res.writeHead(code, { 'Content-Type': `${type}; charset=utf-8`, 'Cache-Control': 'no-store' }); const payload = type === 'application/json' ? JSON.stringify(body) : (typeof body === 'string' || Buffer.isBuffer(body) ? body : JSON.stringify(body)); res.end(payload); };
const readBody = req => new Promise((resolve, reject) => { let raw=''; req.on('data', c => { raw += c; if (raw.length > 1_000_000) reject(new Error('request too large')); }); req.on('end', () => { try { resolve(raw ? JSON.parse(raw) : {}); } catch { reject(new Error('invalid json')); } }); req.on('error', reject); });
const id = () => crypto.randomUUID();
const validUsername = name => typeof name === 'string' && /^[A-Za-z0-9_-]{3,32}$/.test(name);
const routes = async (req, res) => {
  const url = new URL(req.url, `http://${req.headers.host || 'localhost'}`);
  if (req.method === 'GET' && url.pathname === '/api/health') return send(res, 200, { ok: true, service: 'kotaui', version: '0.1.0', uptime: process.uptime() });
  if (req.method === 'GET' && url.pathname === '/api/state') return send(res, 200, { settings: state.settings, inbounds: state.inbounds, clients: state.clients.map(c => ({ ...c, password: undefined })) });
  if (req.method === 'GET' && url.pathname === '/api/settings') return send(res, 200, state.settings);
  if (req.method === 'PUT' && url.pathname === '/api/settings') { const body = await readBody(req); state.settings = { ...state.settings, ...body }; save(state); return send(res, 200, state.settings); }
  if (req.method === 'POST' && url.pathname === '/api/inbounds') { const b = await readBody(req); if (!b.name || !b.type || !Number(b.port)) return send(res, 400, { error: 'name, type and port are required' }); const item = { id: id(), name: String(b.name), type: String(b.type), port: Number(b.port), listen: b.listen || '0.0.0.0', enabled: b.enabled !== false, createdAt: new Date().toISOString() }; state.inbounds.push(item); save(state); return send(res, 201, item); }
  if (req.method === 'DELETE' && url.pathname.startsWith('/api/inbounds/')) { const iid = url.pathname.split('/').pop(); const before = state.inbounds.length; state.inbounds = state.inbounds.filter(x => x.id !== iid); if (before === state.inbounds.length) return send(res, 404, { error: 'inbound not found' }); state.clients.forEach(c => c.inbounds = c.inbounds.filter(x => x !== iid)); save(state); return send(res, 200, { ok: true }); }
  if (req.method === 'POST' && url.pathname === '/api/clients') { const b = await readBody(req); if (!validUsername(b.username)) return send(res, 400, { error: 'username must be 3-32 chars: A-Z, a-z, 0-9, _ or -' }); if (state.clients.some(c => c.username === b.username)) return send(res, 409, { error: 'username already exists' }); const item = { id: id(), username: b.username, note: b.note || '', inbounds: Array.isArray(b.inbounds) ? b.inbounds : [], totalLimitBytes: Number(b.totalLimitBytes || 0), monthlyLimitBytes: Number(b.monthlyLimitBytes || 0), expiresAt: b.expiresAt || null, maxOnlineIps: Number(b.maxOnlineIps || 0), enabled: true, usedBytes: 0, createdAt: new Date().toISOString() }; state.clients.push(item); save(state); return send(res, 201, item); }
  if (req.method === 'PATCH' && url.pathname.startsWith('/api/clients/')) { const cid = url.pathname.split('/').pop(); const item = state.clients.find(x => x.id === cid); if (!item) return send(res, 404, { error: 'client not found' }); const b = await readBody(req); if (b.username && (!validUsername(b.username) || state.clients.some(x => x.id !== cid && x.username === b.username))) return send(res, 400, { error: 'invalid or duplicate username' }); Object.assign(item, b); save(state); return send(res, 200, item); }
  if (req.method === 'GET' && url.pathname.startsWith('/kota-sub/')) { const username = url.pathname.split('/').pop(); const client = state.clients.find(x => x.username === username); if (!client) return send(res, 404, { error: 'subscription not found' }, 'text/plain'); const nodes = state.inbounds.filter(x => client.inbounds.includes(x.id) && x.enabled).map(x => ({ tag: x.name, type: x.type, server: state.settings.domain || '127.0.0.1', server_port: x.port })); return send(res, 200, { username, updated_at: new Date().toISOString(), outbounds: nodes }, 'application/json'); }
  if (req.method === 'GET' && (url.pathname === '/' || url.pathname === '/index.html')) { return send(res, 200, fs.readFileSync(path.join(root, 'public', 'index.html'), 'utf8'), 'text/html'); }
  return send(res, 404, { error: 'not found' });
};
const server = http.createServer((req, res) => routes(req, res).catch(err => send(res, 400, { error: err.message }))); 
server.listen(port, host, () => console.log(`KotaUI listening on http://${host}:${port}`));
