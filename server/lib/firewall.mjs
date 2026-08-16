import { spawnSync } from 'node:child_process';

function enabled() { return process.env.KOTAUI_MANAGE_FIREWALL === '1'; }
function run(args) { return spawnSync('ufw', args, { encoding: 'utf8' }); }
function rulesFor(port, network = '') {
  const protocols = network === 'tcp' ? ['tcp'] : network === 'udp' ? ['udp'] : ['tcp', 'udp'];
  return protocols.map(protocol => `${port}/${protocol}`);
}
export function firewallAvailable() { return enabled() && Boolean(spawnSync('sh', ['-c', 'command -v ufw'], { encoding: 'utf8' }).stdout?.trim()); }
export function openInboundPort(port, network = '') {
  if (!firewallAvailable()) return { ok: true, managed: false, reason: enabled() ? 'ufw unavailable' : 'disabled' };
  const results = rulesFor(port, network).map(rule => ({ rule, result: run(['allow', rule]) }));
  return { ok: results.every(x => x.result.status === 0), managed: true, rules: results.map(x => x.rule) };
}
export function closeInboundPort(port, network = '') {
  if (!firewallAvailable()) return { ok: true, managed: false, reason: enabled() ? 'ufw unavailable' : 'disabled' };
  const results = rulesFor(port, network).map(rule => ({ rule, result: run(['delete', 'allow', rule]) }));
  return { ok: results.every(x => x.result.status === 0 || x.result.status === 1), managed: true, rules: results.map(x => x.rule) };
}
