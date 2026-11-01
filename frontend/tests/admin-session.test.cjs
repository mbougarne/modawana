const { test } = require('node:test');
const assert = require('node:assert/strict');
const {
  ownerReturnTarget,
  redirectOwnerToLogin,
} = require('../.test-output/frontend/src/features/auth/navigation');
const { call } = require('../.test-output/frontend/src/features/auth/api');

test('owner return destinations preserve local editor/filter routes and reject external or normalized escapes', () => {
  for (const locale of ['en', 'ar', 'fr']) {
    const target = `/${locale}/admin/posts?q=hello%26world`;
    assert.equal(ownerReturnTarget(target), target);
  }
  for (const target of [
    null,
    'https://evil.example/en/admin',
    '//evil.example/en/admin',
    '/en/admin/../../outside',
    '/en/admin/%2e%2e/%2e%2e/outside',
    '/en/admin/\\evil.example',
    '/en/admin/%2f%2fevil.example',
    '/en/admin/%5cevil.example',
    '/en/admin/login',
    '/en/admin/login/',
    '/en/administrator',
    '/en/admin\n/secret',
  ]) {
    assert.equal(ownerReturnTarget(target), null, String(target));
  }
});

test('expired owner navigation uses the active locale and never redirects the public login form', () => {
  for (const locale of ['en', 'ar', 'fr']) {
    let destination;
    redirectOwnerToLogin({
      pathname: `/${locale}/admin/editor/lesson/ar`,
      search: '?tab=details',
      replace: (value) => (destination = value),
    });
    const parsed = new URL(destination, 'https://blog.example');
    assert.equal(parsed.pathname, `/${locale}/login`);
    assert.equal(
      parsed.searchParams.get('return'),
      `/${locale}/admin/editor/lesson/ar?tab=details`,
    );
  }
  redirectOwnerToLogin({
    pathname: '/en/login',
    search: '?return=https://evil.example',
    replace: () => assert.fail('login redirect loop'),
  });
});

test('expired security API requests redirect admin navigation but preserve the structured error; 403 does not redirect', async (t) => {
  const previousWindow = global.window;
  const previousFetch = global.fetch;
  t.after(() => {
    global.window = previousWindow;
    global.fetch = previousFetch;
  });
  const destinations = [];
  global.window = {
    location: {
      pathname: '/ar/admin/security',
      search: '',
      replace: (target) => destinations.push(target),
    },
  };
  let status = 401;
  global.fetch = async (url) =>
    url === '/api/v1/session'
      ? {
          status: 200,
          ok: true,
          json: async () => ({ state: 'owner' }),
        }
      : {
          status,
          ok: false,
          json: async () => ({ error: { code: status === 401 ? 'unauthenticated' : 'forbidden' } }),
        };
  await assert.rejects(call('/security/factors'), { code: 'unauthenticated' });
  assert.equal(destinations.length, 1);
  assert.match(destinations[0], /^\/ar\/login\?return=/);
  status = 403;
  await assert.rejects(call('/security/factors'), { code: 'forbidden' });
  assert.equal(destinations.length, 1);
});

test('public and learner failures never start owner passkey ceremonies, including from owner previews', async (t) => {
  const previousWindow = global.window;
  const previousFetch = global.fetch;
  const previousNavigator = Object.getOwnPropertyDescriptor(global, 'navigator');
  t.after(() => {
    global.window = previousWindow;
    global.fetch = previousFetch;
    if (previousNavigator) {
      Object.defineProperty(global, 'navigator', previousNavigator);
    } else {
      delete global.navigator;
    }
  });
  Object.defineProperty(global, 'navigator', {
    configurable: true,
    value: { credentials: { get: () => assert.fail('public request prompted for passkey') } },
  });
  for (const pathname of ['/en/articles/first', '/ar/admin/editor/first/ar']) {
    global.window = { location: { pathname, replace: () => assert.fail('public redirect') } };
    for (const path of ['/public/content/en/locked', '/learning/attempts', '/auth/start']) {
      const requests = [];
      global.fetch = async (url) => {
        requests.push(url);
        return { status: 403, ok: false, json: async () => ({ error: { code: 'lesson_locked' } }) };
      };
      await assert.rejects(call(path), { code: 'lesson_locked' });
      assert.deepEqual(requests, ['/api/v1' + path]);
    }
  }
});

test('security CSRF denial probes session loss without replaying the mutation or redirecting a valid owner', async (t) => {
  const previousWindow = global.window;
  const previousFetch = global.fetch;
  t.after(() => {
    global.window = previousWindow;
    global.fetch = previousFetch;
  });
  for (const locale of ['en', 'ar', 'fr']) {
    for (const state of ['anonymous', 'owner', 'recovery', 'unavailable']) {
      const calls = [];
      const destinations = [];
      global.window = {
        location: {
          pathname: `/${locale}/admin/security`,
          search: '',
          replace: (url) => destinations.push(url),
        },
      };
      global.fetch = async (url, options) => {
        calls.push([url, options.method]);
        if (url === '/api/v1/session') {
          if (state === 'unavailable') {
            throw new Error('offline');
          }
          return { status: 200, ok: true, json: async () => ({ state }) };
        }
        return { status: 403, ok: false, json: async () => ({ error: { code: 'forbidden' } }) };
      };
      await assert.rejects(call('/security/recovery-codes', 'POST', {}, 'stale-csrf'), {
        code: 'forbidden',
      });
      assert.deepEqual(calls, [
        ['/api/v1/security/recovery-codes', 'POST'],
        ['/api/v1/session', 'GET'],
      ]);
      assert.equal(destinations.length, ['anonymous', 'recovery'].includes(state) ? 1 : 0);
      if (destinations.length) {
        const url = new URL(destinations[0], 'https://blog.example');
        assert.equal(url.pathname, `/${locale}/login`);
        assert.equal(url.searchParams.get('return'), `/${locale}/admin/security`);
      }
    }
  }
});
