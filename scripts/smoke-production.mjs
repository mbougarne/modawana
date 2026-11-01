import { spawn } from 'node:child_process';
import { mkdtemp, rm, stat } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { resolve } from 'node:path';
import assert from 'node:assert/strict';

const root = resolve(import.meta.dirname, '..');
const data = await mkdtemp(resolve(tmpdir(), 'blog-production-'));
const origin = 'http://127.0.0.1:18080';
const env = {
  ...process.env,
  BLOG_ENV: 'production',
  BLOG_BASE_URL: 'https://blog.test',
  BLOG_RP_ID: 'blog.test',
  BLOG_WEBAUTHN_ORIGINS: 'https://blog.test',
  BLOG_DATA_ROOT: data,
  BLOG_FRONTEND_DIST: resolve(root, 'frontend/dist'),
  BLOG_LISTEN_ADDR: '127.0.0.1:18080',
  BLOG_TRUSTED_PROXIES: '',
};
let server;
let stopped = true;
function start() {
  stopped = false;
  server = spawn(resolve(root, 'build/blog'), [], { cwd: root, stdio: 'inherit', env });
  server.once('exit', () => {
    stopped = true;
  });
}
async function stop() {
  if (!stopped) {
    const exited = new Promise((r) => server.once('exit', r));
    server.kill('SIGTERM');
    await exited;
  }
}
async function waitReady() {
  const deadline = Date.now() + 30000;
  while (Date.now() < deadline) {
    if (stopped) {
      throw Error('Go server stopped before readiness');
    }
    try {
      if ((await fetch(origin + '/readyz')).ok) {
        return;
      }
    } catch {
      /* Startup is polled until the bounded readiness deadline. */
    }
    await new Promise((r) => setTimeout(r, 200));
  }
  throw Error('readiness timeout');
}
try {
  start();
  await waitReady();
  const config = await fetch(origin + '/api/v1/public/runtime');
  assert.equal(config.status, 200);
  assert.deepEqual(await config.json(), {
    api_version: 'v1',
    default_locale: 'en',
    locales: ['en', 'ar', 'fr'],
  });
  for (const path of ['/api/v1/missing', '/assets/missing.js', '/media/private']) {
    const r = await fetch(origin + path);
    assert.equal(r.status, 404);
    assert.equal((await r.json()).error.code, 'not_found');
  }
  const html = await fetch(origin + '/ar');
  assert.equal(html.status, 200);
  assert.match(html.headers.get('content-security-policy'), /frame-ancestors 'none'/);
  const body = await html.text();
  assert.match(body, /lang="ar" dir="rtl"/);
  assert.ok(body.includes('property="og:image" content="https://blog.test/social-card.png"'));
  assert.ok(body.includes('name="twitter:card" content="summary_large_image"'));
  const card = await fetch(origin + '/social-card.png');
  assert.equal(card.status, 200);
  assert.equal(card.headers.get('content-type'), 'image/png');
  const png = Buffer.from(await card.arrayBuffer());
  assert.equal(png.subarray(0, 8).toString('hex'), '89504e470d0a1a0a');
  assert.equal(png.readUInt32BE(16), 1200);
  assert.equal(png.readUInt32BE(20), 630);
  const script = body.match(/src="(\/assets\/[^"]+\.js)"/)[1];
  const asset = await fetch(origin + script);
  assert.equal(asset.status, 200);
  assert.match(asset.headers.get('cache-control'), /immutable/);
  if (!process.argv.includes('--http-only')) {
    await new Promise((done, reject) => {
      const child = spawn(process.execPath, ['frontend/tests/visual.cjs'], {
        cwd: root,
        stdio: 'inherit',
        env: { ...process.env, BLOG_VISUAL_BASE_URL: origin },
      });
      child.once('error', reject);
      child.once('exit', (code) =>
        code === 0 ? done() : reject(Error('production browser checks failed')),
      );
    });
  }
  await stop();
  assert.ok((await stat(resolve(data, 'blog.sqlite'))).size > 0);
  start();
  await waitReady();
  assert.equal((await fetch(origin + '/healthz')).status, 200);
  console.log(
    `Go binary serves UI/API on one origin; HTTP/assets/CSP and SQLite restart passed (${process.argv.includes('--http-only') ? 'HTTP only' : 'browser included'}).`,
  );
} finally {
  await stop();
  await rm(data, { recursive: true, force: true });
}
