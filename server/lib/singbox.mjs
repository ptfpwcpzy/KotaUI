import fs from 'node:fs';
import path from 'node:path';
import { spawnSync } from 'node:child_process';
import crypto from 'node:crypto';

export function keyForMethod(method) { if (method.includes('aes-128')) return crypto.randomBytes(16).toString('base64'); return crypto.randomBytes(32).toString('base64'); }
export function credentialsForClient(client) { return client.credentials || { reality: crypto.randomUUID(), hysteria2: crypto.randomBytes(18).toString('base64url'), shadowsocks2022: crypto.randomBytes(18).toString('base64url') }; }
function clientsFor(item, clients) { return clients.filter(x => x.enabled !== false && Array.isArray(x.inbounds) && x.inbounds.includes(item.id)); }
export function inboundToSingBox(item, clients = [], settings = {}) {
  const selected = clientsFor(item, clients);
  const base = { tag: item.name, listen: item.listen || '0.0.0.0', listen_port: Number(item.port) };
  if (item.type === 'reality') return { type: 'vless', ...base, users: selected.map(client => ({ name: client.username, uuid: credentialsForClient(client).reality })), tls: { enabled: true, server_name: item.handshakeServer, reality: { enabled: true, handshake: { server: item.handshakeServer, server_port: Number(item.handshakePort || 443) }, private_key: item.privateKey, short_id: [item.shortId] } } };
  if (item.type === 'hysteria2') return { type: 'hysteria2', ...base, up_mbps: Number(item.upMbps || 0), down_mbps: Number(item.downMbps || 0), users: selected.map(client => ({ name: client.username, password: credentialsForClient(client).hysteria2 })), obfs: item.obfsPassword ? { type: 'salamander', password: item.obfsPassword } : undefined, tls: { enabled: true, server_name: item.tlsServerName, certificate_path: item.certificatePath || settings.certFullchainPath || '/var/lib/kotaui/certs/fullchain.pem', key_path: item.keyPath || settings.certPrivateKeyPath || '/var/lib/kotaui/certs/privkey.pem' }, masquerade: item.masquerade || undefined };
  if (item.type === 'shadowsocks2022') return { type: 'shadowsocks', ...base, network: item.network || undefined, method: item.method, password: item.password || keyForMethod(item.method), users: selected.map(client => ({ name: client.username, password: credentialsForClient(client).shadowsocks2022 })) };
  throw new Error(`unsupported inbound type: ${item.type}`);
}
export function buildConfig(state, apiPort = 9090) { return { '$schema': 'https://sing-box.sagernet.org/schema.json', log: { level: 'info' }, inbounds: state.inbounds.filter(x => x.enabled).map(x => inboundToSingBox(x, state.clients, state.settings)), outbounds: [{ type: 'direct', tag: 'direct' }], experimental: { v2ray_api: { listen: `127.0.0.1:${apiPort}`, stats: { enabled: true, inbounds: state.inbounds.filter(x => x.enabled).map(x => x.name), users: state.clients.map(x => x.username) } } } }; }
export function writeConfig(state, configFile, apiPort = 9090) { const config = buildConfig(state, apiPort); fs.mkdirSync(path.dirname(configFile), { recursive: true, mode: 0o700 }); const tmp = `${configFile}.tmp`; fs.writeFileSync(tmp, JSON.stringify(config, null, 2), { mode: 0o600 }); fs.renameSync(tmp, configFile); return config; }
export function validateConfig(configFile, binary = 'sing-box') { const result = spawnSync(binary, ['check', '-c', configFile], { encoding: 'utf8' }); if (result.error?.code === 'ENOENT') return { ok: false, available: false, error: 'sing-box 未安装，无法执行真实配置校验' }; return { ok: result.status === 0, available: true, output: `${result.stdout || ''}${result.stderr || ''}`.trim() }; }
