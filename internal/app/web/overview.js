/* Network quality dashboard extension. */
(() => {
  const originalNav = window.nav;
  const originalDashboard = window.viewDashboard;
  const originalSettings = window.viewSettings;
  const colors = ['#3671ef', '#12af7f', '#7a61e8', '#d19524', '#de5b65', '#4aa8c4'];
  const networkRefreshInterval = 2 * 60 * 1000;
  const hiddenTargets = new Set();
  let pingData = { targets: [], samples: [] };
  let networkLoadPromise = null;
  let networkRetryAt = 0;
  let networkNextRefreshAt = 0;
  let networkFailureCount = 0;

  const style = document.createElement('style');
  style.textContent = `.network-quality-card{align-self:start;height:max-content;min-height:0;margin-top:18px;padding-bottom:16px}.network-quality-head{display:block}.network-quality-targets{display:grid;grid-template-columns:repeat(auto-fit,minmax(180px,max-content));gap:6px;margin-top:14px}.network-quality-target{display:flex;align-items:center;gap:7px;width:max-content;min-width:180px;height:30px;padding:0 9px;border:1px solid var(--line);border-radius:12px;background:#fbfcfe;color:var(--ink);cursor:pointer;font:inherit;text-align:left;box-shadow:none}.network-quality-target.active{border-color:#8db1f5;background:#edf4ff}.network-quality-target.is-hidden{opacity:.48}.network-quality-target .nq-dot{width:7px;height:7px;flex:0 0 7px;border-radius:50%}.network-quality-target .nq-name{max-width:82px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-size:10px;font-weight:700}.network-quality-target .nq-value{font-size:10px;font-weight:700;font-variant-numeric:tabular-nums;white-space:nowrap}.network-quality-target .nq-loss{color:var(--muted);font-weight:400}.nq-chart{width:100%;height:380px;display:block;background:transparent;border:0;text-rendering:geometricPrecision}.nq-empty{padding:24px;text-align:center;color:var(--muted)}.nq-target-list{display:grid;gap:8px;margin-top:12px}.nq-target-row{display:grid;grid-template-columns:1fr auto;align-items:center;gap:10px;padding:10px 12px;border-radius:12px;background:#f7f9fd}.nq-target-row small{display:block;color:var(--muted);margin-top:2px}.nq-target-row button{padding:6px 9px;border-radius:8px;background:#fff0f1;color:var(--danger);font-size:12px}@media(max-width:800px){.network-quality-targets{grid-template-columns:1fr;gap:5px}.network-quality-target{width:100%;min-width:0;padding:0 9px}.network-quality-target .nq-name{max-width:none;flex:1}.nq-chart{height:280px}}.network-quality-host{display:block!important;align-self:center!important;width:100%!important;max-width:1180px!important;margin:18px auto 0!important;padding:0!important;box-sizing:border-box}.network-quality-card{display:block!important;width:100%!important;max-width:none!important;box-sizing:border-box;margin:0!important;padding:18px 20px 16px!important;border-radius:22px!important}.network-quality-head{display:flex;align-items:baseline;justify-content:space-between;gap:12px}.network-quality-head .sub{margin:0}.nq-chart{height:360px}@media(max-width:800px){.network-quality-host{display:block!important;width:100%!important;max-width:none;margin-top:12px;padding:0}.network-quality-card{padding:12px 10px!important}.network-quality-head{display:block}.network-quality-head .sub{margin-top:2px}.nq-chart{height:280px}}.route-map-host{display:block!important;width:100%!important;max-width:1180px!important;margin:18px auto 0!important;padding:0!important;box-sizing:border-box}.route-map-card{padding:18px 20px 16px}.route-map-card .sub{color:var(--muted);font-size:12px;margin-top:6px}.route-map-card .sub b{color:var(--ink)}.route-map-card .pathline{display:flex;flex-wrap:wrap;align-items:center;gap:6px 8px;margin-top:14px;padding-top:12px;border-top:1px solid var(--line);font-size:13px}.route-map-card .pathline b{font-weight:700}.route-map-card .pathline .arr{color:var(--muted)}.route-map-card .pathline .ms{font-weight:700;margin-left:2px}.route-map-card .foot2{display:flex;margin-top:12px;color:var(--muted);font-size:12px}.route-map-card .legend{display:flex;gap:12px;align-items:center}.route-map-card .legend i{display:inline-block;width:10px;height:10px;border-radius:3px;margin-right:4px;vertical-align:-1px}.route-map-card .legend .g{background:#2fbf7a}.route-map-card .legend .o{background:#e69f00}.route-map-card .legend .r{background:#e5484d}.route-map-card .tg{color:#2fbf7a}.route-map-card .to{color:#e69f00}.route-map-card .tr{color:#e5484d}@media(max-width:800px){.route-map-card{padding:16px}}.route-map-card .mapwrap{margin-top:10px;border:1px solid var(--line);border-radius:16px;background:#f7fbff;overflow:hidden}.route-map-card .mapwrap svg{display:block;width:100%;height:auto}.route-land{fill:#e6ecf4;stroke:#d7dfeb;stroke-width:.5}.route-grat{stroke:#e2e9f2;stroke-width:.5;fill:none}.route-line{fill:none;stroke:#3569ee;stroke-width:2.5;stroke-linecap:round;stroke-linejoin:round}.route-flow{fill:none;stroke:#fff;stroke-width:2.5;stroke-linecap:round;stroke-dasharray:2 14;animation:route-flow 1.2s linear infinite}@keyframes route-flow{to{stroke-dashoffset:-16}}.route-hop{stroke:#fff;stroke-width:2}.route-lbl{font-size:11px;font-weight:600;fill:#15223a}.route-sub{font-size:10px;fill:#78869a}.route-num{font-size:10px;font-weight:700;fill:#15223a}`;
  document.head.append(style);

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

  // Keep the SVG light on mobile while retaining local min/max points so a
  // short latency spike is not smoothed away by downsampling.
  function compactSeries(points, limit) {
    if (points.length <= limit) return points;
    const result = [];
    const bucketSize = (points.length - 1) / Math.max(1, limit - 1);
    for (let bucket = 0; bucket < limit; bucket += 1) {
      const from = Math.floor(bucket * bucketSize);
      const to = Math.min(points.length, Math.max(from + 1, Math.floor((bucket + 1) * bucketSize)));
      const candidates = points.slice(from, to);
      if (!candidates.length) continue;
      const chosen = [candidates[0], candidates[candidates.length - 1]];
      const valid = candidates.filter(point => point.value !== null);
      if (valid.length) {
        chosen.push(valid.reduce((a, b) => a.value < b.value ? a : b));
        chosen.push(valid.reduce((a, b) => a.value > b.value ? a : b));
      }
      chosen.sort((a, b) => a.time - b.time).forEach(point => {
        const previous = result[result.length - 1];
        if (!previous || previous.time !== point.time || previous.value !== point.value) result.push(point);
      });
    }
    return result;
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
    const seriesByTarget = pingData.targets.map(target => compactSeries(displaySeries(target.id, start, now), mobile ? 240 : 720));
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
      const summary = pingData.summary24h?.[target.id];
      const hasSummary = summary && summary.checks > 0;
      const latency = hasSummary ? `${summary.averageMs.toFixed(1)}ms` : '--';
      const loss = hasSummary ? `${summary.lossPercent.toFixed(2)}%` : '--';
      const hidden = hiddenTargets.has(target.id);
      return `<button type="button" class="network-quality-target ${hidden ? 'is-hidden' : 'active'}" data-nq-target="${window.esc(target.id)}" title="${window.esc(target.name)}"><i class="nq-dot" style="background:${colors[index % colors.length]}"></i><b class="nq-name">${window.esc(target.name)}</b><strong class="nq-value">${latency}</strong><span class="nq-value nq-loss">${loss}</span></button>`;
    }).join('');
    card.innerHTML = `<div class="network-quality-head"><h2>网络质量</h2></div>${pingData.targets.length ? chart() : '<div class="nq-empty">请在设置中添加 IP 或域名监测目标</div>'}<div class="network-quality-targets">${targets}</div>`;
    card.querySelectorAll('[data-nq-target]').forEach(button => button.onclick = () => {
      const id = button.dataset.nqTarget;
      if (hiddenTargets.has(id)) hiddenTargets.delete(id); else hiddenTargets.add(id);
      renderNetwork(dashboard);
    });
  }

  function ensureNetworkHost() {
    const view = document.querySelector('#view');
    if (!view) return null;
    let host = document.querySelector('#network-quality-host');
    if (!host) {
      host = document.createElement('div');
      host.id = 'network-quality-host';
      host.className = 'network-quality-host';
    }
    // The main dashboard is rebuilt by the metrics poller. Reinsert the host
    // as a sibling every time instead of relying on a stale DOM reference.
    if (host.parentNode !== view.parentNode || host.previousElementSibling !== view) {
      view.insertAdjacentElement('afterend', host);
    }
    return host;
  }

  async function loadNetwork() {
    if (networkLoadPromise) return networkLoadPromise;
    if (Date.now() < networkRetryAt || Date.now() < networkNextRefreshAt) return;
    networkLoadPromise = (async () => {
      const controller = new AbortController();
      const timeout = setTimeout(() => controller.abort(), 8000);
      try {
        const response = await fetch('/api/network-quality', { credentials: 'same-origin', cache: 'no-store', signal: controller.signal });
        if (!response.ok) throw new Error(`network-quality: ${response.status}`);
        pingData = await response.json();
        if (!pingData.targets) pingData.targets = [];
        if (!pingData.samples) pingData.samples = [];
        if (!pingData.summary24h) pingData.summary24h = {};
        networkFailureCount = 0;
        networkRetryAt = 0;
        networkNextRefreshAt = Date.now() + networkRefreshInterval;
        if (document.querySelector('#view .dashboard-grid')) {
          const host = ensureNetworkHost();
          if (host) renderNetwork(host);
        }
      } catch {
        // A direct route can temporarily stall while a proxy route works.
        // Do not start another request every 2 seconds; retry with backoff.
        networkFailureCount += 1;
        networkRetryAt = Date.now() + Math.min(30000, 2000 * (2 ** Math.min(networkFailureCount - 1, 4)));
      } finally {
        clearTimeout(timeout);
      }
      networkLoadPromise = null;
    })();
    return networkLoadPromise;
  }

  window.nav = function overviewNav() {
    return originalNav().replace('仪表盘</button>', '概览</button>');
  };


  // 回程路由：主页打开时探测一次，只显示一串箭头，不画地图。
  let routeLoaded = false;

  function ensureRouteHost() {
    const view = document.querySelector('#view');
    if (!view) return null;
    let host = document.querySelector('#route-map-host');
    if (!host) {
      host = document.createElement('div');
      host.id = 'route-map-host';
      host.className = 'route-map-host';
    }
    if (host.parentNode !== view.parentNode || host.previousElementSibling !== view) {
      view.insertAdjacentElement('afterend', host);
    }
    return host;
  }

  // 地图用中国常用样式：以 150°E 为中央经线，中国在左、太平洋居中、美洲在右。
  // 地图左右边缘是西经 30°，跨过边缘的陆地和路由都在边缘处切开，不横穿整张图。
  const ROUTE_W = 1000, ROUTE_H = 500;
  function routeSx(lon) { return ((lon + 30) % 360 + 360) % 360; }
  function routePx(sx) { return sx / 360 * ROUTE_W; }
  function routePy(lat) { return (85 - lat) / 145 * ROUTE_H; }
  function routeGrat() {
    let d = '';
    for (let lon = -180; lon < 180; lon += 30) d += `M${routePx(routeSx(lon))},0V${ROUTE_H}`;
    for (let lat = -60; lat <= 90; lat += 30) d += `M0,${routePy(lat)}H${ROUTE_W}`;
    return d;
  }
  // 把一组经纬点展开成连续的横坐标（相邻两点差值不超过 180°），
  // 避免跨过地图边缘的线在输出坐标里横穿整张图。
  function routeUnwrap(pts) {
    const out = [];
    let offset = 0, prevU = null;
    pts.forEach(([lon, lat]) => {
      let u = routeSx(lon) + offset;
      if (prevU !== null) {
        while (u - prevU > 180) { u -= 360; offset -= 360; }
        while (u - prevU < -180) { u += 360; offset += 360; }
      }
      out.push([u, lat]);
      prevU = u;
    });
    return out;
  }
  // 陆地：展开后按需平移绘制，超出地图的部分由 clipPath 切掉，格陵兰/南极洲不会拉出横穿线。
  function routeLand() {
    let out = '';
    LAND.forEach(poly => {
      if (poly.length < 3) return;
      const pts = routeUnwrap(poly);
      let umin = Infinity, umax = -Infinity;
      pts.forEach(([u]) => { if (u < umin) umin = u; if (u > umax) umax = u; });
      [0, -360, 360].forEach(shift => {
        if (umax < -shift || umin >= -shift + 360) return;
        out += `<path class="route-land" clip-path="url(#route-clip)" d="M${pts.map(([u, lat]) => routePx(u + shift).toFixed(1) + ',' + routePy(lat).toFixed(1)).join('L')}Z"/>`;
      });
    });
    return out;
  }
  // 路由：展开后按地图边缘切开，每段单独画，不横穿整张图。
  function routeSplit(hops) {
    const pts = routeUnwrap(hops.map(h => [h.lon, h.lat]));
    const segs = [];
    let cur = [];
    let prevU = null, prevLat = null;
    pts.forEach(([u, lat]) => {
      if (prevU === null) {
        cur = [[((u % 360) + 360) % 360, lat]];
        prevU = u; prevLat = lat;
        return;
      }
      const dir = u >= prevU ? 1 : -1;
      let k = dir > 0 ? Math.floor(prevU / 360) + 1 : Math.ceil(prevU / 360) - 1;
      while ((dir > 0 && 360 * k < u - 1e-9) || (dir < 0 && 360 * k > u + 1e-9)) {
        const cutU = 360 * k;
        const t = (cutU - prevU) / (u - prevU);
        const latEdge = prevLat + (lat - prevLat) * t;
        cur.push([dir > 0 ? 360 : 0, latEdge]);
        segs.push(cur);
        cur = [[dir > 0 ? 0 : 360, latEdge]];
        prevU = cutU; prevLat = latEdge;
        k += dir;
      }
      cur.push([((u % 360) + 360) % 360, lat]);
      prevU = u; prevLat = lat;
    });
    if (cur.length) segs.push(cur);
    return segs;
  }
  // 直线连点：曲线在重合点/折返点会算出跑偏的控制点、绕出圈来，直线不会。
  function routePath(pts) {
    if (pts.length < 2) return '';
    return pts.map((pt, i) => (i === 0 ? 'M' : 'L') + pt[0].toFixed(1) + ',' + pt[1].toFixed(1)).join('');
  }

  function routeTCls(ms) { return ms < 100 ? 'tg' : ms <= 200 ? 'to' : 'tr'; }

  function renderRouteMap(card, data) {
    const hops = data.hops || [];
    if (hops.length < 2) {
      card.innerHTML = `<div class="section-head"><h2>回程路由</h2></div><div class="sub">未探测到路由数据</div>`;
      return;
    }
    const total = hops[hops.length - 1].ms || 0;
    const head = `<div class="section-head"><h2>回程路由</h2></div><div class="sub">从服务器到你的位置 · 共 <b>${hops.length - 1} 跳</b> · 往返 <b>${total.toFixed(0)} ms</b></div>`;
    // 地图只画能定位的跳；少于 2 个点就只显示箭头串。
    const mapHops = hops.filter(h => h.lat && h.lon);
    let map = '';
    if (typeof LAND !== 'undefined' && mapHops.length >= 2) {
      let svg = `<clipPath id="route-clip"><rect width="1000" height="500"/></clipPath><path class="route-grat" d="${routeGrat()}"/>${routeLand()}`;
      mapHops.forEach(h => { h.sx = routeSx(h.lon); });
      routeSplit(mapHops).forEach(seg => {
        const pts = seg.map(([sx, lat]) => [routePx(sx), routePy(lat)]);
        const d = routePath(pts);
        svg += `<path class="route-line" d="${d}"/><path class="route-flow" d="${d}"/>`;
      });
      const labeled = [];
      const placedTexts = [];
      let labelCount = 0;
      // 找一个不与已放文字重叠的位置（向下错开），给只标数字的跳用。
      const placeNum = (x, y) => {
        let px = x, py = y, tries = 0;
        while (tries < 8 && placedTexts.some(q => Math.hypot(q.x - px, q.y - py) < 13)) { py += 13; tries++; }
        placedTexts.push({ x: px, y: py });
        return [px, py];
      };
      mapHops.forEach((h, i) => {
        const x = routePx(h.sx), y = routePy(h.lat);
        const c = routeTCls(h.ms), col = c === 'tg' ? '#2fbf7a' : c === 'to' ? '#e69f00' : '#e5484d';
        const isFirst = i === 0, isLast = i === mapHops.length - 1;
        const fill = isFirst ? '#3569ee' : isLast ? '#15223a' : col;
        svg += `<circle class="route-hop" cx="${x.toFixed(1)}" cy="${y.toFixed(1)}" r="${isFirst || isLast ? 8 : 7}" fill="${fill}"><title>${window.esc(h.name)} · ${h.ms.toFixed(1)} ms</title></circle>`;
        const anchor = x > 880 ? 'end' : 'start', tx = x > 880 ? x - 12 : x + 12;
        if (isFirst) {
          const ly = 26, dy = y + ly;
          svg += `<text class="route-lbl" x="${tx.toFixed(1)}" y="${dy.toFixed(1)}" text-anchor="${anchor}">服务器</text>`;
          placedTexts.push({ x: tx, y: dy });
          labeled.push({ x, y, ly });
          labelCount += 1;
          return;
        }
        // 终点始终标名；若与已标点太近，翻到另一侧、改放左边，避开右边的数字。
        if (isLast) {
          let ly = (labelCount % 2 === 0) ? 26 : -16;
          let anchor2 = anchor, tx2 = tx;
          let near = null, nearD = 1e9;
          labeled.forEach(q => { const d = Math.hypot(q.x - x, q.y - y); if (d < 20 && d < nearD) { near = q; nearD = d; } });
          if (near) {
            ly = -near.ly;
            if (x > 150) { anchor2 = 'end'; tx2 = x - 12; }
          }
          const dy = y + ly;
          svg += `<text class="route-lbl" x="${tx2.toFixed(1)}" y="${dy.toFixed(1)}" text-anchor="${anchor2}">${i} · 你的位置</text>`;
          svg += `<text class="route-sub" x="${tx2.toFixed(1)}" y="${(dy + 13).toFixed(1)}" text-anchor="${anchor2}">${h.ms.toFixed(1)} ms</text>`;
          placedTexts.push({ x: tx2, y: dy }, { x: tx2, y: dy + 13 });
          labeled.push({ x, y, ly });
          labelCount += 1;
          return;
        }
        // 中间跳：挤在一起的只标数字（向下错开，不丢）；不挤的标全名。
        if (labeled.some(q => Math.hypot(q.x - x, q.y - y) < 20)) {
          const [nx, ny] = placeNum(x + 10, y - 10);
          svg += `<text class="route-num" x="${nx.toFixed(1)}" y="${ny.toFixed(1)}" text-anchor="start">${i}</text>`;
          return;
        }
        const ly = (labelCount % 2 === 0) ? 26 : -16, dy = y + ly;
        svg += `<text class="route-lbl" x="${tx.toFixed(1)}" y="${dy.toFixed(1)}" text-anchor="${anchor}">${i} · ${window.esc(h.name)}</text>`;
        svg += `<text class="route-sub" x="${tx.toFixed(1)}" y="${(dy + 13).toFixed(1)}" text-anchor="${anchor}">${h.ms.toFixed(1)} ms</text>`;
        placedTexts.push({ x: tx, y: dy }, { x: tx, y: dy + 13 });
        labeled.push({ x, y, ly });
        labelCount += 1;
      });
      map = `<div class="mapwrap"><svg viewBox="0 0 1000 500">${svg}</svg></div>`;
    }
    const chain = hops.map((hop, i) => {
      const name = `<b>${window.esc(hop.name)}</b>`;
      const ms = i === 0 ? '' : ` <span class="ms ${routeTCls(hop.ms)}">${hop.ms.toFixed(1)} ms</span>`;
      return `<span>${name}${ms}</span>`;
    }).join('<span class="arr">→</span>');
    card.innerHTML = head + map
      + `<div class="pathline">${chain}</div>`
      + `<div class="foot2"><div class="legend"><span><i class="g"></i>&lt;100ms</span><span><i class="o"></i>100–200ms</span><span><i class="r"></i>&gt;200ms</span></div></div>`;
  }

  async function loadRouteMap() {
    if (routeLoaded) return;
    const host = ensureRouteHost();
    if (!host) return;
    routeLoaded = true;
    let card = host.querySelector('.route-map-card');
    if (!card) {
      card = document.createElement('section');
      card.className = 'card section route-map-card';
      host.append(card);
    }
    card.innerHTML = `<div class="section-head"><h2>回程路由</h2></div><div class="sub">正在探测路由…</div>`;
    try {
      const response = await fetch('/api/route', { credentials: 'same-origin', cache: 'no-store' });
      const data = await response.json();
      if (!response.ok || data.error) throw new Error(data.error || ('探测失败：' + response.status));
      renderRouteMap(card, data);
    } catch (error) {
      card.innerHTML = `<div class="section-head"><h2>回程路由</h2></div><div class="sub">${window.esc(error.message)}</div>`;
    }
  }

  window.viewDashboard = function overviewDashboard() {
    originalDashboard();
    document.querySelectorAll('[aria-label="返回仪表盘"]').forEach(element => element.setAttribute('aria-label', '返回概览'));
    document.querySelector('.dashboard-grid')?.querySelector('.address-strip')?.remove();
    const host = ensureNetworkHost();
    if (!host) return;
    renderNetwork(host);
    loadNetwork();
    loadRouteMap();
  };

  function renderSettingsTargets() {
    const form = document.querySelector('.settings-form');
    if (!form || document.querySelector('.ping-settings-card')) return;
    const card = document.createElement('section');
    card.className = 'card section ping-settings-card';
    card.style.marginTop = '18px';
    card.innerHTML = `<div class="section-head"><div><h2>回程网络监测</h2></div></div><form class="fields" id="ping-target-form"><label>名称<input name="name" required maxlength="40" placeholder="例如：电信"></label><label>IP 或域名<input name="address" required maxlength="253" placeholder="例如：1.1.1.1 或 example.com"></label><div class="full"><button class="primary" type="submit">添加监测目标</button></div></form><div class="nq-target-list" id="ping-target-list"></div>`;
    (form.closest('.list') || form.closest('.card')?.parentElement)?.append(card);
    card.querySelector('#ping-target-form').onsubmit = async event => {
      event.preventDefault();
      const data = Object.fromEntries(new FormData(event.target));
      try {
        const response = await fetch('/api/network-quality/targets', { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify(data) });
        const body = await response.json();
        if (!response.ok) throw Error(body.error || '添加失败');
        event.target.reset(); toast('监测目标已添加'); loadTargetList(card);
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
      const targets = data.targets || [];
      list.innerHTML = targets.map(target => `<div class="nq-target-row"><div><b>${window.esc(target.name)}</b><small>${window.esc(target.address)}</small></div><button data-delete-ping="${target.id}">删除</button></div>`).join('') || '<div class="nq-empty">尚未添加监测目标</div>';
      list.querySelectorAll('[data-delete-ping]').forEach(button => button.onclick = async () => {
        if (!confirm('确定删除这个监测目标及其历史数据吗？')) return;
        await fetch('/api/network-quality/targets/' + button.dataset.deletePing, { method: 'DELETE' });
        loadTargetList(card);
      });
    } catch {}
  }

  window.viewSettings = function () { originalSettings(); renderSettingsTargets(); };
  // index.html already starts the initial load(). Calling go() here caused a
  // second complete dashboard render and made the network card arrive late.
  // Start the data request early; viewDashboard will mount the result when the
  // initial dashboard render completes.
  loadNetwork();
  loadRouteMap();
  setInterval(() => {
    if (document.querySelector('#view .dashboard-grid')) loadNetwork();
  }, networkRefreshInterval);
})();
