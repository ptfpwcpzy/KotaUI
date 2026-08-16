import test from 'node:test';
import assert from 'node:assert/strict';
import { certificateKind, certbotArgs, renewalCommand } from '../server/lib/certs.mjs';

test('domain and IP certificate policies', () => {
  const domain = certbotArgs({ subject: 'example.com', email: 'admin@example.com' });
  assert.equal(domain.isIp, false);
  assert.ok(domain.args.includes('--standalone'));
  assert.ok(!domain.args.includes('shortlived'));
  const ip = certbotArgs({ subject: '198.51.100.10' });
  assert.equal(ip.isIp, true);
  assert.deepEqual(ip.args.slice(-2), ['--cert-profile', 'shortlived']);
  assert.ok(ip.args.includes('--cert-profile'));
  assert.ok(ip.args.includes('shortlived'));
  assert.match(certificateKind('198.51.100.10').warning, /短周期/);
});

test('renewal command supports deploy hook', () => {
  const command = renewalCommand('certbot', '/usr/local/bin/kota-cert-renew-hook');
  assert.deepEqual(command.args, ['renew', '--quiet', '--deploy-hook', '/usr/local/bin/kota-cert-renew-hook']);
});
