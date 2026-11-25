import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { resolve } from 'node:path';

const root = resolve(import.meta.dirname, '../..');
const version = process.argv[2] || 'acceptance';
const original = resolve(root, 'build/releases', version + '.tar.gz.sha256');
const work = await mkdtemp(resolve(tmpdir(), 'modawana-repack-'));
try {
  execFileSync(process.execPath, ['scripts/release/package.mjs', version, work], {
    cwd: root,
    stdio: 'inherit',
    env: { ...process.env, TZ: 'Pacific/Auckland' },
  });
  assert.equal(
    await readFile(resolve(work, version + '.tar.gz.sha256'), 'utf8'),
    await readFile(original, 'utf8'),
  );
  console.log('Release archive reproduced byte for byte in a separate directory.');
} finally {
  await rm(work, { recursive: true, force: true });
}
