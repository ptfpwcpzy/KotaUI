/* Design reminder: add TUIC without expanding the user’s mental load. Certificate, TLS, UUID, password and safe transport defaults are automatic. */
(() => {
  const baseProtocolName = window.protocolName;
  const baseViewInbounds = window.viewInbounds;
  window.protocolName = function protocolNameWithTUIC(type) {
    return type === 'tuic' ? 'TUIC' : baseProtocolName(type);
  };

  window.viewInbounds = function viewInboundsWithTUIC() {
    baseViewInbounds();
    document.querySelectorAll('#view').forEach((view) => {
      view.innerHTML = view.innerHTML.replace('请先创建 VLESS + REALITY、Hysteria 2 或 Shadowsocks 2022 入站。', '请先创建 VLESS + REALITY、Hysteria 2、Shadowsocks 2022 或 TUIC 入站。');
    });
  };

  function fields(type, existing) {
    if (type !== 'tuic') return window.protocolFields(type, existing);
    return `<div class="tuic-note"><i>i</i><div><b>TUIC 自动配置</b>复用面板证书和域名；客户端 UUID、密码、TLS、原生 UDP 中继与安全默认项会自动生成。</div></div>`;
  }

  window.inboundModal = function inboundModalWithTUIC(existing, forcedType) {
    const type = forcedType || existing?.type || 'reality';
    const protocols = [['reality', 'VLESS + REALITY'], ['hysteria2', 'Hysteria 2'], ['shadowsocks2022', 'Shadowsocks 2022'], ['tuic', 'TUIC']];
    window.openModal(`<h2>${existing ? '编辑入站' : '新增入站'}</h2><p class="sub">只填写必要参数；密钥、凭据和安全连接参数由系统自动生成。</p><div class="protocols tuic-protocols">${protocols.map(([value, label]) => `<button class="protocol ${type === value ? 'on' : ''}" type="button" onclick="inboundModal(null,'${value}')">${label}</button>`).join('')}</div><form class="fields" id="inboundForm"><label>名称<input name="name" required value="${window.esc(existing?.name || '')}"></label><label>监听端口<input name="port" type="number" required value="${existing?.port || Math.floor(20000 + Math.random() * 35000)}"></label><div class="ipv6-choice"><div><b>使用 IPv6 分享地址</b><small>服务器有公网 IPv6 时，在订阅节点中优先使用 IPv6 地址</small></div><label class="switch" title="使用 IPv6 分享地址"><input type="checkbox" name="useIPv6" ${existing?.useIPv6 ? 'checked' : ''}><span></span></label></div><div id="protocolFields" class="full">${fields(type, existing || {})}</div></form><div class="dialog-actions"><button class="alt" onclick="closeModal()">取消</button><button class="primary" onclick="document.querySelector('#inboundForm').requestSubmit()">${existing ? '保存修改' : '创建入站'}</button></div>`);
    if (type === 'reality') window.renderSNIRows();
    document.querySelector('#inboundForm').onsubmit = async (event) => {
      event.preventDefault();
      try {
        const form = new FormData(event.target);
        const input = Object.fromEntries(form);
        input.type = type;
        input.port = Number(input.port);
        input.handshakePort = Number(input.handshakePort || 443);
        input.useIPv6 = form.get('useIPv6') === 'on';
        input.upMbps = Number(input.upMbps || 0);
        input.downMbps = Number(input.downMbps || 0);
        const created = await window.api(existing ? `/api/inbounds/${existing.id}` : '/api/inbounds', {method: existing ? 'PATCH' : 'POST', body: JSON.stringify(input)});
        window.closeModal();
        await window.load();
        window.toast(existing ? '入站已保存' : type === 'tuic' ? 'TUIC 入站已创建，凭据已自动生成' : type === 'reality' ? '入站已创建，密钥已自动生成' : '入站已创建');
        if (!existing && created?.id) window.go('inbounds');
      } catch (error) {
        window.toast(error.message);
      }
    };
  };
})();
