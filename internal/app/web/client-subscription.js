/* Design reminder: client creation stays concise; a checked-by-default random subscription suffix prevents username-only URL guessing. */
(() => {
  function subscriptionID(client) {
    return `${client.username || ''}${client.subscriptionSuffix || ''}`;
  }

  function subscriptionLink(client) {
    return `${window.dash.subscriptionBaseURL || ''}/${encodeURIComponent(subscriptionID(client))}`;
  }

  window.clientModal = function clientModal(existing) {
    const enabled = (window.state.inbounds || []).filter((inbound) => inbound.enabled);
    if (!enabled.length) {
      window.toast('请先创建并启用入站');
      window.go('inbounds');
      return;
    }
    const randomOption = existing ? '' : `<div class="access-toggle"><div><b>随机订阅标识</b><small>默认启用：在订阅地址末尾加入 5 位随机字母，避免仅凭用户名猜测地址。</small></div><label class="switch" title="随机订阅标识"><input type="checkbox" name="randomSubscriptionSuffix" checked><span></span></label></div>`;
    window.openModal(`<h2>${existing ? '编辑客户端' : '新增客户端'}</h2><p class="sub">${existing ? '修改入站、流量、期限与在线限制；订阅标识将保持不变。' : '创建后会立即显示订阅地址与使用信息。'}</p><form id="clientForm" class="fields"><label>用户名<input name="username" required pattern="[A-Za-z0-9_-]{3,32}" value="${window.esc(existing?.username || '')}" placeholder="3–32 位字母、数字、下划线或连字符"></label><label class="full">绑定入站<div class="badges" style="margin-top:7px">${enabled.map((inbound) => `<label class="badge"><input type="checkbox" name="inbound" value="${inbound.id}" ${!existing || existing.inboundIds.includes(inbound.id) ? 'checked' : ''} style="width:auto;margin:0 5px 0 0"> ${window.esc(inbound.name)} · ${window.esc(window.protocolName(inbound.type))}</label>`).join('')}</div></label>${randomOption}<label>总流量上限（GiB，0 为不限）<input name="total" type="number" min="0" value="${existing ? ((existing.totalLimitBytes || 0) / 1073741824) : 0}"></label><label>自然月流量（GiB，0 为不限）<input name="monthly" type="number" min="0" value="${existing ? ((existing.monthlyLimitBytes || 0) / 1073741824) : 0}"></label><label>有效期 <span class="sub">留空不限</span><input name="expiresAt" type="date" value="${window.esc(existing?.expiresAt || '')}"></label><label>同时在线 IP 数 <span class="sub">0 为不限</span><input name="maxOnlineIps" type="number" min="0" value="${existing?.maxOnlineIps || 0}"></label></form><div class="dialog-actions"><button class="alt" onclick="closeModal()">取消</button><button class="primary" onclick="document.querySelector('#clientForm').requestSubmit()">${existing ? '保存修改' : '创建客户端'}</button></div>`);
    document.querySelector('#clientForm').onsubmit = async (event) => {
      event.preventDefault();
      try {
        const form = new FormData(event.target);
        const input = Object.fromEntries(form);
        input.inboundIds = form.getAll('inbound');
        input.totalLimitBytes = Math.round(Number(input.total || 0) * 1073741824);
        input.monthlyLimitBytes = Math.round(Number(input.monthly || 0) * 1073741824);
        input.maxOnlineIps = Number(input.maxOnlineIps || 0);
        if (!existing) input.randomSubscriptionSuffix = form.get('randomSubscriptionSuffix') === 'on';
        const client = await window.api(existing ? `/api/clients/${existing.id}` : '/api/clients', { method: existing ? 'PATCH' : 'POST', body: JSON.stringify(input) });
        window.closeModal();
        await window.load();
        window.clientDetail(existing?.id || client.id);
        window.toast(existing ? '客户端已保存' : '客户端已创建');
      } catch (error) {
        window.toast(error.message);
      }
    };
  };

  window.clientDetail = function clientDetail(id) {
    const client = (window.state.clients || []).find((item) => item.id === id);
    if (!client) return;
    const link = subscriptionLink(client);
    window.openModal(`<h2>${window.esc(client.username)}</h2><p class="sub">客户端详情</p><div class="detail-grid"><div><span>绑定入站</span><b>${client.inboundIds.length} 个</b></div><div><span>状态</span><b class="${client.paused ? 'red' : 'green'}">${client.paused ? '已暂停' : '正常'}</b></div><div><span>累计流量</span><b>${window.bytes(client.usedBytes)} / ${client.totalLimitBytes ? window.bytes(client.totalLimitBytes) : '不限'}</b></div><div><span>累计上传 / 下载</span><b>↑ ${window.bytes(client.uploadBytes)} · ↓ ${window.bytes(client.downloadBytes)}</b></div><div><span>本月流量</span><b>${window.bytes(client.monthlyUsedBytes)} / ${client.monthlyLimitBytes ? window.bytes(client.monthlyLimitBytes) : '不限'}</b></div><div><span>有效期</span><b>${window.esc(client.expiresAt || '不限')}</b></div><div><span>在线 IP 限制</span><b>${client.maxOnlineIps || '不限'}</b></div></div><div class="subscription" id="subLink">${window.esc(link)}</div><div class="item-actions" style="margin-top:14px"><button class="alt" onclick="copySub()">复制订阅</button><button class="alt" onclick="openSubscription('${window.esc(client.username)}')">查看订阅</button><button class="alt" onclick="clientAction('${client.id}','${client.paused ? 'resume' : 'pause'}')">${client.paused ? '恢复' : '暂停'}</button><button class="alt" onclick="clientAction('${client.id}','reset')">重置流量</button><button class="danger" onclick="clientAction('${client.id}','delete')">删除</button></div><div class="dialog-actions"><button class="alt" onclick="closeModal()">关闭</button></div>`);
  };

  window.openSubscription = function openSubscription(username) {
    const client = (window.state.clients || []).find((item) => item.username === username);
    if (!client) {
      window.toast('未找到客户端订阅地址');
      return;
    }
    window.open(subscriptionLink(client), '_blank', 'noopener');
  };
})();
