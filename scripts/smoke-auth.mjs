import { smokeSuite, smokeLocales } from './smoke-policy.mjs';
import { smokeAcceptance } from './smoke-acceptance.mjs';
import { smokeTranslator } from './smoke-i18n.mjs';
import { smokeTransfer } from './smoke-transfer.mjs';
import { smokeOAuth } from './smoke-oauth.mjs';
import { smokePublisher } from './smoke-publisher.mjs';
import { smokeLearning } from './smoke-learning.mjs';
import { smokeOwner } from './smoke-owner.mjs';
// Real Go/SQLite/HTTPS + browser ceremonies. Authenticators are simulated, not hardware.
import { spawn, spawnSync, execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { mkdtemp, mkdir, writeFile, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { resolve } from 'node:path';
import { randomBytes, createHmac } from 'node:crypto';
import https from 'node:https';
import http from 'node:http';
import { createRequire } from 'node:module';
import assert from 'node:assert/strict';
import { smokeMedia } from './smoke-media.mjs';
import { smokeContent } from './smoke-content.mjs';
import { smokeConfiguration } from './smoke-configuration.mjs';
import { smokePublic } from './smoke-public.mjs';

const require = createRequire(resolve(import.meta.dirname, '../frontend/package.json'));
const { chromium } = require('playwright');
const root = resolve(import.meta.dirname, '..');
const suite = smokeSuite();
const locales = smokeLocales(suite);
console.log(
  `Browser suite: ${suite}; locales: ${locales.join(',')}; runtime: ${process.env.BLOG_TEST_IMAGE ? 'container' : 'native'}`,
);
let activeStage = 'startup';
const recentResponses = [];
async function stage(locale, name, run) {
  activeStage = `${locale}/${name}`;
  const start = performance.now();
  console.log(`${locale}: starting ${name}`);
  try {
    await run();
  } catch (error) {
    throw new Error(`${locale}/${name}: ${error.message}`, { cause: error });
  } finally {
    console.log(`${locale}/${name}: ${((performance.now() - start) / 1000).toFixed(2)}s`);
  }
}
const tmp = await mkdtemp(resolve(tmpdir(), 'blog-auth-'));
const origin = 'https://localhost:18443';
const cert = resolve(tmp, 'cert.pem');
const key = resolve(tmp, 'tls.key');
const authKey = resolve(tmp, 'auth.key');
const openssl = spawnSync(
  'openssl',
  [
    'req',
    '-x509',
    '-newkey',
    'rsa:2048',
    '-nodes',
    '-keyout',
    key,
    '-out',
    cert,
    '-days',
    '1',
    '-subj',
    '/CN=localhost',
    '-addext',
    'subjectAltName=DNS:localhost,IP:127.0.0.1',
  ],
  { stdio: 'ignore' },
);
assert.equal(openssl.status, 0);
await writeFile(authKey, randomBytes(32), { mode: 0o600 });
const proxy = https.createServer(
  { key: await readFile(key), cert: await readFile(cert) },
  (request, response) => {
    const upstream = http.request(
      {
        hostname: '127.0.0.1',
        port: 18081,
        path: request.url,
        method: request.method,
        headers: request.headers,
      },
      (r) => {
        response.writeHead(r.statusCode, r.headers);
        r.pipe(response);
      },
    );
    upstream.on('error', () => {
      response.writeHead(502);
      response.end();
    });
    request.pipe(upstream);
  },
);
await new Promise((r) => proxy.listen(18443, '127.0.0.1', r));
const browser = await chromium.launch({
  headless: true,
  args: ['--host-resolver-rules=MAP localhost 127.0.0.1'],
});
const execute = promisify(execFile);
let server;
let containerName;
async function stop() {
  if (server) {
    const child = server;
    server = null;
    await new Promise((r) => {
      child.once('exit', r);
      if (containerName) {
        execute('docker', ['stop', '-t', '10', containerName]).catch(r);
      } else {
        child.kill('SIGTERM');
      }
    });
  }
}
function otp(secret, step = Math.floor(Date.now() / 30000)) {
  const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567';
  let bits = '';
  for (const c of secret) {
    bits += alphabet.indexOf(c).toString(2).padStart(5, '0');
  }
  const bytes = [];
  for (let i = 0; i + 8 <= bits.length; i += 8) {
    bytes.push(parseInt(bits.slice(i, i + 8), 2));
  }
  const counter = Buffer.alloc(8);
  counter.writeBigUInt64BE(BigInt(step));
  const h = createHmac('sha1', Buffer.from(bytes)).update(counter).digest();
  return ((h.readUInt32BE(h[19] & 15) & 0x7fffffff) % 1000000).toString().padStart(6, '0');
}
try {
  for (const locale of locales) {
    const data = resolve(tmp, locale);
    await mkdir(data, { mode: 0o700 });
    const env = {
      ...process.env,
      BLOG_ENV: 'production',
      BLOG_BASE_URL: origin,
      BLOG_RP_ID: 'localhost',
      BLOG_WEBAUTHN_ORIGINS: origin,
      BLOG_DATA_ROOT: data,
      BLOG_FRONTEND_DIST: resolve(root, 'frontend/dist'),
      BLOG_LISTEN_ADDR: '127.0.0.1:18081',
      BLOG_AUTH_KEY_FILE: authKey,
      BLOG_TRUSTED_PROXIES: '',
    };
    function grant(command) {
      const out = spawnSync(resolve(root, 'build/blog'), [command], {
        env,
        cwd: root,
        encoding: 'utf8',
      });
      assert.equal(out.status, 0, 'local command failed');
      assert.match(out.stdout.trim(), /^[A-Za-z0-9_-]{43}$/);
      return out.stdout.trim();
    }
    let activeData = data;
    const setup = grant('bootstrap');
    async function startServer(maintenance = false) {
      if (process.env.BLOG_TEST_IMAGE) {
        containerName = 'blog-acceptance-' + process.pid + '-' + locale;
        const containerEnv = {
          ...env,
          BLOG_DATA_ROOT: '/var/lib/blog',
          BLOG_FRONTEND_DIST: '/app/frontend',
          BLOG_LISTEN_ADDR: '0.0.0.0:18081',
          BLOG_AUTH_KEY_FILE: '/run/auth-key',
          TMPDIR: '/var/lib/blog/tmp',
        };
        const args = [
          'run',
          '--rm',
          '--name',
          containerName,
          '--read-only',
          '--cap-drop=ALL',
          '--security-opt=no-new-privileges',
          '--user',
          process.getuid() + ':' + process.getgid(),
          '-p',
          '127.0.0.1:18081:18081',
          '--mount',
          'type=bind,source=' + activeData + ',target=/var/lib/blog',
          '--mount',
          'type=bind,source=' + authKey + ',target=/run/auth-key,readonly',
        ];
        for (const [key, value] of Object.entries(containerEnv)) {
          if ((key.startsWith('BLOG_') && key !== 'BLOG_TEST_IMAGE') || key === 'TMPDIR') {
            args.push('-e', key + '=' + value);
          }
        }
        args.push(process.env.BLOG_TEST_IMAGE);
        server = spawn('docker', args, { stdio: ['ignore', 'ignore', 'inherit'] });
      } else {
        server = spawn(resolve(root, 'build/blog'), [], {
          env,
          cwd: root,
          stdio: ['ignore', 'ignore', 'ignore'],
        });
      }
      let ready = false;
      for (let n = 0; n < 100; n++) {
        try {
          if ((await fetch('http://127.0.0.1:18081/' + (maintenance ? 'healthz' : 'readyz'))).ok) {
            ready = true;
            break;
          }
        } catch {
          /* Startup is polled until the bounded readiness deadline. */
        }
        await new Promise((r) => setTimeout(r, 100));
      }
      assert.ok(ready);
    }
    await startServer();
    const context = await browser.newContext({
      ignoreHTTPSErrors: true,
      viewport: { width: 390, height: 844 },
      colorScheme: 'light',
    });
    activeStage = `${locale}/authentication`;
    context.on('response', (response) => {
      const pathname = new URL(response.url()).pathname;
      if (!/^\/api\/v1\/(auth|session|security)(\/|$)/.test(pathname)) {
        return;
      }
      recentResponses.push({
        method: response.request().method(),
        path: pathname,
        status: response.status(),
      });
      if (recentResponses.length > 20) {
        recentResponses.shift();
      }
    });
    const page = await context.newPage();
    page.setDefaultTimeout(30000);
    const errors = [];
    page.on('pageerror', (e) => errors.push(e.message));
    page.on('console', (m) => {
      if (
        m.type() === 'error' &&
        /Content Security Policy|Refused to|violates.*directive/i.test(m.text())
      ) {
        errors.push(m.text());
      }
    });
    const cdp = await context.newCDPSession(page);
    await cdp.send('WebAuthn.enable');
    let virtual = await cdp.send('WebAuthn.addVirtualAuthenticator', {
      options: {
        protocol: 'ctap2',
        transport: 'internal',
        hasResidentKey: true,
        hasUserVerification: true,
        isUserVerified: true,
        automaticPresenceSimulation: true,
      },
    });
    async function replacement() {
      await cdp.send('WebAuthn.setAutomaticPresenceSimulation', {
        authenticatorId: virtual.authenticatorId,
        enabled: false,
      });
      virtual = await cdp.send('WebAuthn.addVirtualAuthenticator', {
        options: {
          protocol: 'ctap2',
          transport: 'usb',
          hasResidentKey: true,
          hasUserVerification: true,
          isUserVerified: true,
          automaticPresenceSimulation: true,
        },
      });
    }
    const t = smokeTranslator(locale);
    const button = (k) => page.getByRole('button', { name: t(k), exact: true });
    const login = async () => {
      await page.getByTestId('passkey-login').click();
      await page.getByRole('heading', { name: t('auth.security'), exact: true }).waitFor();
    };
    const shot = async (name) => {
      await page.evaluate(() => document.fonts.ready);
      assert.ok(
        await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth),
        'horizontal overflow',
      );
      await page.screenshot({
        path: resolve(root, `frontend/test-results/auth-${locale}-${name}.png`),
        fullPage: true,
      });
    };
    assert.equal((await context.request.get(origin + `/${locale}/admin/security`)).status(), 401);
    for (const path of ['/admin/security', '/admin/posts', '/admin/editor/private-key/ar']) {
      await page.goto(origin + '/' + locale + path);
      await page.getByTestId('passkey-login').waitFor();
      const location = new URL(page.url());
      assert.equal(location.pathname, `/${locale}/login`);
      assert.equal(location.searchParams.get('return'), '/' + locale + path);
    }
    await page.goto(origin + `/${locale}/admin/login`);
    await page.getByTestId('passkey-login').waitFor();
    assert.equal(new URL(page.url()).pathname, `/${locale}/login`);
    assert.equal(
      (
        await context.request.get(origin + '/api/v1/security/factors', {
          headers: { Accept: 'text/html' },
        })
      ).status(),
      401,
    );
    await page.goto(origin + `/${locale}/login`);
    await button('auth.setupOption').waitFor();
    await shot('login');
    await page.evaluate(() => {
      Object.defineProperty(window, 'PublicKeyCredential', {
        value: undefined,
        configurable: true,
      });
    });
    await button('auth.passkey').click();
    await page
      .getByRole('status')
      .filter({ hasText: t('auth.unsupported') })
      .waitFor();
    await page.reload();
    await page.evaluate(() => {
      navigator.credentials.get = ({ signal }) =>
        new Promise((resolve, reject) => {
          signal.addEventListener(
            'abort',
            () => reject(new DOMException('test cancellation', 'AbortError')),
            { once: true },
          );
        });
    });
    await button('auth.passkey').focus();
    await page.keyboard.press('Enter');
    await button('auth.cancel').click();
    await page
      .getByRole('status')
      .filter({ hasText: t('auth.cancelled') })
      .waitFor();
    await page.reload();
    await button('auth.setupOption').click();
    await page.getByLabel(t('auth.email'), { exact: true }).fill('test-owner@example.test');
    await page.getByLabel(t('auth.grant'), { exact: true }).fill(setup);
    await button('auth.continue').click();
    await page.getByLabel(t('auth.keyLabel'), { exact: true }).fill('Daily');
    await button('auth.enroll').click();
    await login();
    assert.equal((await context.cookies()).find((c) => c.name === '__Host-owner').secure, true);
    // Losing the owner cookie while Security is open must reach the full login
    // form (including password/TOTP), rather than retaining a broken owner page.
    await stage(locale, 'lost-session-redirect', async () => {
      assert.equal(new URL(page.url()).pathname, `/${locale}/admin/security`);
      await context.clearCookies();
      assert.equal(
        (await context.cookies()).some((cookie) => cookie.name === '__Host-owner'),
        false,
      );
      const [denied] = await Promise.all([
        page.waitForResponse(
          (response) =>
            new URL(response.url()).pathname === '/api/v1/security/recovery-codes' &&
            response.request().method() === 'POST',
        ),
        button('auth.generate').click(),
      ]);
      assert.equal(denied.status(), 403, 'lost cookie must be rejected by the security CSRF check');
      await page.waitForURL((url) => url.pathname === `/${locale}/login`);
    });
    assert.equal(new URL(page.url()).searchParams.get('return'), `/${locale}/admin/security`);
    await page.getByTestId('passkey-login').waitFor();
    await login();
    await page.waitForURL((url) => url.pathname === `/${locale}/admin/security`);
    await page.getByRole('heading', { name: t('auth.security'), exact: true }).waitFor();
    if (locale === 'en') {
      await stage(locale, 'media', () => smokeMedia({ context, page, origin, root }));
      await stage(locale, 'content', () => smokeContent({ context, origin, root }));
    }
    assert.equal(
      (
        await context.request.post(origin + '/api/v1/security/recovery-codes', {
          headers: { Origin: origin },
          data: {},
        })
      ).status(),
      403,
    );
    await stage(locale, 'configuration', () =>
      smokeConfiguration({ context, origin, root, locale, t }),
    );
    await stage(locale, 'public', () => smokePublic({ context, origin, root, locale, t }));
    await stage(locale, 'owner', () => smokeOwner({ context, origin, root, locale, t }));
    await stage(locale, 'learning', () => smokeLearning({ context, origin, root, locale, t }));
    await stage(locale, 'oauth', () => smokeOAuth({ context, page, origin, locale, t }));
    await stage(locale, 'publisher', () =>
      smokePublisher({ context, origin, root, locale, t, cert, data }),
    );
    await stage(locale, 'transfer', () => smokeTransfer({ context, origin, root, locale, t }));
    if (!process.env.BLOG_TEST_IMAGE) {
      await stage(locale, 'acceptance', () => smokeAcceptance({ context, origin, root, locale }));
    }
    if (process.env.BLOG_TEST_IMAGE) {
      const url = origin + '/api/v1/content/editor-' + locale + '/' + locale;
      const before = await (await context.request.get(url)).json();
      const mediaBefore = await (await context.request.get(origin + '/api/v1/media')).json();
      await stop();
      await startServer();
      const after = await context.request.get(url);
      assert.equal(after.status(), 200, 'owner session survived recreation');
      assert.deepEqual(await after.json(), before, 'immutable revisions survived recreation');
      assert.deepEqual(
        await (await context.request.get(origin + '/api/v1/media')).json(),
        mediaBefore,
        'media metadata survived recreation',
      );
      for (const item of mediaBefore.items.slice(0, 3)) {
        assert.equal(
          (await context.request.get(origin + item.url)).status(),
          200,
          'media bytes survived recreation',
        );
      }
      console.log(
        locale + ': isolated container recreation retained login, revisions and uploads.',
      );
    }
    activeStage = `${locale}/backup-factor-and-recovery`;
    await page.bringToFront();
    // Add and test an independently registered virtual authenticator.
    await cdp.send('WebAuthn.setAutomaticPresenceSimulation', {
      authenticatorId: virtual.authenticatorId,
      enabled: false,
    });
    virtual = await cdp.send('WebAuthn.addVirtualAuthenticator', {
      options: {
        protocol: 'ctap2',
        transport: 'usb',
        hasResidentKey: true,
        hasUserVerification: true,
        isUserVerified: true,
        automaticPresenceSimulation: true,
      },
    });
    await page
      .getByLabel(t('auth.keyLabel'), { exact: true })
      .last()
      .fill(locale === 'ar' ? 'مفتاح احتياطي' : 'Backup');
    await page.getByTestId('add-passkey').click();
    await login();
    await button('auth.generate').click();
    await page.getByTestId('recovery-codes').waitFor();
    const codes = await page.getByTestId('recovery-codes').locator('li').allTextContents();
    assert.equal(codes.length, 10);
    await button('auth.hideCodes').click();
    await page
      .getByTestId('security-events')
      .getByText(t('securityEvent.recovery_codes_regenerated'), { exact: false })
      .waitFor();
    assert.ok((await page.getByTestId('security-events').locator('li').count()) > 0);
    await shot('security');
    await page.emulateMedia({ colorScheme: 'dark' });
    await shot('security-dark');
    await page.emulateMedia({ colorScheme: 'light' });
    await button('auth.signout').click();
    await button('auth.recoveryOption').click();
    await page.getByLabel(t('auth.recoveryCode'), { exact: true }).fill(codes[0]);
    await button('auth.continue').click();
    await replacement();
    await page.getByLabel(t('auth.keyLabel'), { exact: true }).fill('Recovered');
    await button('auth.enroll').click();
    await login();
    // Optional password + TOTP enrollment. The +1 allowed step avoids reusing the enrollment step.
    await page.getByTestId('password').fill('test-only long passphrase');
    await button('auth.showPassword').click();
    assert.equal(await page.getByTestId('password').getAttribute('type'), 'text');
    await button('auth.hidePassword').click();
    await button('auth.enable').click();
    await page.getByTestId('totp-secret').waitFor();
    const secret = await page.getByTestId('totp-secret').textContent();
    const enrollmentStep = Math.floor(Date.now() / 30000);
    await page.getByTestId('otp').fill(otp(secret, enrollmentStep));
    await button('auth.verify').click();
    await button('auth.passkey').waitFor();
    await button('auth.passwordOption').click();
    await page.getByLabel(t('auth.email'), { exact: true }).fill('test-owner@example.test');
    await page.getByTestId('password').fill('test-only long passphrase');
    await button('auth.continue').click();
    await page.getByTestId('otp').fill(otp(secret, enrollmentStep + 1));
    await button('auth.verify').click();
    await page.getByRole('heading', { name: t('auth.security'), exact: true }).waitFor();
    await button('auth.revokeAll').click();
    await page.getByTestId('passkey-login').waitFor();
    assert.equal((await context.request.get(origin + `/${locale}/admin/security`)).status(), 401);
    await login();
    // Local command revokes active sessions and grants enrollment only.
    const backupFile = resolve(tmp, locale + '-restore.zip');
    let operation = spawnSync(resolve(root, 'build/blog'), ['backup', '--file', backupFile], {
      env,
      cwd: root,
      encoding: 'utf8',
    });
    assert.equal(operation.status, 0, operation.stderr);
    await stop();
    activeData = resolve(tmp, locale + '-restored');
    env.BLOG_DATA_ROOT = activeData;
    operation = spawnSync(resolve(root, 'build/blog'), ['restore', '--file', backupFile], {
      env,
      cwd: root,
      encoding: 'utf8',
    });
    assert.equal(operation.status, 0, operation.stderr);
    await startServer(true);
    assert.equal((await context.request.get(origin + '/' + locale)).status(), 503);
    assert.equal(
      spawnSync(resolve(root, 'build/blog'), ['resume'], { env, cwd: root, encoding: 'utf8' })
        .status,
      1,
    );
    const recovery = grant('recover');
    assert.equal((await context.request.get(origin + `/${locale}/admin/security`)).status(), 401);
    await page.goto(origin + `/${locale}/login`);
    await button('auth.setupOption').click();
    await page.getByLabel(t('auth.email'), { exact: true }).fill('test-owner@example.test');
    await page.getByLabel(t('auth.grant'), { exact: true }).fill(recovery);
    await button('auth.continue').click();
    await replacement();
    await page.getByLabel(t('auth.keyLabel'), { exact: true }).fill('Local recovery');
    await button('auth.enroll').click();
    await login();
    operation = spawnSync(resolve(root, 'build/blog'), ['resume'], {
      env,
      cwd: root,
      encoding: 'utf8',
    });
    assert.equal(operation.status, 0, operation.stderr);
    assert.equal((await context.request.get(origin + '/readyz')).status(), 200);
    assert.equal(
      (
        await context.request.get(origin + '/api/v1/content/editor-' + locale + '/' + locale)
      ).status(),
      200,
    );
    console.log(
      locale +
        ': full backup/maintenance restore, invalidated sessions and tested-factor re-enrollment passed.',
    );
    await button('auth.signout').click();
    assert.equal((await context.request.get(origin + `/${locale}/admin/security`)).status(), 401);
    assert.deepEqual(errors, []);
    await context.close();
    await stop();
    console.log(
      `${locale}: setup, two passkeys, recovery, TOTP, local recovery, logout and private-route denial passed.`,
    );
  }
} catch (error) {
  // Never attach auth traces, response bodies, headers, cookies or secret-field screenshots.
  const diagnostics = { stage: activeStage, responses: recentResponses };
  console.error('Auth smoke diagnostics:', JSON.stringify(diagnostics));
  await mkdir(resolve(root, 'frontend/test-results'), { recursive: true });
  await writeFile(
    resolve(root, 'frontend/test-results/auth-failure.json'),
    JSON.stringify(diagnostics, null, 2),
  );
  throw error;
} finally {
  await stop();
  await browser.close();
  await new Promise((r) => proxy.close(r));
  await rm(tmp, { recursive: true, force: true });
}
