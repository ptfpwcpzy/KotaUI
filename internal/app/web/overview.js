/* Design reminder: the sidebar calls the homepage “概览”; “仪表盘” remains the compact resource-gauge card inside it. */
(() => {
  const originalNav = window.nav;
  const originalDashboard = window.viewDashboard;

  window.nav = function overviewNav() {
    const markup = originalNav();
    return markup.replace('仪表盘</button>', '概览</button>');
  };

  function addressRow(label, values) {
    const value = Array.isArray(values) && values.length ? values[0] : '';
    if (!value) {
      return `<div class="address-row empty"><span>${label}</span><button type="button" disabled>${label === 'IPv6' ? '未检测到公网 IPv6' : '未检测到公网 IPv4'}</button></div>`;
    }
    return `<div class="address-row"><span>${label}</span><button type="button" title="点击复制 ${window.esc(value)}" onclick="copyNetworkAddress('${window.esc(value)}')">${window.esc(value)}</button></div>`;
  }

  window.copyNetworkAddress = async function copyNetworkAddress(value) {
    try {
      await navigator.clipboard.writeText(value);
      window.toast(`${value} 已复制`);
    } catch {
      window.toast('复制失败，请长按地址后手动复制');
    }
  };

  window.viewDashboard = function overviewDashboard() {
    originalDashboard();
    document.querySelectorAll('[aria-label="返回仪表盘"]').forEach((element) => element.setAttribute('aria-label', '返回概览'));
    const dashboard = document.querySelector('.dashboard-grid');
    const online = dashboard?.querySelector('.online-strip');
    if (!dashboard || !online || dashboard.querySelector('.address-strip')) return;
    const network = window.dash?.network || {};
    const strip = document.createElement('section');
    strip.className = 'card dashboard-strip address-strip';
    strip.innerHTML = `<div class="address-title"><b>VPS 网络地址</b><span>点击复制</span></div>${addressRow('IPv4', network.ipv4)}${addressRow('IPv6', network.ipv6)}`;
    online.insertAdjacentElement('afterend', strip);
  };

  window.go('dashboard');
})();
