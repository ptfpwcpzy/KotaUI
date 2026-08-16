import crypto from 'node:crypto';

const NOTICE = '作者那么羡慕你，仅供学习自用，请勿随意传播。';

function encode(value) { return encodeURIComponent(String(value || '')); }
function base64Url(value) { return Buffer.from(String(value), 'utf8').toString('base64url'); }
function bytes(value) { return Math.max(0, Number(value || 0)); }

export function formatBytes(value) {
  const raw = bytes(value);
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let amount = raw;
  let unit = 0;
  while (amount >= 1024 && unit < units.length - 1) { amount /= 1024; unit += 1; }
  return `${amount >= 100 || unit === 0 ? amount.toFixed(0) : amount.toFixed(1)} ${units[unit]}`;
}

export function clientStatus(client, timeZone = 'UTC', now = new Date()) {
  if (client.enabled === false) return { enabled: false, reason: '已暂停' };
  if (client.expiresAt && new Date(client.expiresAt).getTime() <= now.getTime()) return { enabled: false, reason: '已到期' };
  if (Number(client.totalLimitBytes || 0) > 0 && bytes(client.usedBytes) >= Number(client.totalLimitBytes)) return { enabled: false, reason: '总流量已用尽' };
  const month = new Intl.DateTimeFormat('en-CA', { year: 'numeric', month: '2-digit', timeZone }).format(now);
  if (client.monthKey === month && Number(client.monthlyLimitBytes || 0) > 0 && bytes(client.monthlyUsedBytes) >= Number(client.monthlyLimitBytes)) return { enabled: false, reason: '本月流量已用尽' };
  return { enabled: true, reason: '正常' };
}

function titleFor(inbound, client, settings = {}) {
  if (!settings.titleEnabled) return inbound.name;
  const total = Number(client.totalLimitBytes || 0);
  const used = bytes(client.usedBytes);
  const pieces = [];
  for (const field of settings.titleFields || ['username', 'used', 'total']) {
    if (field === 'username') pieces.push(client.username);
    if (field === 'used') pieces.push(`已用 ${formatBytes(used)}`);
    if (field === 'total' && total > 0) pieces.push(`总量 ${formatBytes(total)}`);
    if (field === 'remaining' && total > 0) pieces.push(`剩余 ${formatBytes(Math.max(0, total - used))}`);
    if (field === 'monthly') pieces.push(`本月 ${formatBytes(client.monthlyUsedBytes)}`);
    if (field === 'expiry' && client.expiresAt) pieces.push(`到期 ${String(client.expiresAt).slice(0, 10)}`);
  }
  return [inbound.name, ...pieces].filter(Boolean).join(' · ');
}

function nodeServer(settings, requestHost = '') {
  const configured = String(settings.domain || '').trim();
  if (configured) return configured.replace(/^https?:\/\//, '').split(':')[0];
  return String(requestHost || '127.0.0.1').replace(/^\[|\]$/g, '').split(':')[0];
}

function realityLink(inbound, client, settings, server) {
  const uuid = client.credentials?.reality || '';
  const params = new URLSearchParams({
    encryption: 'none', security: 'reality', type: 'tcp',
    sni: inbound.sni || inbound.handshakeServer || '',
    fp: inbound.fingerprint || 'chrome',
    pbk: inbound.publicKey || '',
    sid: inbound.shortId || '',
    flow: inbound.flow || 'xtls-rprx-vision',
  });
  return `vless://${encode(uuid)}@${server}:${Number(inbound.port)}?${params.toString()}#${encode(titleFor(inbound, client, settings))}`;
}

function hysteriaLink(inbound, client, settings, server) {
  const params = new URLSearchParams({ sni: inbound.tlsServerName || settings.domain || '' });
  if (inbound.obfsPassword) { params.set('obfs', 'salamander'); params.set('obfs-password', inbound.obfsPassword); }
  return `hysteria2://${encode(client.credentials?.hysteria2 || '')}@${server}:${Number(inbound.port)}?${params.toString()}#${encode(titleFor(inbound, client, settings))}`;
}

function shadowsocksLink(inbound, client, settings, server) {
  // AEAD-2022 multi-user clients need the server PSK followed by the client PSK.
  const password = `${inbound.password || ''}:${client.credentials?.shadowsocks2022 || ''}`;
  const userinfo = `${encode(inbound.method || '2022-blake3-aes-256-gcm')}:${encode(password)}`;
  return `ss://${userinfo}@${server}:${Number(inbound.port)}#${encode(titleFor(inbound, client, settings))}`;
}

export function subscriptionLinks(state, client, requestHost = '') {
  const server = nodeServer(state.settings, requestHost);
  return state.inbounds
    .filter(inbound => inbound.enabled !== false && Array.isArray(client.inbounds) && client.inbounds.includes(inbound.id))
    .map(inbound => {
      if (inbound.type === 'reality') return realityLink(inbound, client, state.settings, server);
      if (inbound.type === 'hysteria2') return hysteriaLink(inbound, client, state.settings, server);
      if (inbound.type === 'shadowsocks2022') return shadowsocksLink(inbound, client, state.settings, server);
      return null;
    })
    .filter(Boolean);
}

function escapeHtml(value) { return String(value || '').replace(/[&<>'"]/g, char => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', "'": '&#39;', '"': '&quot;' }[char])); }

