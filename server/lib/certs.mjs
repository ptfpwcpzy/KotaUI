import net from 'node:net';
import { spawnSync } from 'node:child_process';

export function certificateKind(subject) {
  const value = String(subject || '').trim();
  return { subject: value, isIp: net.isIP(value) !== 0, warning: net.isIP(value) !== 0 ? 'IP 证书必须使用短周期 profile，有效期可能只有数天，需要高频续期。' : '' };
}

export function certbotArgs({ subject, email = '', webroot = '', staging = false } = {}) {
  const kind = certificateKind(subject);
  if (!kind.subject) throw new Error('证书主体不能为空');
  const args = ['certonly', '--non-interactive', '--agree-tos', '--keep-until-expiring', '-d', kind.subject];
  if (email) args.push('--email', email); else args.push('--register-unsafely-without-email');
  if (webroot) args.push('--webroot', '--webroot-path', webroot); else args.push('--standalone');
  if (kind.isIp) args.push('--cert-profile', 'shortlived');
  if (staging) args.push('--staging');
  return { args, ...kind };
}

export function runCertbot(options, binary = 'certbot') {
  const command = certbotArgs(options);
  const result = spawnSync(binary, command.args, { encoding: 'utf8', timeout: 180000 });
  if (result.error) return { ok: false, command, error: result.error.message, stdout: result.stdout || '', stderr: result.stderr || '' };
  return { ok: result.status === 0, command, stdout: result.stdout || '', stderr: result.stderr || '' };
}

export function renewalCommand(binary = 'certbot', deployHook = '') {
  const args = ['renew', '--quiet'];
  if (deployHook) args.push('--deploy-hook', deployHook);
  return { binary, args };
}
