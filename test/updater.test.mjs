import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import crypto from 'node:crypto';
import { atomicReplace, sha256File, verifySha256 } from '../server/lib/updater.mjs';

test('updater verifies hash and atomically replaces files', () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'kota-update-'));
  const source = path.join(dir, 'source.bin'); const destination = path.join(dir, 'target.bin');
  fs.writeFileSync(source, 'new-content'); fs.writeFileSync(destination, 'old-content');
  const digest = sha256File(source); assert.equal(verifySha256(source, digest), true); assert.equal(verifySha256(source, 'bad'), false);
  atomicReplace(source, destination, digest); assert.equal(fs.readFileSync(destination, 'utf8'), 'new-content');
});
