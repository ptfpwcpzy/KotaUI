import path from 'node:path';
import { fileURLToPath } from 'node:url';
import grpc from '@grpc/grpc-js';
import protoLoader from '@grpc/proto-loader';

const protoPath = process.env.KOTAUI_STATS_PROTO || path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../../proto/stats.proto');
const packageDefinition = protoLoader.loadSync(protoPath, { keepCase: true, longs: String, defaults: true, oneofs: true });
const loaded = grpc.loadPackageDefinition(packageDefinition);
const StatsService = loaded.v2ray.core.app.stats.command.StatsService;

function call(client, method, request) { return new Promise((resolve, reject) => { client[method](request, (error, response) => error ? reject(error) : resolve(response)); }); }

export async function queryStats(address = '127.0.0.1:9090', pattern = 'user>>>') {
  const client = new StatsService(address, grpc.credentials.createInsecure());
  try { const response = await call(client, 'QueryStats', { pattern, regexp: false, reset: false }); return response.stat || []; }
  finally { client.close(); }
}

export async function collectUserTraffic(address, usernames) {
  const stats = await queryStats(address, 'user>>>');
  const result = {};
  for (const username of usernames) result[username] = { uploadBytes: 0, downloadBytes: 0 };
  for (const item of stats) {
    const name = String(item.name || '');
    const match = name.match(/^user>>>(.+?)>>>traffic>>>(uplink|downlink)$/);
    if (!match || !result[match[1]]) continue;
    if (match[2] === 'uplink') result[match[1]].uploadBytes = Number(item.value || 0);
    else result[match[1]].downloadBytes = Number(item.value || 0);
  }
  return result;
}
