const { test } = require('node:test');
const assert = require('node:assert/strict');
const path = require('node:path');
const Module = require('node:module');
const original = Module._resolveFilename;
Module._resolveFilename = function (request, ...args) {
  return original.call(
    this,
    request === '@ui' ? path.resolve(__dirname, '../.test-output/ui/src/index.js') : request,
    ...args,
  );
};
const { JSDOM } = require('jsdom');
const dom = new JSDOM('<!doctype html><html><body></body></html>', {
  url: 'http://localhost/en/search',
});
global.window = dom.window;
global.document = dom.window.document;
global.HTMLElement = dom.window.HTMLElement;
global.IS_REACT_ACT_ENVIRONMENT = true;
const React = require('react');
const { act } = React;
const { createRoot } = require('react-dom/client');
const { MemoryRouter } = require('react-router-dom');
const { I18nProvider } = require('../.test-output/ui/src/i18n/I18nProvider');
const { WritingPage } = require('../.test-output/frontend/src/pages/public/writing');
async function fill(input, value) {
  await act(async () => {
    Object.getOwnPropertyDescriptor(dom.window.HTMLInputElement.prototype, 'value').set.call(
      input,
      value,
    );
    input.dispatchEvent(new dom.window.Event('input', { bubbles: true }));
  });
}
async function tick() {
  await act(async () => new Promise((resolve) => setTimeout(resolve, 300)));
}
test('Live search keeps focus while updating the URL and rejects delayed older results', async () => {
  let resolveOld;
  const calls = [];
  global.fetch = async (url) => {
    calls.push(url);
    if (url.includes('taxonomy')) {
      return { ok: true, json: async () => ({ items: [] }) };
    }
    const q = new URL(url, 'http://test').searchParams.get('q');
    const result = {
      items: q
        ? [
            {
              key: q,
              slug: q,
              kind: 'article',
              title: q,
              url: '/en/articles/' + q,
              published_at: '2026-10-25T00:00:00Z',
              tag_keys: [],
            },
          ]
        : [],
      total: q ? 1 : 0,
      next_cursor: '',
    };
    if (q === 'slow') {
      return new Promise((resolve) => {
        resolveOld = () => resolve({ ok: true, json: async () => result });
      });
    }
    return { ok: true, json: async () => result };
  };
  const el = document.createElement('div');
  document.body.append(el);
  const root = createRoot(el);
  await act(async () =>
    root.render(
      React.createElement(
        MemoryRouter,
        { initialEntries: ['/en/search'] },
        React.createElement(I18nProvider, { locale: 'en' }, React.createElement(WritingPage)),
      ),
    ),
  );
  try {
    const input = el.querySelector('input[type="search"]');
    await act(async () => input.focus());
    await fill(input, 'slow');
    await tick();
    assert.equal(typeof resolveOld, 'function');
    await fill(input, 'fresh');
    await tick();
    assert.equal(document.activeElement, input);
    assert.equal(el.querySelector('.vb-post-main b').textContent, 'fresh');
    await act(async () => resolveOld());
    assert.equal(el.querySelector('.vb-post-main b').textContent, 'fresh');
    assert.ok(calls.some((url) => url.includes('q=fresh')));
    await act(async () => {
      input.blur();
      document.body.dispatchEvent(
        new dom.window.KeyboardEvent('keydown', { key: '/', bubbles: true }),
      );
    });
    assert.equal(document.activeElement, input);
  } finally {
    await act(async () => root.unmount());
    el.remove();
  }
});
