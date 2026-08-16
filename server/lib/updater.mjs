import crypto from 'node:crypto';
import fs from 'node:fs';
import path from 'node:path';
import { pipeline } from 'node:stream/promises';
import { Readable } from 'node:stream';

export function sha256File(file) { const hash = crypto.createHash('sha256'); hash.update(fs.readFileSync(file)); return hash.digest('hex'); }
export function verifySha256(file, expected) { return !expected || sha256File(file).toLowerCase() === String(expected).toLowerCase(); }
export async function downloadArtifact(url, destination, allowedHosts = []) { const parsed = new URL(url); if (allowedHosts.length && !allowedHosts.includes(parsed.hostname)) throw new Error(`不允许的更新源: ${parsed.hostname}`); const response = await fetch(parsed); if (!response.ok || !response.body) throw new Error(`下载失败: HTTP ${response.status}`); fs.mkdirSync(path.dirname(destination), { recursive: true, mode: 0o700 }); const tmp = `${destination}.download-${process.pid}`; await pipeline(Readable.fromWeb(response.body), fs.createWriteStream(tmp, { mode: 0o700 })); fs.renameSync(tmp, destination); return destination; }
export function atomicReplace(source, destination, expectedSha256) { if (!verifySha256(source, expectedSha256)) throw new Error(`更新文件校验失败: ${path.basename(source)}`); fs.mkdirSync(path.dirname(destination), { recursive: true, mode: 0o700 }); const tmp = `${destination}.new-${process.pid}`; fs.copyFileSync(source, tmp); fs.chmodSync(tmp, 0o700); fs.renameSync(tmp, destination); return destination; }
export function backupFile(file, backupDir) { if (!fs.existsSync(file)) return null; fs.mkdirSync(backupDir, { recursive: true, mode: 0o700 }); const target = path.join(backupDir, `${path.basename(file)}.${Date.now()}.bak`); fs.copyFileSync(file, target); fs.chmodSync(target, 0o600); return target; }
