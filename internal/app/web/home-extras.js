/* Homepage extras: 7-day traffic only. */
(() => {
  const style = document.createElement('style');
  style.textContent = `.home-extras{max-width:1180px;margin:12px auto 0}
.week-card{padding:14px 16px}
.week-card h2{margin:0 0 8px;font-size:16px;font-weight:700}
.week-bars{display:grid;grid-template-columns:repeat(7,minmax(0,1fr));gap:8px;align-items:end;height:118px}
.week-bar{display:flex;flex-direction:column;align-items:center;justify-content:flex-end;gap:3px;height:100%}
.week-bar i{display:block;width:100%;max-width:22px;border-radius:6px 6px 2px 2px;background:#2fbf7a;min-height:4px}
.week-bar b{color:var(--ink);font-size:10px;font-weight:700;line-height:1}
.week-bar span{color:var(--muted);font-size:10px}
@media (max-width:800px),(max-aspect-ratio:3/4){.week-bars{height:108px}}`;
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

  function mountExtras() {
    const grid = document.querySelector('.dashboard-grid');
    if (!grid) return;
    let host = document.querySelector('#home-extras');
    if (!host) {
      host = document.createElement('div');
      host.id = 'home-extras';
      host.className = 'home-extras';
      const gauge = grid.querySelector('.gauge-card');
      if (gauge) gauge.insertAdjacentElement('afterend', host);
      else {
        const strips = grid.querySelector('.dashboard-strips');
        if (strips) strips.insertAdjacentElement('beforebegin', host);
        else grid.append(host);
      }
    }
    host.innerHTML = weekCard();
  }

  const original = window.viewDashboard;
  window.viewDashboard = function () {
    if (typeof original === 'function') original();
    mountExtras();
  };
})();
