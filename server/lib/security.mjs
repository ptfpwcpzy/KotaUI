import crypto from 'node:crypto';

export function hashPassword(password, salt = crypto.randomBytes(16).toString('hex')) {
  const hash = crypto.scryptSync(String(password), salt, 64).toString('hex');
  return { salt, hash };
}

export function verifyPassword(password, record) {
  if (!record?.salt || !record?.hash) return false;
  const actual = crypto.scryptSync(String(password), record.salt, 64).toString('hex');
  return crypto.timingSafeEqual(Buffer.from(actual, 'hex'), Buffer.from(record.hash, 'hex'));
}

export function token() { return crypto.randomBytes(32).toString('hex'); }
