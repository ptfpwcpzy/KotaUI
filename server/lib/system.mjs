import fs from 'node:fs';
import os from 'node:os';

let previousCpu = null;
let previousNetwork = null;

function readFile(path, fallback = '') {
  try { return fs.readFileSync(path, 'utf8'); } catch { return fallback; }
}

function number(value) { return Number(value || 0); }

function parseMemInfo() {
  const values = Object.fromEntries(readFile('/proc/meminfo').split(/\r?\n/).map(line => {
    const match = line.match(/^(\w+):\s+(\d+)/);
    return match ? [match[1], number(match[2]) * 1024] : [];
  }).filter(item => item.length));
  const total = values.MemTotal || os.totalmem();
  const available = values.MemAvailable || Math.max(0, total - (values.MemFree || 0) - (values.Buffers || 0) - (values.Cached || 0));
  return { totalBytes: total, usedBytes: Math.max(0, total - available), availableBytes: available };
}

function cpuSnapshot() {
  const aggregate = readFile('/proc/stat').split(/\r?\n/)[0]?.trim().split(/\s+/).slice(1).map(number) || [];
  const total = aggregate.reduce((sum, value) => sum + value, 0);
  const idle = number(aggregate[3]) + number(aggregate[4]);
  const previous = previousCpu;
  previousCpu = { total, idle };
  if (!previous || total <= previous.total) return { usagePercent: 0, cores: os.cpus().length || 1 };
  const totalDelta = total - previous.total;
  const idleDelta = idle - previous.idle;
  return { usagePercent: Math.max(0, Math.min(100, ((totalDelta - idleDelta) / totalDelta) * 100)), cores: os.cpus().length || 1 };
}

function diskSnapshot() {
  try {
    const stat = fs.statfsSync('/');
    const totalBytes = Number(stat.blocks) * Number(stat.bsize);
    const freeBytes = Number(stat.bavail) * Number(stat.bsize);
    return { totalBytes, usedBytes: Math.max(0, totalBytes - freeBytes), freeBytes };
  } catch {
    return { totalBytes: 0, usedBytes: 0, freeBytes: 0 };
  }
}

function networkSnapshot() {
  const interfaces = {};
  for (const line of readFile('/proc/net/dev').split(/\r?\n/).slice(2)) {
    const match = line.match(/^\s*([^:]+):\s*(.+)$/);
    if (!match) continue;
    const name = match[1].trim();
    if (name === 'lo') continue;
    const values = match[2].trim().split(/\s+/).map(number);
    interfaces[name] = { rxBytes: values[0] || 0, txBytes: values[8] || 0 };
  }
  const totals = Object.values(interfaces).reduce((sum, item) => ({ rxBytes: sum.rxBytes + item.rxBytes, txBytes: sum.txBytes + item.txBytes }), { rxBytes: 0, txBytes: 0 });
  const now = Date.now();
  const previous = previousNetwork;
  previousNetwork = { ...totals, at: now };
  const seconds = previous ? Math.max(0.25, (now - previous.at) / 1000) : 1;
  return {
    ...totals,
    downloadRateBytes: previous ? Math.max(0, (totals.rxBytes - previous.rxBytes) / seconds) : 0,
    uploadRateBytes: previous ? Math.max(0, (totals.txBytes - previous.txBytes) / seconds) : 0,
    interfaces: Object.keys(interfaces),
  };
}

function primaryIp() {
  for (const entries of Object.values(os.networkInterfaces())) {
    for (const entry of entries || []) {
      if (entry.family === 'IPv4' && !entry.internal) return entry.address;
    }
  }
  return '';
}

export function collectSystemMetrics() {
  const uptime = number(readFile('/proc/uptime').split(/\s+/)[0]) || os.uptime();
  return {
    at: new Date().toISOString(),
    cpu: cpuSnapshot(),
    memory: parseMemInfo(),
    disk: diskSnapshot(),
    network: networkSnapshot(),
    load: os.loadavg().map(value => Number(value.toFixed(2))),
    uptimeSeconds: Math.floor(uptime),
    ip: primaryIp(),
    platform: os.platform(),
    release: os.release(),
  };
}

export function formatBytes(value) {
  const bytes = Math.max(0, Number(value || 0));
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
  let index = 0;
  let result = bytes;
  while (result >= 1024 && index < units.length - 1) { result /= 1024; index += 1; }
  return `${result >= 100 || index === 0 ? result.toFixed(0) : result.toFixed(1)} ${units[index]}`;
}

export function formatDuration(seconds) {
  const total = Math.max(0, Math.floor(Number(seconds || 0)));
  const days = Math.floor(total / 86400);
  const hours = Math.floor((total % 86400) / 3600);
  const minutes = Math.floor((total % 3600) / 60);
  return days > 0 ? `${days} 天 ${hours} 小时` : `${hours} 小时 ${minutes} 分钟`;
}
