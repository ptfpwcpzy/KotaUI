function monthKey(date = new Date(), timeZone = 'UTC') { return new Intl.DateTimeFormat('en-CA', { timeZone, year: 'numeric', month: '2-digit' }).format(date); }

export function ensureMonthlyReset(client, timeZone = 'UTC', now = new Date()) {
  const key = monthKey(now, timeZone);
  if (client.monthKey !== key) { client.monthKey = key; client.monthlyUsedBytes = 0; if (client.monthlyLimitBytes > 0 && client.enabled !== false) client.enabled = true; }
  return client;
}

export function applyTraffic(client, uploadBytes, downloadBytes, timeZone = 'UTC', now = new Date()) {
  ensureMonthlyReset(client, timeZone, now);
  const delta = Math.max(0, Number(uploadBytes || 0)) + Math.max(0, Number(downloadBytes || 0));
  client.usedBytes = Number(client.usedBytes || 0) + delta;
  client.monthlyUsedBytes = Number(client.monthlyUsedBytes || 0) + delta;
  if ((client.totalLimitBytes > 0 && client.usedBytes >= client.totalLimitBytes) || (client.monthlyLimitBytes > 0 && client.monthlyUsedBytes >= client.monthlyLimitBytes)) client.enabled = false;
  return client;
}

export function activeIpDecision(client, activeIps, now = Date.now()) {
  const windowMs = 30_000;
  const current = new Map(Object.entries(activeIps || {}).filter(([, seen]) => now - Number(seen) <= windowMs));
  const limit = Number(client.maxOnlineIps || 0);
  return { allowed: limit <= 0 || current.size <= limit, activeIps: [...current.keys()], windowMs };
}

export function markActiveIp(activeIps, ip, now = Date.now()) { return { ...(activeIps || {}), [String(ip)]: now }; }
