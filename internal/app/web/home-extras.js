/* Homepage extras: 7-day traffic and VPS-client world map. */
(() => {
  const style = document.createElement('style');
  style.textContent = `.home-extras{max-width:1180px;margin:12px auto 0;display:grid;grid-template-columns:minmax(0,.86fr) minmax(0,1.14fr);gap:10px}.week-card,.globe-card{padding:14px 16px}.week-card h2,.globe-card h2{margin:0 0 8px;font-size:16px}.week-bars{display:grid;grid-template-columns:repeat(7,minmax(0,1fr));gap:8px;align-items:end;height:118px}.week-bar{display:flex;flex-direction:column;align-items:center;justify-content:flex-end;gap:3px;height:100%}.week-bar i{display:block;width:100%;max-width:22px;border-radius:6px 6px 2px 2px;background:#2fbf7a;min-height:4px}.week-bar b{color:var(--ink);font-size:10px;font-weight:700;line-height:1}.week-bar span{color:var(--muted);font-size:10px}.globe-wrap{display:grid;gap:8px}.globe-svg{width:100%;height:148px;display:block}.globe-meta{color:var(--muted);font-size:11px}.globe-meta b{display:block;color:var(--ink);font-size:12px;margin-bottom:2px}@media (max-width:800px),(max-aspect-ratio:3/4){.home-extras{grid-template-columns:1fr;margin-top:10px}.globe-svg{height:132px}.week-bars{height:108px}}`;
  document.head.append(style);

  function bytes(n) { return window.bytes ? window.bytes(n) : String(n || 0); }

  function weekCard() {
    const rows = window.dash && window.dash.dailyTraffic || [];
    const max = Math.max(1, ...rows.map(row => Number(row.bytes) || 0));
    const bars = rows.map(row => {
      const value = Number(row.bytes) || 0;
      const h = Math.max(4, Math.round((value / max) * 72));
      const label = String(row.date || '').slice(5);
      return `<div class="week-bar" title="${label} ${bytes(value)}"><b>${bytes(value)}</b><i style="height:${h}px"></i><span>${label}</span></div>`;
    }).join('');
    return `<section class="card section week-card"><h2>近7日流量</h2><div class="week-bars">${bars || '<span class="sub">暂无按天数据</span>'}</div></section>`;
  }

  function project(lat, lon) { return { x: 20 + ((lon + 180) / 360) * 360, y: 18 + ((90 - lat) / 180) * 112 }; }

  function hashPoint(name) {
    let n = 0;
    for (const ch of String(name)) n = (n * 33 + ch.charCodeAt(0)) >>> 0;
    return project(((n % 120) - 50) * 0.7, (Math.floor(n / 120) % 300) - 150);
  }

  // Simplified continent silhouettes in equirectangular coordinates. Dots
  // are clipped to land instead of filling the whole map rectangle.
  function worldMap() {
    return `<defs><pattern id="world-dots" width="5" height="5" patternUnits="userSpaceOnUse"><circle cx="1.5" cy="1.5" r="1.05" fill="#9bd5bc"/></pattern><clipPath id="world-land"><path d="M34 43 42 32 58 27 73 29 86 37 98 38 111 48 104 57 91 58 83 66 67 62 57 67 47 59 37 58 28 51Z"/><path d="M108 76 119 78 128 87 126 99 118 105 114 119 106 125 99 115 101 103 94 95 99 84Z"/><path d="M185 43 196 36 210 35 219 40 230 40 242 47 258 44 274 50 294 48 315 55 333 62 355 68 363 78 351 83 333 79 320 86 305 81 294 87 283 82 268 88 252 82 241 89 228 82 218 88 208 80 197 81 188 72 177 69Z"/><path d="M195 76 207 78 215 88 211 99 205 109 201 122 192 130 182 126 179 114 184 103 181 91Z"/><path d="M301 106 311 108 320 115 316 123 304 122 297 115Z"/><path d="M92 21 101 19 107 24 103 31 96 29Z"/></clipPath></defs><rect x="20" y="18" width="360" height="112" fill="url(#world-dots)" clip-path="url(#world-land)"/>`;
  }

  function globeCard() {
    const online = window.dash && window.dash.onlineUsers || [];
    const nets = window.dash && window.dash.network || [];
    const vps = project(22, 114);
    const lines = online.slice(0, 8).map(user => {
      const point = hashPoint(user.username || user);
      return `<path d="M${vps.x},${vps.y} Q${(vps.x + point.x) / 2},${Math.min(vps.y, point.y) - 22} ${point.x},${point.y}" fill="none" stroke="#4d8fe8" stroke-width="1.2" opacity=".78"/><circle cx="${point.x}" cy="${point.y}" r="3" fill="#2fbf7a"/>`;
    }).join('');
    const esc = window.esc || (value => String(value));
    const names = online.slice(0, 8).map(user => esc(user.username || user)).join('、') || '暂无在线客户端';
    const ip = Array.isArray(nets) ? nets.filter(Boolean).slice(0, 2).join(' · ') : '';
    return `<section class="card section globe-card"><h2>全球网络拓扑</h2><div class="globe-wrap"><svg class="globe-svg" viewBox="0 0 400 150" aria-hidden="true">${worldMap()}<circle cx="${vps.x}" cy="${vps.y}" r="4" fill="#1f8a58"/><text x="${Math.min(vps.x + 8, 360)}" y="${vps.y - 6}" font-size="10" fill="#1f8a58">VPS</text>${lines}</svg><div class="globe-meta"><b>VPS ${esc(ip || '公网地址')}</b>在线：${names}</div></div></section>`;
  }

  function dataSignature() {
    const dash = window.dash || {};
    return JSON.stringify([dash.dailyTraffic || [], dash.onlineUsers || [], dash.network || []]);
  }

  const original = window.viewDashboard;
  window.viewDashboard = function extrasDashboard() {
    const previous = document.querySelector('#home-extras');
    if (previous) previous.remove();
    if (typeof original === 'function') original();
    const grid = document.querySelector('.dashboard-grid');
    if (!grid) return;
    const host = previous || Object.assign(document.createElement('div'), { id: 'home-extras', className: 'home-extras' });
    const strips = grid.querySelector('.dashboard-strips');
    if (strips) strips.insertAdjacentElement('beforebegin', host); else grid.append(host);
    const signature = dataSignature();
    if (host.dataset.signature !== signature) {
      host.innerHTML = weekCard() + globeCard();
      host.dataset.signature = signature;
    }
  };
})();
