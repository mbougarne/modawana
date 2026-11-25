import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { resolve } from 'node:path';
import { spawn, spawnSync } from 'node:child_process';
import assert from 'node:assert/strict';
import { verifyPackage } from './verify-package.mjs';

const input = resolve(process.argv[2] || 'build/releases/acceptance.tar.gz');
const previous = process.argv[3] && resolve(process.argv[3]);
const work = await mkdtemp(resolve(tmpdir(), 'blog-package-'));
let server;
try {
  const { source } = await verifyPackage(input, work);
  const env = {
    ...process.env,
    BLOG_ENV: 'production',
    BLOG_BASE_URL: 'https://second-owner.test',
    BLOG_RP_ID: 'second-owner.test',
    BLOG_WEBAUTHN_ORIGINS: 'https://second-owner.test',
    BLOG_DATA_ROOT: resolve(work, 'data'),
    BLOG_FRONTEND_DIST: resolve(source, 'frontend'),
    BLOG_LISTEN_ADDR: '127.0.0.1:18089',
    BLOG_TRUSTED_PROXIES: '',
  };
  const binary = resolve(source, 'blog');
  async function start(executable) {
    server = spawn(executable, [], { cwd: source, env, stdio: ['ignore', 'ignore', 'inherit'] });
    for (let n = 0; n < 100 && server.exitCode === null; n++) {
      try {
        if ((await fetch('http://127.0.0.1:18089/readyz')).ok) {
          return;
        }
      } catch {
        /* Poll startup. */
      }
      await new Promise((r) => setTimeout(r, 100));
    }
    throw new Error('Packaged server did not become ready');
  }
  async function stop() {
    const stopped = new Promise((r) => server.once('exit', r));
    server.kill('SIGTERM');
    assert.equal(await stopped, 0);
    server = null;
  }
  if (previous) {
    await start(previous);
    const prior = spawnSync(previous, ['bootstrap'], { cwd: source, env, encoding: 'utf8' });
    assert.equal(prior.status, 0);
    // The old binary owns the pre-upgrade snapshot. The new release then
    // opens the same data root under a fresh runtime lease.
    assert.equal(
      spawnSync(previous, ['backup', '--file', resolve(work, 'before.zip')], {
        cwd: source,
        env,
      }).status,
      0,
    );
    await stop();
  }
  await start(binary);
  assert.equal(spawnSync(binary, ['ready'], { cwd: source, env }).status, 0);
  const page = await fetch('http://127.0.0.1:18089/ar');
  assert.equal(page.status, 200);
  assert.match(await page.text(), /dir="rtl"/);
  const grant = spawnSync(binary, ['bootstrap'], { cwd: source, env, encoding: 'utf8' });
  assert.equal(grant.status, 0);
  assert.match(grant.stdout.trim(), /^[A-Za-z0-9_-]{43}$/);
  assert.equal(spawnSync(binary, ['maintenance'], { cwd: source, env }).status, 0);
  assert.equal(
    spawnSync(binary, ['backup', '--file', resolve(work, 'backup.zip')], { cwd: source, env })
      .status,
    0,
  );
  assert.equal(
    spawnSync(binary, ['backup', '--file', resolve(work, 'backup.zip')], { cwd: source, env })
      .status,
    1,
    'do not overwrite backups',
  );
  await stop();
  const restored = { ...env, BLOG_DATA_ROOT: resolve(work, 'restored') };
  assert.equal(
    spawnSync(binary, ['restore', '--file', resolve(work, 'backup.zip')], {
      cwd: source,
      env: restored,
    }).status,
    0,
  );
  assert.equal(spawnSync(binary, ['resume'], { cwd: source, env: restored }).status, 1);
  console.log(
    'Packaged binaries/frontend/notices checksums, fresh second-domain startup, grant, readiness, maintenance and backup/restore commands passed. Browser enrollment is covered separately.',
  );
  if (previous) {
    console.log('Upgrade from the previous binary reused the same data root and passed readiness.');
  }
} finally {
  server?.kill('SIGTERM');
  await rm(work, { recursive: true, force: true });
}
