import http from 'node:http';
import fs from 'node:fs';
import path from 'node:path';
import crypto from 'node:crypto';
import net from 'node:net';
import { fileURLToPath } from 'node:url';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const dataDir = process.env.KOTAUI_DATA_DIR || path.join(root, 'data');
const port = Number(process.env.KOTAUI_PORT || 1108);
const host = process.env.KOTAUI_HOST || '127.0.0.1';
fs.mkdirSync(dataDir, { recursive: true, mode: 0o700 });
const dbFile = path.join(dataDir, 'state.json');
const updateDir = path.join(dataDir, 'updates');
const supportedTypes = new Set(['reality', 'hysteria2', 'shadowsocks2022']);
const initialState = {
  settings: { panelName: 'KotaUI', domain: '', panelPort: port, panelPath: '/admin', subscriptionPort: 1109, subscriptionPath: '/kota-sub', titleEnabled: true, titleFields: ['username', 'used', 'total'] },
  versions: { ui: '0.1.0', singBox: '1.14.0', hysteria2: '2.6.0', lastUpdate: null },
  inbounds: [],
  clients: [],
  updateHistory: []
};
function clone(x) { return JSON.parse(JSON.stringify(x)); }
function save(next) { const tmp = `${dbFile}.tmp`; fs.writeFileSync(tmp, JSON.stringify(next, null, 2), { mode: 0o600 }); fs.renameSync(tmp, dbFile); }
function load() { try { const current = JSON.parse(fs.readFileSync(dbFile, 'utf8')); return { ...clone(initialState), ...current, settings: { ...initialState.settings, ...(current.settings || {}) }, versions: { ...initialState.versions, ...(current.versions || {}) } }; } catch { save(initialState); return clone(initialState); } }
let state = load();
const send = (res, code, body, type='application/json') => { res.writeHead(code, { 'Content-Type': `${type}; charset=utf-8`, 'Cache-Control': 'no-store' }); const payload = type === 'application/json' ? JSON.stringify(body) : (typeof body === 'string' || Buffer.isBuffer(body) ? body : JSON.stringify(body)); res.end(payload); };
const readBody = req => new Promise((resolve, reject) => { let raw=''; req.on('data', c => { raw += c; if (raw.length > 1_000_000) reject(new Error('request too large')); }); req.on('end', () => { try { resolve(raw ? JSON.parse(raw) : {}); } catch { reject(new Error('invalid json')); } }); req.on('error', reject); });
const id = () => crypto.randomUUID();
const validUsername = name => typeof name === 'string' && /^[A-Za-z0-9_-]{3,32}$/.test(name);
const highPort = value => Number.isInteger(Number(value)) && Number(value) >= 1024 && Number(value) <= 65535;
function validateInbound(b) {
  if (!b.name || !supportedTypes.has(b.type) || !highPort(b.port)) return '名称、支持的协议和 1024-65535 高位端口为必填项';
  if (b.type === 'reality' && (!b.handshakeServer || !b.privateKey || !b.shortId)) return 'Reality 需要握手域名、私钥和 short_id';
  if (b.type === 'hysteria2' && !b.tlsServerName) return 'Hysteria 2 需要 TLS server_name';
  if (b.type === 'shadowsocks2022' && !['2022-blake3-aes-128-gcm', '2022-blake3-aes-256-gcm', '2022-blake3-chacha20-poly1305'].includes(b.method)) return 'Shadowsocks 2022 加密方法无效';
  return null;
}
function configFor(item) {
  const base = { type: item.type === 'reality' ? 'vless' : item.type === 'shadowsocks2022' ? 'shadowsocks' : 'hysteria2', tag: item.name, listen: item.listen, listen_port: item.port };
  if (item.type === 'reality') return { ...base, users: [{ name: item.username || 'client', uuid: item.uuid || id() }], tls: { enabled: true, server_name: item.handshakeServer, reality: { enabled: true, handshake: { server: item.handshakeServer, server_port: Number(item.handshakePort || 443) }, private_key: item.privateKey, short_id: [item.shortId] } } };
  if (item.type === 'hysteria2') return { ...base, up_mbps: Number(item.upMbps || 0), down_mbps: Number(item.downMbps || 0), obfs: item.obfsPassword ? { type: 'salamander', password: item.obfsPassword } : undefined, users: [{ name: item.username || 'client', password: item.password || id() }], tls: { enabled: true, server_name: item.tlsServerName, certificate_path: item.certificatePath || '' }, masquerade: item.masquerade || '' };
  return { ...base, network: item.network || 'tcp,udp', method: item.method, password: item.password || crypto.randomBytes(item.method.includes('128') ? 16 : 32).toString('base64'), users: item.users || [] };
}
function certPreview(value) { const input = String(value || '').trim(); const ip = net.isIP(input) !== 0; return { input, isIp: ip, warning: ip ? 'IP 证书有效期可能明显短于域名证书，请在到期前及时续订。' : '', recommended: ip ? '确认 CA 支持 IP 证书，并启用到期提醒。' : '可使用标准 ACME 域名验证流程。' }; }
function createBackup() { fs.mkdirSync(updateDir, { recursive: true, mode: 0o700 }); const file = path.join(updateDir, `backup-${Date.now()}.json`); fs.writeFileSync(file, JSON.stringify(state, null, 2), { mode: 0o600 }); return file; }
async function runUpdate(body) { const backupFile = createBackup(); const before = clone(state); const selected = body.scope || 'all'; const update = { id: id(), scope: selected, startedAt: new Date().toISOString(), status: 'running', backupFile }; state.updateHistory.unshift(update); save(state); try { if (body.simulateFailure) throw new Error('模拟更新失败'); if (selected === 'all' || selected === 'ui') state.versions.ui = body.uiVersion || '0.1.1'; if (selected === 'all' || selected === 'sing-box') state.versions.singBox = body.singBoxVersion || '1.14.1'; if (selected === 'all' || selected === 'hysteria2') state.versions.hysteria2 = body.hysteria2Version || '2.6.1'; state.versions.lastUpdate = new Date().toISOString(); update.status = 'success'; update.finishedAt = new Date().toISOString(); save(state); return { ok: true, update, versions: state.versions }; } catch (error) { state = before; state.updateHistory.unshift({ ...update, status: 'rolled_back', error: error.message, finishedAt: new Date().toISOString() }); save(state); return { ok: false, rolledBack: true, error: error.message, versions: state.versions }; } }
const routes = async (req, res) => {
  const url = new URL(req.url, `http://${req.headers.host || 'localhost'}`);
  if (req.method === 'GET' && url.pathname === '/api/health') return send(res, 200, { ok: true, service: 'kotaui', version: state.versions.ui, uptime: process.uptime() });
  if (req.method === 'GET' && url.pathname === '/api/state') return send(res, 200, { settings: state.settings, versions: state.versions, inbounds: state.inbounds.map(x => ({ ...x, config: undefined, privateKey: undefined, password: undefined })), clients: state.clients.map(c => ({ ...c, password: undefined })) });
  if (req.method === 'GET' && url.pathname === '/api/settings') return send(res, 200, state.settings);
  if (req.method === 'PUT' && url.pathname === '/api/settings') { const body = await readBody(req); state.settings = { ...state.settings, ...body }; save(state); return send(res, 200, state.settings); }
  if (req.method === 'POST' && url.pathname === '/api/certificates/preview') { const b = await readBody(req); return send(res, 200, certPreview(b.domain)); }
  if (req.method === 'GET' && url.pathname === '/api/updates') return send(res, 200, { current: state.versions, available: { ui: '0.1.1', singBox: '1.14.1', hysteria2: '2.6.1' }, history: state.updateHistory.slice(0, 10) });
  if (req.method === 'POST' && url.pathname === '/api/updates/run') return send(res, 200, await runUpdate(await readBody(req)));
  if (req.method === 'POST' && url.pathname === '/api/inbounds') { const b = await readBody(req); const error = validateInbound(b); if (error) return send(res, 400, { error }); const item = { ...b, id: id(), name: String(b.name), port: Number(b.port), listen: b.listen || '0.0.0.0', enabled: b.enabled !== false, createdAt: new Date().toISOString() }; item.config = configFor(item); state.inbounds.push(item); save(state); return send(res, 201, { ...item, privateKey: undefined, password: undefined, config: undefined }); }
  if (req.method === 'DELETE' && url.pathname.startsWith('/api/inbounds/')) { const iid = url.pathname.split('/').pop(); const before = state.inbounds.length; state.inbounds = state.inbounds.filter(x => x.id !== iid); if (before === state.inbounds.length) return send(res, 404, { error: 'inbound not found' }); state.clients.forEach(c => c.inbounds = c.inbounds.filter(x => x !== iid)); save(state); return send(res, 200, { ok: true }); }
  if (req.method === 'POST' && url.pathname === '/api/clients') { const b = await readBody(req); if (!validUsername(b.username)) return send(res, 400, { error: 'username must be 3-32 chars: A-Z, a-z, 0-9, _ or -' }); if (state.clients.some(c => c.username === b.username)) return send(res, 409, { error: 'username already exists' }); const item = { id: id(), username: b.username, note: b.note || '', inbounds: Array.isArray(b.inbounds) ? b.inbounds : [], totalLimitBytes: Number(b.totalLimitBytes || 0), monthlyLimitBytes: Number(b.monthlyLimitBytes || 0), expiresAt: b.expiresAt || null, maxOnlineIps: Number(b.maxOnlineIps || 0), enabled: true, usedBytes: 0, createdAt: new Date().toISOString() }; state.clients.push(item); save(state); return send(res, 201, item); }
  if (req.method === 'PATCH' && url.pathname.startsWith('/api/clients/')) { const cid = url.pathname.split('/').pop(); const item = state.clients.find(x => x.id === cid); if (!item) return send(res, 404, { error: 'client not found' }); const b = await readBody(req); if (b.username && (!validUsername(b.username) || state.clients.some(x => x.id !== cid && x.username === b.username))) return send(res, 400, { error: 'invalid or duplicate username' }); Object.assign(item, b); save(state); return send(res, 200, item); }
  if (req.method === 'GET' && url.pathname.startsWith('/kota-sub/')) { const username = url.pathname.split('/').pop(); const client = state.clients.find(x => x.username === username); if (!client) return send(res, 404, { error: 'subscription not found' }, 'text/plain'); const nodes = state.inbounds.filter(x => client.inbounds.includes(x.id) && x.enabled).map(x => ({ tag: x.name, type: x.type, server: state.settings.domain || '127.0.0.1', server_port: x.port })); return send(res, 200, { username, updated_at: new Date().toISOString(), outbounds: nodes }, 'application/json'); }
  if (req.method === 'GET' && (url.pathname === '/' || url.pathname === '/index.html')) return send(res, 200, fs.readFileSync(path.join(root, 'public', 'index.html'), 'utf8'), 'text/html');
  return send(res, 404, { error: 'not found' });
};
const server = http.createServer((req, res) => routes(req, res).catch(err => send(res, 400, { error: err.message })));
server.listen(port, host, () => console.log(`KotaUI listening on http://${host}:${port}`));
