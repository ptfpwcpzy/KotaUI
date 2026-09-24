/* Homepage extras: 7-day traffic and VPS-client world map. */
(() => {
  const style = document.createElement('style');
  style.textContent = `.home-extras{max-width:1180px;margin:12px auto 0;display:grid;grid-template-columns:minmax(0,.86fr) minmax(0,1.14fr);gap:10px}
.week-card,.globe-card{padding:14px 16px}
.week-card h2,.globe-card h2{margin:0 0 8px;font-size:16px}
.week-bars{display:grid;grid-template-columns:repeat(7,minmax(0,1fr));gap:8px;align-items:end;height:118px}
.week-bar{display:flex;flex-direction:column;align-items:center;justify-content:flex-end;gap:3px;height:100%}
.week-bar i{display:block;width:100%;max-width:22px;border-radius:6px 6px 2px 2px;background:#2fbf7a;min-height:4px}
.week-bar b{color:var(--ink);font-size:10px;font-weight:700;line-height:1}
.week-bar span{color:var(--muted);font-size:10px}
.globe-wrap{display:grid;gap:8px}
.globe-svg{width:100%;height:148px;display:block}
.globe-meta{color:var(--muted);font-size:11px}
.globe-meta b{display:block;color:var(--ink);font-size:12px;margin-bottom:2px}
@media (max-width:800px),(max-aspect-ratio:3/4){.home-extras{grid-template-columns:1fr;margin-top:10px}.globe-svg{height:132px}.week-bars{height:108px}}`;
  document.head.append(style);

  function bytes(n) {
    return window.bytes ? window.bytes(n) : String(n || 0);
  }

  function weekCard() {
    const rows = window.dash && window.dash.dailyTraffic || [];
    const max = Math.max(1, ...rows.map((row) => Number(row.bytes) || 0));
    const bars = rows.map((row) => {
      const value = Number(row.bytes) || 0;
      const h = Math.max(4, Math.round((value / max) * 72));
      const label = String(row.date || '').slice(5);
      return `<div class="week-bar" title="${label} ${bytes(value)}"><b>${bytes(value)}</b><i style="height:${h}px"></i><span>${label}</span></div>`;
    }).join('');
    return `<section class="card section week-card"><h2>近7日流量</h2><div class="week-bars">${bars || '<span class="sub">暂无按天数据</span>'}</div></section>`;
  }

  function project(lat, lon) {
    return { x: 20 + ((lon + 180) / 360) * 360, y: 18 + ((90 - lat) / 180) * 112 };
  }

  function hashPoint(name) {
    let n = 0;
    for (const ch of String(name)) n = (n * 33 + ch.charCodeAt(0)) >>> 0;
    const lat = ((n % 120) - 50) * 0.7;
    const lon = ((Math.floor(n / 120) % 300) - 150);
    return project(lat, lon);
  }

  function dots() {
    const land = [
      [-100, 48], [-90, 42], [-80, 38], [-70, 46], [-112, 34], [-98, 28],
      [-64, -16], [-58, -28], [-68, -38],
      [8, 48], [2, 42], [12, 50], [18, 52],
      [12, 8], [20, 4], [8, -6], [28, -22],
      [78, 28], [88, 36], [104, 32], [118, 28], [126, 36], [138, 36],
      [135, -24], [146, -32]
    ];
    return land.map(([lon, lat]) => {
      const p = project(lat, lon);
      return `<circle cx="${p.x}" cy="${p.y}" r="2.1" fill="#9ecfb4"/>`;
    }).join('');
  }

  function globeCard() {
    const online = window.dash && window.dash.onlineUsers || [];
    const nets = window.dash && window.dash.network || [];
    const vps = project(22, 114);
    const lines = online.slice(0, 8).map((user, index) => {
      const point = hashPoint(user.username || user || index);
      return `<path d="M${vps.x},${vps.y} Q${(vps.x + point.x) / 2},${Math.min(vps.y, point.y) - 22} ${point.x},${point.y}" fill="none" stroke="#4d8fe8" stroke-width="1.4" opacity=".8"/><circle cx="${point.x}" cy="${point.y}" r="3" fill="#2fbf7a"/>`;
    }).join('');
    const esc = window.esc || ((v) => String(v));
    const names = online.slice(0, 8).map((user) => esc(user.username || user)).join('、') || '暂无在线客户端';
    const ip = Array.isArray(nets) ? nets.filter(Boolean).slice(0, 2).join(' · ') : '';
    return `<section class="card section globe-card"><h2>连接</h2><div class="globe-wrap"><svg class="globe-svg" viewBox="0 0 400 150" aria-hidden="true"><rect x="0" y="0" width="400" height="150" fill="#f7fbf8" rx="10"/><g fill="#cfe6d7">${Array.from({ length: 9 }, (_, row) => Array.from({ length: 22 }, (__, col) => `<circle cx="${18 + col * 17}" cy="${16 + row * 14}" r="1.05"/>`).join('')).join('')}</g>${dots()}<circle cx="${vps.x}" cy="${vps.y}" r="4" fill="#1f8a58"/><text x="${Math.min(vps.x + 8, 360)}" y="${vps.y - 6}" font-size="10" fill="#1f8a58">VPS</text>${lines}</svg><div class="globe-meta"><b>VPS ${esc(ip || '公网地址')}</b>在线：${names}</div></div></section>`;
  }

  const original = window.viewDashboard;
  window.viewDashboard = function extrasDashboard() {
    if (typeof original === 'function') original();
    const grid = document.querySelector('.dashboard-grid');
    if (!grid) return;
    let host = document.querySelector('#home-extras');
    if (!host) {
      host = document.createElement('div');
      host.id = 'home-extras';
      host.className = 'home-extras';
      const strips = grid.querySelector('.dashboard-strips');
      if (strips) strips.insertAdjacentElement('beforebegin', host);
      else grid.append(host);
    }
    host.innerHTML = weekCard() + globeCard();
  };
})();
