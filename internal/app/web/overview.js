/* The network-quality view intentionally reuses KotaUI's existing cards, colors, spacing and responsive layout. */
(() => {
  const originalNav = window.nav;
  const originalDashboard = window.viewDashboard;
  const originalSettings = window.viewSettings;
  let pingTimer = 0;
  let pingMode = 'latency';
  let pingData = { targets: [], samples: [] };

  const style = document.createElement('style');
  style.textContent = `.network-quality-card{margin-top:18px}.network-quality-head{display:block}.network-quality-targets{display:grid;gap:1px;margin-top:14px;border:1px solid var(--line);border-radius:12px;overflow:hidden;background:var(--line)}.network-quality-table-head{display:grid;grid-template-columns:minmax(0,1.5fr) repeat(3,minmax(64px,1fr));gap:8px;padding:6px 12px;background:#f1f4fa;color:var(--muted);font-size:11px}.network-quality-target{display:grid;grid-template-columns:minmax(0,1.5fr) repeat(3,minmax(64px,1fr));align-items:center;gap:8px;min-height:42px;padding:8px 12px;background:#fbfcfe;border-left:3px solid var(--blue)}.network-quality-target b{display:block;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-size:14px;line-height:1.35}.network-quality-target .nq-metrics{display:contents;color:var(--muted);font-size:12px;line-height:1.35;font-variant-numeric:tabular-nums;white-space:nowrap}.network-quality-target .nq-metrics strong{color:var(--ink);font-size:14px}.nq-tabs{display:flex;gap:7px;margin:12px 0 9px}.nq-tabs button{padding:7px 11px;border-radius:10px;background:#f1f4fa;color:#5b6b84}.nq-tabs button.on{background:#eaf1ff;color:var(--blue);font-weight:700}.nq-chart{width:100%;height:205px;display:block;border-radius:14px;background:#fbfcfe;border:1px solid var(--line);text-rendering:geometricPrecision}.nq-legend{display:flex;flex-wrap:wrap;gap:6px 14px;margin-top:9px}.nq-legend .badge{margin:0}.nq-empty{padding:24px;text-align:center;color:var(--muted)}.nq-target-list{display:grid;gap:8px;margin-top:12px}.nq-target-row{display:grid;grid-template-columns:1fr auto;align-items:center;gap:10px;padding:10px 12px;border-radius:12px;background:#f7f9fd}.nq-target-row small{display:block;color:var(--muted);margin-top:2px}.nq-target-row button{padding:6px 9px;border-radius:8px;background:#fff0f1;color:var(--danger);font-size:12px}@media(max-width:800px){.network-quality-table-head,.network-quality-target{grid-template-columns:minmax(0,1.35fr) repeat(3,minmax(48px,1fr));gap:5px;padding-left:9px;padding-right:9px}.network-quality-target .nq-metrics{font-size:11px}.network-quality-target .nq-metrics strong{font-size:13px}.network-quality-card{margin-top:12px}.nq-chart{height:190px}}`;
  document.head.append(style);

  window.nav = function overviewNav() {
    const markup = originalNav();
    return markup.replace('仪表盘</button>', '概览</button>');
  };

  function stat(sample) {
    if (!sample) return { main: '--', loss: '--', jitter: '--' };
    if (pingMode === 'loss') return { main: `${sample.loss.toFixed(2)}%`, loss: '', jitter: '' };
    if (pingMode === 'jitter') return { main: `${sample.jitterMs.toFixed(2)} ms`, loss: '', jitter: '' };
    return { main: `${sample.avgMs.toFixed(1)} ms`, loss: `${sample.loss.toFixed(2)}% 丢包`, jitter: `${sample.jitterMs.toFixed(2)} ms 抖动` };
  }

  function latestFor(id) {
    return pingData.samples.filter(x => x.targetId === id).sort((a,b) => new Date(b.checkedAt)-new Date(a.checkedAt))[0];
  }

  function chart() {
    const width = 900, height = 250, left = 48, right = 14, top = 16, bottom = 32;
    const innerW = width-left-right, innerH = height-top-bottom;
    const samples = pingData.samples;
    const values = samples.flatMap(s => pingMode === 'loss' ? [s.loss] : pingMode === 'jitter' ? [s.jitterMs] : [s.avgMs]).filter(Number.isFinite);
    const max = Math.max(pingMode === 'loss' ? 1 : 10, ...(values.length ? values : [100]));
    const yMax = pingMode === 'loss' ? Math.max(1, Math.ceil(max / 5) * 5) : Math.ceil(max / 20) * 20;
    const now = Date.now(), start = now - 24*60*60*1000;
    const x = t => left + Math.max(0, Math.min(1, (new Date(t).getTime()-start)/(now-start))) * innerW;
    const y = v => top + innerH - (Math.max(0, v)/yMax)*innerH;
    let svg = `<svg class="nq-chart" viewBox="0 0 ${width} ${height}" preserveAspectRatio="xMidYMid meet" role="img" aria-label="24小时网络质量趋势图"><style>text{font-family:Arial,"Noto Sans CJK SC","Microsoft YaHei",sans-serif;font-weight:400;letter-spacing:0;text-rendering:geometricPrecision}</style>`;
    for(let i=0;i<=4;i++){const yy=top+innerH*i/4;const value=(yMax*(4-i)/4).toFixed(0);svg+=`<line x1="${left}" x2="${width-right}" y1="${yy}" y2="${yy}" stroke="#e4ebf4" stroke-dasharray="4 4"/><text x="${left-8}" y="${yy+4}" text-anchor="end" fill="#78869a" font-size="12">${value}</text>`;}
    const labels = [0,.5,1]; labels.forEach(p=>{const xx=left+innerW*p;const d=new Date(start+(now-start)*p);svg+=`<text x="${xx}" y="${height-10}" text-anchor="${p===0?'start':p===1?'end':'middle'}" fill="#78869a" font-size="12">${d.getMonth()+1}/${d.getDate()} ${String(d.getHours()).padStart(2,'0')}:${String(d.getMinutes()).padStart(2,'0')}</text>`;});
    const colors=['#3671ef','#12af7f','#7a61e8','#d19524','#de5b65','#4aa8c4'];
    pingData.targets.forEach((target,index)=>{const points=samples.filter(s=>s.targetId===target.id).sort((a,b)=>new Date(a.checkedAt)-new Date(b.checkedAt)).filter(s=>new Date(s.checkedAt)>=new Date(start));let path='',pen=false;points.forEach(s=>{const v=pingMode==='loss'?s.loss:pingMode==='jitter'?s.jitterMs:s.avgMs;if(!s.received||!Number.isFinite(v)){pen=false;return;}const point=`${x(s.checkedAt)},${y(v)}`;path+=(pen?' L':' M')+point;pen=true;});if(path)svg+=`<path d="${path}" fill="none" stroke="${colors[index%colors.length]}" stroke-width="2"/>`;});
    svg+='</svg>';return svg;
  }

  function renderNetwork(dashboard) {
    let card = dashboard.querySelector('.network-quality-card');
    if (!card) { card=document.createElement('section'); card.className='card section network-quality-card'; dashboard.append(card); }
    const colors=['#3671ef','#12af7f','#7a61e8','#d19524','#de5b65','#4aa8c4'];
    const cards=pingData.targets.map((target,index)=>{const latest=latestFor(target.id),s=stat(latest);const loss=latest?`${latest.loss.toFixed(2)}%`:'--';const jitter=latest?`${latest.jitterMs.toFixed(2)} ms`:'--';return `<div class="network-quality-target" style="border-left-color:${colors[index%colors.length]}"><b title="${window.esc(target.name)}">${window.esc(target.name)}</b><div class="nq-metrics"><strong>${s.main}</strong><span>${loss}</span><span>${jitter}</span></div></div>`}).join('');
    const legend=pingData.targets.map((t,i)=>`<span class="badge"><i style="background:${colors[i%colors.length]}"></i>${window.esc(t.name)}</span>`).join('');
    card.innerHTML=`<div class="network-quality-head"><h2>网络质量</h2><p class="sub">最近 24 小时状态 · 每分钟检测</p></div>${cards?`<div class="network-quality-targets"><div class="network-quality-table-head"><span>目标</span><span>平均延时</span><span>丢包</span><span>抖动</span></div>${cards}</div>`:'<div class="nq-empty">请在设置中添加 IP 或域名监测目标</div>'}<div class="nq-tabs"><button class="${pingMode==='latency'?'on':''}" data-nq-mode="latency">延迟</button><button class="${pingMode==='loss'?'on':''}" data-nq-mode="loss">丢包</button><button class="${pingMode==='jitter'?'on':''}" data-nq-mode="jitter">抖动</button></div>${pingData.targets.length?chart():''}<div class="nq-legend">${legend}</div>`;
    card.querySelectorAll('[data-nq-mode]').forEach(button=>button.onclick=()=>{pingMode=button.dataset.nqMode;renderNetwork(dashboard)});
  }

  async function loadNetwork() {
    try { const response=await fetch('/api/network-quality',{credentials:'same-origin'}); if(!response.ok)return; pingData=await response.json(); const dashboard=document.querySelector('.dashboard-grid'); if(dashboard)renderNetwork(dashboard); } catch {}
  }

  window.viewDashboard = function overviewDashboard() {
    originalDashboard();
    document.querySelectorAll('[aria-label="返回仪表盘"]').forEach((element) => element.setAttribute('aria-label', '返回概览'));
    const dashboard = document.querySelector('.dashboard-grid');
    dashboard?.querySelector('.address-strip')?.remove();
    loadNetwork();
    clearInterval(pingTimer); pingTimer=setInterval(loadNetwork,60000);
  };

  function renderSettingsTargets() {
    const form=document.querySelector('.settings-form'); if(!form||document.querySelector('.ping-settings-card'))return;
    const card=document.createElement('section'); card.className='card section ping-settings-card'; card.style.marginTop='18px';
    card.innerHTML=`<div class="section-head"><div><h2>网络质量监测</h2><p class="sub">每分钟从 VPS 向目标发送 3 次 Ping，保留最近 24 小时数据。</p></div></div><form class="fields" id="ping-target-form"><label>名称<input name="name" required maxlength="40" placeholder="例如：电信"></label><label>IP 或域名<input name="address" required maxlength="253" placeholder="例如：1.1.1.1 或 example.com"></label><div class="full"><button class="primary" type="submit">添加监测目标</button></div></form><div class="nq-target-list" id="ping-target-list"></div>`;
    form.closest('.card')?.insertAdjacentElement('afterend',card);
    card.querySelector('#ping-target-form').onsubmit=async e=>{e.preventDefault();const data=Object.fromEntries(new FormData(e.target));try{const r=await fetch('/api/network-quality/targets',{method:'POST',headers:{'content-type':'application/json'},body:JSON.stringify(data)});const body=await r.json();if(!r.ok)throw Error(body.error||'添加失败');e.target.reset();toast('监测目标已添加');renderSettingsTargets();}catch(err){toast(err.message)}};
    loadTargetList(card);
  }
  async function loadTargetList(card){try{const r=await fetch('/api/network-quality',{credentials:'same-origin'});if(!r.ok)return;const d=await r.json();const list=card.querySelector('#ping-target-list');list.innerHTML=d.targets.map(t=>`<div class="nq-target-row"><div><b>${window.esc(t.name)}</b><small>${window.esc(t.address)}</small></div><button data-delete-ping="${t.id}">删除</button></div>`).join('')||'<div class="nq-empty">尚未添加监测目标</div>';list.querySelectorAll('[data-delete-ping]').forEach(btn=>btn.onclick=async()=>{if(!confirm('确定删除这个监测目标及其历史数据吗？'))return;await fetch('/api/network-quality/targets/'+btn.dataset.deletePing,{method:'DELETE'});renderSettingsTargets();});}catch{}}
  window.viewSettings=function(){originalSettings();renderSettingsTargets();};
  window.go('dashboard');
})();