export function subscriptionHtml(state, client, requestHost = '') {
  const status = clientStatus(client, state.settings.timeZone || 'UTC');
  const total = Number(client.totalLimitBytes || 0);
  const remaining = total > 0 ? Math.max(0, total - bytes(client.usedBytes)) : 0;
  const links = subscriptionLinks(state, client, requestHost);
  const subUrl = `https://${escapeHtml(requestHost || state.settings.domain)}:${Number(state.settings.subscriptionPort || 1109)}${escapeHtml(state.settings.subscriptionPath || '/kota-sub')}/${encode(client.username)}`;
  return `<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>${escapeHtml(state.settings.panelName || 'KotaUI')} · ${escapeHtml(client.username)}</title><style>body{margin:0;background:#f6f8fb;color:#1c2533;font:15px system-ui,-apple-system,"Microsoft YaHei",sans-serif}.wrap{max-width:720px;margin:48px auto;padding:0 18px}.card{background:#fff;border:1px solid #e5eaf1;border-radius:18px;padding:26px;box-shadow:0 10px 36px #23334a0b}h1{margin:0;font-size:27px}h2{font-size:16px;margin:26px 0 10px}.muted{color:#6d7785}.notice{margin:18px 0;padding:12px 14px;border-left:3px solid #2f7df6;background:#f3f7ff;color:#38547b;border-radius:6px}.stats{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:10px}.stat{padding:13px;background:#f8fafc;border-radius:10px}.stat b{display:block;font-size:18px;margin-top:4px}.link{display:block;word-break:break-all;padding:10px;border:1px dashed #cfd8e6;border-radius:8px;margin:8px 0;color:#155dc4;text-decoration:none}.status{color:${status.enabled ? '#118b4d' : '#c52d3d'};font-weight:700}</style></head><body><main class="wrap"><section class="card"><p class="muted">${escapeHtml(state.settings.panelName || 'KotaUI')}</p><h1>${escapeHtml(client.username)}</h1><p class="status">${escapeHtml(status.reason)}</p><p class="notice">${NOTICE}</p><div class="stats"><div class="stat"><span class="muted">累计已用</span><b>${formatBytes(client.usedBytes)}</b></div><div class="stat"><span class="muted">总流量</span><b>${total > 0 ? formatBytes(total) : '不限'}</b></div><div class="stat"><span class="muted">剩余流量</span><b>${total > 0 ? formatBytes(remaining) : '不限'}</b></div><div class="stat"><span class="muted">本月已用</span><b>${formatBytes(client.monthlyUsedBytes)}</b></div></div><h2>订阅地址</h2><a class="link" href="${subUrl}">${subUrl}</a><h2>节点数量：${links.length}</h2><p class="muted">请在支持订阅的客户端中直接添加上述订阅地址。浏览器打开时显示本页面；兼容客户端请求时返回订阅内容。</p></section></main></body></html>`;
}

export function subscriptionPayload(state, client, requestHost = '') {
  const links = subscriptionLinks(state, client, requestHost);
  const upload = bytes(client.usedBytes) / 2;
  const download = bytes(client.usedBytes) - upload;
  const total = Number(client.totalLimitBytes || 0);
  const expire = client.expiresAt ? Math.floor(new Date(client.expiresAt).getTime() / 1000) : 0;
  return {
    body: Buffer.from(links.join('\n'), 'utf8').toString('base64'),
    headers: {
      'Content-Type': 'text/plain; charset=utf-8',
      'Profile-Title': base64Url(`${state.settings.panelName || 'KotaUI'} · ${client.username}`),
      'Subscription-Userinfo': `upload=${Math.floor(upload)}; download=${Math.floor(download)}; total=${Math.max(0, total)}; expire=${expire}`,
      'Content-Disposition': `attachment; filename="${client.username}.txt"`,
    },
  };
}

export function isSubscriptionClient(userAgent = '', query = '') {
  const source = `${userAgent} ${query}`.toLowerCase();
  return /shadowrocket|v2rayng|v2rayn|sing-box|clash|surge|sub=1|format=/.test(source);
}

export function subscriptionNotice() { return NOTICE; }

export function newSubscriptionSecret() { return crypto.randomBytes(18).toString('base64url'); }
