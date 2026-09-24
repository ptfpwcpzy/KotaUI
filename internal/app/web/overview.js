/* Network quality dashboard extension. */
(() => {
  const originalNav = window.nav;
  const originalDashboard = window.viewDashboard;
  const originalSettings = window.viewSettings;
  const colors = ['#3671ef', '#12af7f', '#7a61e8', '#d19524', '#de5b65', '#4aa8c4'];
  const hiddenTargets = new Set();
  let pingData = { targets: [], samples: [] };
  let networkLoaded = false;

  const style = document.createElement('style');
  style.textContent = `.network-quality-card{align-self:start;height:max-content;min-height:0;margin-top:18px;padding-bottom:16px}.network-quality-head{display:block}.network-quality-targets{display:grid;grid-template-columns:repeat(auto-fit,minmax(180px,max-content));gap:6px;margin-top:14px}.network-quality-target{display:flex;align-items:center;gap:7px;width:max-content;min-width:180px;height:30px;padding:0 9px;border:1px solid var(--line);border-radius:12px;background:#fbfcfe;color:var(--ink);cursor:pointer;font:inherit;text-align:left;box-shadow:none}.network-quality-target.active{border-color:#8db1f5;background:#edf4ff}.network-quality-target.is-hidden{opacity:.48}.network-quality-target .nq-dot{width:7px;height:7px;flex:0 0 7px;border-radius:50%}.network-quality-target .nq-name{max-width:82px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-size:10px;font-weight:700}.network-quality-target .nq-value{font-size:10px;font-weight:700;font-variant-numeric:tabular-nums;white-space:nowrap}.network-quality-target .nq-loss{color:var(--muted);font-weight:400}.nq-chart{width:100%;height:380px;display:block;background:transparent;border:0;text-rendering:geometricPrecision}.nq-empty{padding:24px;text-align:center;color:var(--muted)}.nq-target-list{display:grid;gap:8px;margin-top:12px}.nq-target-row{display:grid;grid-template-columns:1fr auto;align-items:center;gap:10px;padding:10px 12px;border-radius:12px;background:#f7f9fd}.nq-target-row small{display:block;color:var(--muted);margin-top:2px}.nq-target-row button{padding:6px 9px;border-radius:8px;background:#fff0f1;color:var(--danger);font-size:12px}@media(max-width:800px){.network-quality-targets{grid-template-columns:1fr;gap:5px}.network-quality-target{width:100%;min-width:0;padding:0 9px}.network-quality-target .nq-name{max-width:none;flex:1}.nq-chart{height:280px}}`;
  document.head.append(style);

  function latestFor(id) {
    return pingData.samples
      .filter(sample => sample.targetId === id)
      .sort((a, b) => new Date(b.checkedAt) - new Date(a.checkedAt))[0];
  }

  function displaySeries(targetID, start, end) {
    return pingData.samples
      .filter(sample => sample.targetId === targetID)
      .map(sample => ({
        time: new Date(sample.checkedAt).getTime(),
        value: sample.received > 0 && Number.isFinite(sample.avgMs) ? sample.avgMs : null,
      }))
      .filter(point => Number.isFinite(point.time) && point.time >= start && point.time <= end)
      .sort((a, b) => a.time - b.time);
  }

  function stepPath(points, x, y) {
    if (!points.length) return '';
    let path = `M${x(points[0].time)},${y(points[0].value)}`;
    for (let index = 1; index < points.length; index += 1) {
      const previous = points[index - 1];
      const current = points[index];
      path += ` L${x(current.time)},${y(previous.value)} L${x(current.time)},${y(current.value)}`;
    }
    return path;
  }

  function chart() {
    const mobile = window.matchMedia('(max-width: 800px)').matches;
    const width = mobile ? 360 : 900;
    const height = mobile ? 280 : 380;
    const left = mobile ? 42 : 48, right = mobile ? 10 : 14;
    const top = mobile ? 16 : 24, bottom = mobile ? 36 : 44;
    const innerW = width - left - right, innerH = height - top - bottom;
    const now = Date.now(), start = now - 24 * 60 * 60 * 1000;
    const seriesByTarget = pingData.targets.map(target => displaySeries(target.id, start, now));
    const values = seriesByTarget.flatMap((series, index) => hiddenTargets.has(pingData.targets[index].id) ? [] : series
      .filter(point => point.value !== null)
      .map(point => point.value));
    const maximum = Math.max(10, ...(values.length ? values : [100]));
    const yMax = maximum <= 100 ? 100 : maximum <= 200 ? 200 : 500;
    const x = time => left + Math.max(0, Math.min(1, (time - start) / (now - start))) * innerW;
    const y = value => top + innerH - (Math.max(0, value) / yMax) * innerH;
    let svg = `<svg class="nq-chart" viewBox="0 0 ${width} ${height}" preserveAspectRatio="none" role="img" aria-label="24小时延迟趋势"><style>text{font-family:Arial,"Noto Sans CJK SC","Microsoft YaHei",sans-serif;font-weight:400;letter-spacing:0;text-rendering:geometricPrecision}</style>`;
    for (let index = 0; index <= 4; index += 1) {
      const yy = top + innerH * index / 4;
      const value = (yMax * (4 - index) / 4).toFixed(0);
      svg += `<line x1="${left}" x2="${width - right}" y1="${yy}" y2="${yy}" stroke="#e4e9f0"/><text x="${left - 8}" y="${yy + 4}" text-anchor="end" fill="#78869a" font-size="12">${value}</text>`;
    }
    [0, .5, 1].forEach(position => {
      const xx = left + innerW * position;
      const date = new Date(start + (now - start) * position);
      svg += `<text x="${xx}" y="${height - 10}" text-anchor="${position === 0 ? 'start' : position === 1 ? 'end' : 'middle'}" fill="#78869a" font-size="12">${date.getMonth() + 1}/${date.getDate()} ${String(date.getHours()).padStart(2, '0')}:${String(date.getMinutes()).padStart(2, '0')}</text>`;
    });
    pingData.targets.forEach((target, index) => {
      if (hiddenTargets.has(target.id)) return;
      const series = seriesByTarget[index];
      let segment = [];
      const flush = () => {
        if (segment.length) svg += `<path d="${stepPath(segment, x, y)}" fill="none" stroke="${colors[index % colors.length]}" stroke-width="1.2" stroke-linecap="square" stroke-linejoin="miter"/>`;
        segment = [];
      };
      series.forEach(point => {
        if (point.value === null) { flush(); return; }
        segment.push(point);
      });
      flush();
    });
    return `${svg}</svg>`;
  }

  function renderNetwork(dashboard) {
    let card = dashboard.querySelector('.network-quality-card');
    if (!card) { card = document.createElement('section'); card.className = 'card section network-quality-card'; dashboard.append(card); }
    const targets = pingData.targets.map((target, index) => {
      const latest = latestFor(target.id);
      const latency = latest ? `${latest.avgMs.toFixed(1)}ms` : '--';
      const loss = latest ? `${latest.loss.toFixed(2)}%` : '--';
      const hidden = hiddenTargets.has(target.id);
      return `<button type="button" class="network-quality-target ${hidden ? 'is-hidden' : 'active'}" data-nq-target="${window.esc(target.id)}" title="${window.esc(target.name)}"><i class="nq-dot" style="background:${colors[index % colors.length]}"></i><b class="nq-name">${window.esc(target.name)}</b><strong class="nq-value">${latency}</strong><span class="nq-value nq-loss">${loss}</span></button>`;
    }).join('');
    card.innerHTML = `<div class="network-quality-head"><h2>网络质量</h2><p class="sub">最近 24 小时状态 · 每分钟检测</p></div>${pingData.targets.length ? chart() : '<div class="nq-empty">请在设置中添加 IP 或域名监测目标</div>'}<div class="network-quality-targets">${targets}</div>`;
    card.querySelectorAll('[data-nq-target]').forEach(button => button.onclick = () => {
      const id = button.dataset.nqTarget;
      if (hiddenTargets.has(id)) hiddenTargets.delete(id); else hiddenTargets.add(id);
      renderNetwork(dashboard);
    });
  }

  async function loadNetwork() {
    try {
      const response = await fetch('/api/network-quality', { credentials: 'same-origin', cache: 'no-store' });
      if (!response.ok) return;
      pingData = await response.json();
      const dashboard = document.querySelector('.dashboard-grid');
      if (dashboard) renderNetwork(dashboard);
    } catch {}
  }

  window.nav = function overviewNav() {
    return originalNav().replace('仪表盘</button>', '概览</button>');
  };

  window.viewDashboard = function overviewDashboard() {
    originalDashboard();
    document.querySelectorAll('[aria-label="返回仪表盘"]').forEach(element => element.setAttribute('aria-label', '返回概览'));
    document.querySelector('.dashboard-grid')?.querySelector('.address-strip')?.remove();
    if (window.__dashboardRefreshInProgress) return;
    if (!networkLoaded) {
      networkLoaded = true;
      loadNetwork();
    } else if (pingData.targets.length) {
      renderNetwork(document.querySelector('.dashboard-grid'));
    }
  };

  function renderSettingsTargets() {
    const form = document.querySelector('.settings-form');
    if (!form || document.querySelector('.ping-settings-card')) return;
    const card = document.createElement('section');
    card.className = 'card section ping-settings-card';
    card.style.marginTop = '18px';
    card.innerHTML = `<div class="section-head"><div><h2>网络质量监测</h2><p class="sub">每分钟从 VPS 向目标发送 3 次 Ping，保留最近 24 小时数据。</p></div></div><form class="fields" id="ping-target-form"><label>名称<input name="name" required maxlength="40" placeholder="例如：电信"></label><label>IP 或域名<input name="address" required maxlength="253" placeholder="例如：1.1.1.1 或 example.com"></label><div class="full"><button class="primary" type="submit">添加监测目标</button></div></form><div class="nq-target-list" id="ping-target-list"></div>`;
    form.closest('.card')?.insertAdjacentElement('afterend', card);
    card.querySelector('#ping-target-form').onsubmit = async event => {
      event.preventDefault();
      const data = Object.fromEntries(new FormData(event.target));
      try {
        const response = await fetch('/api/network-quality/targets', { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify(data) });
        const body = await response.json();
        if (!response.ok) throw Error(body.error || '添加失败');
        event.target.reset(); toast('监测目标已添加'); renderSettingsTargets();
      } catch (error) { toast(error.message); }
    };
    loadTargetList(card);
  }

  async function loadTargetList(card) {
    try {
      const response = await fetch('/api/network-quality', { credentials: 'same-origin' });
      if (!response.ok) return;
      const data = await response.json();
      const list = card.querySelector('#ping-target-list');
      list.innerHTML = data.targets.map(target => `<div class="nq-target-row"><div><b>${window.esc(target.name)}</b><small>${window.esc(target.address)}</small></div><button data-delete-ping="${target.id}">删除</button></div>`).join('') || '<div class="nq-empty">尚未添加监测目标</div>';
      list.querySelectorAll('[data-delete-ping]').forEach(button => button.onclick = async () => {
        if (!confirm('确定删除这个监测目标及其历史数据吗？')) return;
        await fetch('/api/network-quality/targets/' + button.dataset.deletePing, { method: 'DELETE' });
        renderSettingsTargets();
      });
    } catch {}
  }

  window.viewSettings = function () { originalSettings(); renderSettingsTargets(); };
  window.go('dashboard');
})();
