import { mkdtemp, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { resolve } from 'node:path';
import { spawn, spawnSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import assert from 'node:assert/strict';

const source = resolve(process.argv[2] || 'build/releases/acceptance');
const work = await mkdtemp(resolve(tmpdir(), 'blog-package-'));
let server;
try {
  const manifest = JSON.parse(await readFile(resolve(source, 'release.json'), 'utf8'));
  for (const file of manifest.files) {
    assert.equal(
      createHash('sha256')
        .update(await readFile(resolve(source, file.path)))
        .digest('hex'),
      file.sha256,
    );
  }
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
  server = spawn(binary, [], { cwd: source, env, stdio: ['ignore', 'ignore', 'inherit'] });
  let ready = false;
  for (let n = 0; n < 100; n++) {
    try {
      if ((await fetch('http://127.0.0.1:18089/readyz')).ok) {
        ready = true;
        break;
      }
    } catch {
      /* Poll startup. */
    }
    await new Promise((r) => setTimeout(r, 100));
  }
  assert.ok(ready);
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
  const stopped = new Promise((r) => server.once('exit', r));
  server.kill('SIGTERM');
  await stopped;
  server = null;
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
} finally {
  server?.kill('SIGTERM');
  await rm(work, { recursive: true, force: true });
}
