/* Homepage extras: 7-day traffic and compact VPS/client globe. */
(() => {
  const style = document.createElement('style');
  style.textContent = `.home-extras{max-width:1180px;margin:12px auto 0;display:grid;grid-template-columns:minmax(0,.9fr) minmax(0,1.1fr);gap:10px}
.week-card,.globe-card{padding:14px 16px}
.week-card h2,.globe-card h2{margin:0 0 8px;font-size:16px}
.week-bars{display:grid;grid-template-columns:repeat(7,minmax(0,1fr));gap:8px;align-items:end;height:88px}
.week-bar{display:flex;flex-direction:column;align-items:center;justify-content:flex-end;gap:4px;height:100%}
.week-bar i{display:block;width:100%;max-width:22px;border-radius:6px 6px 2px 2px;background:#2fbf7a;min-height:4px}
.week-bar span{color:var(--muted);font-size:10px}
.globe-wrap{display:grid;grid-template-columns:140px minmax(0,1fr);gap:12px;align-items:center}
.globe-svg{width:140px;height:140px}
.globe-meta{display:grid;gap:6px}
.globe-meta b{font-size:12px}
.globe-meta small{color:var(--muted);font-size:11px}
@media (max-width:800px),(max-aspect-ratio:3/4){.home-extras{grid-template-columns:1fr;margin-top:10px}.globe-wrap{grid-template-columns:110px minmax(0,1fr)}.globe-svg{width:110px;height:110px}.week-bars{height:72px}}`;
  document.head.append(style);

  function bytes(n) {
    return window.bytes ? window.bytes(n) : String(n || 0);
  }

  function weekCard() {
    const rows = window.dash?.dailyTraffic || [];
    const max = Math.max(1, ...rows.map((row) => Number(row.bytes) || 0));
    const bars = rows.map((row) => {
      const value = Number(row.bytes) || 0;
      const h = Math.max(4, Math.round((value / max) * 72));
      const label = String(row.date || '').slice(5);
      return `<div class="week-bar" title="${label} ${bytes(value)}"><i style="height:${h}px"></i><span>${label}</span></div>`;
    }).join('');
    return `<section class="card section week-card"><h2>近7日流量</h2><div class="week-bars">${bars || '<span class="sub">暂无按天数据</span>'}</div></section>`;
  }

  function hashPoint(name) {
    let n = 0;
    for (const ch of String(name)) n = (n * 33 + ch.charCodeAt(0)) >>> 0;
    const lat = ((n % 140) - 70) * 0.8;
    const lon = ((Math.floor(n / 140) % 360) - 180);
    return project(lat, lon);
  }

  function project(lat, lon) {
    const x = 70 + (lon / 180) * 52;
    const y = 70 - (lat / 90) * 40;
    return { x, y };
  }

  function globeCard() {
    const online = window.dash?.onlineUsers || [];
    const nets = window.dash?.network || [];
    const vps = project(20, 110);
    const lines = online.slice(0, 8).map((user, index) => {
      const point = hashPoint(user.username || user || index);
      return `<path d="M${vps.x},${vps.y} Q${(vps.x + point.x) / 2},${Math.min(vps.y, point.y) - 18} ${point.x},${point.y}" fill="none" stroke="#4d8fe8" stroke-width="1.2" opacity=".75"/><circle cx="${point.x}" cy="${point.y}" r="2.4" fill="#2fbf7a"/>`;
    }).join('');
    const names = online.slice(0, 8).map((user) => window.esc(user.username || user)).join('、') || '暂无在线客户端';
    const ip = Array.isArray(nets) ? nets.filter(Boolean).slice(0, 2).join(' · ') : '';
    return `<section class="card section globe-card"><h2>连接</h2><div class="globe-wrap"><svg class="globe-svg" viewBox="0 0 140 140" aria-hidden="true"><circle cx="70" cy="70" r="58" fill="#f7fbf8" stroke="#d7e6dc"/><g fill="#b7d7c4">${Array.from({ length: 18 }, (_, row) => Array.from({ length: 24 }, (__, col) => {
      const y = 18 + row * 6.2;
      const x = 18 + col * 4.4 + (row % 2 ? 2 : 0);
      const dx = x - 70, dy = y - 70;
      if (dx * dx + dy * dy > 50 * 50) return '';
      return `<circle cx="${x}" cy="${y}" r="0.9"/>`;
    }).join('')).join('')}</g><circle cx="${vps.x}" cy="${vps.y}" r="3.2" fill="#1f8a58"/>${lines}</svg><div class="globe-meta"><b>VPS ${window.esc(ip || '公网地址')}</b><small>在线：${names}</small></div></div></section>`;
  }

  const original = window.viewDashboard;
  window.viewDashboard = function extrasDashboard() {
    original();
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
