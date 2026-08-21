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
		return `<div class="address-row empty"><span>${label}</span><b>${label === 'IPv6' ? '未检测到公网 IPv6' : '未检测到公网 IPv4'}</b></div>`;
    }
	return `<div class="address-row"><span>${label}</span><b>${window.esc(value)}</b></div>`;
  }

  window.viewDashboard = function overviewDashboard() {
    originalDashboard();
    document.querySelectorAll('[aria-label="返回仪表盘"]').forEach((element) => element.setAttribute('aria-label', '返回概览'));
    const dashboard = document.querySelector('.dashboard-grid');
    const online = dashboard?.querySelector('.online-strip');
    if (!dashboard || !online || dashboard.querySelector('.address-strip')) return;
    const network = window.dash?.network || {};
    const strip = document.createElement('section');
    strip.className = 'card dashboard-strip address-strip';
	strip.innerHTML = `<div class="address-title"><b>VPS 网络地址</b></div>${addressRow('IPv4', network.ipv4)}${addressRow('IPv6', network.ipv6)}`;
    online.insertAdjacentElement('afterend', strip);
  };

  window.go('dashboard');
})();
