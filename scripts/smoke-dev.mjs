import { spawn } from 'node:child_process';
import assert from 'node:assert/strict';
import { createRequire } from 'node:module';
import { resolve } from 'node:path';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';

const secure = process.argv.includes('--https');
const browserTest = secure || process.argv.includes('--auth');
const origin = secure ? 'https://localhost:3000' : 'http://localhost:3000';
const data = await mkdtemp(resolve(tmpdir(), 'blog-dev-smoke-'));
const child = spawn('node', ['scripts/project.mjs', secure ? 'dev:https' : 'dev'], {
  stdio: 'inherit',
  detached: process.platform !== 'win32',
  env: { ...process.env, BLOG_DATA_ROOT: data },
});
let browser;
try {
  let ready = false;
  for (let attempt = 0; attempt < 90; attempt++) {
    if (child.exitCode !== null) {
      throw new Error('Development processes exited');
    }
    try {
      const response = await fetch(
        secure ? 'http://127.0.0.1:8082/readyz' : 'http://127.0.0.1:3000/readyz',
      );
      if (response.ok && (await response.json()).status === 'ok') {
        ready = true;
        break;
      }
    } catch {
      /* Retry while the development servers are starting. */
    }
    await new Promise((resolve) => setTimeout(resolve, 500));
  }
  assert.ok(ready, 'Development proxy did not become healthy');
  if (browserTest) {
    const require = createRequire(resolve(import.meta.dirname, '../frontend/package.json'));
    const { chromium } = require('playwright');
    browser = await chromium.launch({ args: ['--host-resolver-rules=MAP localhost 127.0.0.1'] });
    const page = await browser.newPage({ ignoreHTTPSErrors: true });
    // Go can be ready before Webpack has finished compiling.
    let loaded = false;
    for (let attempt = 0; attempt < 90; attempt++) {
      try {
        const response = await page.goto(origin + '/en/login', { timeout: 1000 });
        if (response?.status() === 200) {
          loaded = true;
          break;
        }
      } catch {
        /* Retry until Webpack accepts connections. */
      }
      await new Promise((resolve) => setTimeout(resolve, 500));
    }
    assert.ok(loaded, 'Frontend did not become ready');
    assert.ok(
      await page.evaluate(() => window.isSecureContext),
      'Localhost must be a secure context',
    );
    const cdp = await page.context().newCDPSession(page);
    await cdp.send('WebAuthn.enable');
    await cdp.send('WebAuthn.addVirtualAuthenticator', {
      options: {
        protocol: 'ctap2',
        transport: 'internal',
        hasResidentKey: true,
        hasUserVerification: true,
        isUserVerified: true,
        automaticPresenceSimulation: true,
      },
    });
    const created = await page.evaluate(async () => {
      const credential = await navigator.credentials.create({
        publicKey: {
          rp: { id: 'localhost', name: 'Local development test' },
          user: { id: new Uint8Array([1]), name: 'test@example.invalid', displayName: 'Test' },
          challenge: crypto.getRandomValues(new Uint8Array(32)),
          pubKeyCredParams: [{ type: 'public-key', alg: -7 }],
          authenticatorSelection: { residentKey: 'required', userVerification: 'required' },
        },
      });
      return credential?.type;
    });
    assert.equal(created, 'public-key', 'Localhost RP must permit a UV resident credential');
    const result = await page.evaluate(async () => {
      const start = await fetch('/api/v1/auth/start', {
        method: 'POST',
        headers: { 'X-Blog-Request': '1' },
      });
      const initial = await start.json();
      const session = await fetch('/api/v1/session');
      const current = await session.json();
      const denied = await fetch('/api/v1/auth/grant', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': 'invalid' },
        body: JSON.stringify({ grant: '', email: '' }),
      });
      const confirmed = await fetch('/api/v1/auth/grant', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': initial.csrf },
        body: JSON.stringify({ grant: 'invalid-test-grant', email: 'test@example.invalid' }),
      });
      return {
        status: start.status,
        initial,
        current,
        denied: denied.status,
        confirmed: confirmed.status,
      };
    });
    assert.equal(result.status, 200);
    assert.ok(result.initial.csrf);
    assert.equal(result.current.csrf, result.initial.csrf, 'Secure flow cookie must survive');
    assert.equal(result.denied, 403, 'Invalid CSRF must still be rejected');
    assert.equal(result.confirmed, 401, 'Valid CSRF must reach grant verification');
    const cookies = await page.context().cookies();
    const flow = cookies.find((cookie) => cookie.name === '__Host-auth');
    assert.ok(flow?.secure && flow.httpOnly && flow.sameSite === 'Strict');
  } else {
    const page = await fetch('http://127.0.0.1:3000/en');
    assert.equal(page.status, 200);
    assert.match(await page.text(), /id="root"/);
  }
  console.log('Development frontend and proxied Go health endpoint passed.');
} finally {
  await browser?.close();
  if (child.pid) {
    try {
      process.kill(process.platform === 'win32' ? child.pid : -child.pid, 'SIGTERM');
    } catch {
      /* The development child may have already exited. */
    }
  }
  if (child.exitCode === null && child.signalCode === null) {
    await new Promise((resolve) => child.once('exit', resolve));
  }
  await rm(data, { recursive: true, force: true });
}
