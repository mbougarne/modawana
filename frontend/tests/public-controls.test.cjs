const { test } = require('node:test');
const assert = require('node:assert/strict');
const { JSDOM } = require('jsdom');

test('live search cancels stale responses and leaves the GET form available on failure', async (t) => {
  t.mock.timers.enable({ apis: ['setTimeout'] });
  const dom = new JSDOM(
    `<form action="/en/search" method="get" data-live-search data-live-error="Try again">
      <input type="search" name="q"><input type="hidden" name="category" value="code">
    </form>
    <p data-live-status hidden></p>
    <div data-discovery-results><p>Initial results</p></div>`,
    { url: 'https://blog.test/en/search?category=code' },
  );
  const { window } = dom;
  for (const key of ['window', 'document', 'FormData', 'DOMParser', 'location', 'history']) {
    const prior = Object.getOwnPropertyDescriptor(globalThis, key);
    Object.defineProperty(globalThis, key, { configurable: true, value: window[key] });
    t.after(() => {
      if (prior) {
        Object.defineProperty(globalThis, key, prior);
      } else {
        delete globalThis[key];
      }
    });
  }
  t.after(() => dom.window.close());
  const requests = [];
  t.mock.method(globalThis, 'fetch', (url, options) => {
    return new Promise((resolve) => requests.push({ url, options, resolve }));
  });
  require('../.test-output/frontend/src/islands/publicControls');

  const input = window.document.querySelector('input[type="search"]');
  const results = window.document.querySelector('[data-discovery-results]');
  const status = window.document.querySelector('[data-live-status]');
  function type(query) {
    input.value = query;
    input.dispatchEvent(new window.Event('input'));
    t.mock.timers.tick(250);
    return requests.at(-1);
  }
  async function finish(request, text) {
    request.resolve({
      ok: !!text,
      text: async () => `<div data-discovery-results><p>${text}</p></div>`,
    });
    await new Promise((resolve) => setImmediate(resolve));
  }

  const slow = type('slow');
  const current = type('current');
  assert.equal(slow.options.signal.aborted, true);
  assert.equal(current.url.searchParams.get('category'), 'code');
  assert.equal(results.getAttribute('aria-busy'), 'true');
  await finish(current, 'Current results');
  await finish(slow, 'Obsolete results');
  assert.equal(results.textContent, 'Current results');
  assert.equal(new URL(window.location.href).searchParams.get('q'), 'current');
  assert.equal(results.hasAttribute('aria-busy'), false);

  await finish(type('unavailable'));
  assert.equal(status.hidden, false);
  assert.equal(status.textContent, 'Try again');
  assert.equal(new URL(window.location.href).searchParams.get('q'), 'current');
  assert.equal(window.document.querySelector('form').method, 'get');
});
