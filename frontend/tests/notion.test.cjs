const { test } = require('node:test');
const assert = require('node:assert/strict');
const { JSDOM } = require('jsdom');
const dom = new JSDOM('<!doctype html><html><body></body></html>', { url: 'http://localhost/' });
global.window = dom.window;
global.document = dom.window.document;
global.HTMLElement = dom.window.HTMLElement;
global.IS_REACT_ACT_ENVIRONMENT = true;
const React = require('react');
const { act } = React;
const { createRoot } = require('react-dom/client');
const { NotionImport } = require('../.test-output/frontend/src/features/owner/NotionImport');
const { I18nProvider } = require('../.test-output/ui/src/i18n/I18nProvider');

async function mount() {
  const el = document.createElement('div');
  document.body.append(el);
  const root = createRoot(el);
  await act(async () =>
    root.render(
      React.createElement(
        I18nProvider,
        { locale: 'en' },
        React.createElement(NotionImport, { csrf: 'test-csrf' }),
      ),
    ),
  );
  return {
    el,
    cleanup: async () => {
      await act(async () => root.unmount());
      el.remove();
    },
  };
}
const button = (el, label) =>
  [...el.querySelectorAll('button')].find((b) => b.textContent === label);
async function chooseFile(el, size = 100) {
  const input = el.querySelector('input[type="file"]');
  Object.defineProperty(input, 'files', {
    configurable: true,
    value: [{ name: 'export.zip', size }],
  });
  await act(async () => input.dispatchEvent(new dom.window.Event('change', { bubbles: true })));
}
const preview = {
  pages: [{ key: 'one', title: 'An imported lesson', kind: 'lesson', existing: false }],
  issues: [],
  media: 0,
  ignored: 0,
  skipped: 0,
  collection_title: 'A learning path',
};

test('Notion import requires a preview; changed options invalidate it and failed uploads can retry', async () => {
  const calls = [];
  let fail = false;
  global.fetch = async (url, options) => {
    if (url.endsWith('/session')) {
      return { ok: true, json: async () => ({ state: 'owner', csrf: 'test-csrf' }) };
    }
    calls.push({ url, options });
    if (fail) {
      throw new Error('network interrupted');
    }
    return {
      ok: true,
      json: async () =>
        url.includes('/preview?') ? preview : { revisions: 1, media: 0, skipped: 0 },
    };
  };
  const m = await mount();
  try {
    assert.equal(button(m.el, 'Preview Notion import').disabled, true);
    assert.equal(button(m.el, 'Import these drafts'), undefined);
    await chooseFile(m.el);
    await act(async () => button(m.el, 'Preview Notion import').click());
    assert.match(m.el.textContent, /An imported lesson/);
    assert.equal(calls[0].options.headers['X-CSRF-Token'], 'test-csrf');
    const select = m.el.querySelector('select');
    select.value = 'ar';
    await act(async () => select.dispatchEvent(new dom.window.Event('change', { bubbles: true })));
    assert.equal(button(m.el, 'Import these drafts'), undefined);
    await act(async () => button(m.el, 'Preview Notion import').click());
    assert.match(calls.at(-1).url, /locale=ar/);
    fail = true;
    await act(async () => button(m.el, 'Import these drafts').click());
    assert.equal(button(m.el, 'Import these drafts').disabled, false);
    fail = false;
    await act(async () => button(m.el, 'Import these drafts').click());
    assert.match(m.el.textContent, /Imported 1 drafts/);
    assert.equal(button(m.el, 'Import these drafts'), undefined);
  } finally {
    await m.cleanup();
  }
});

test('Notion unsupported-content preview blocks import and oversized replacement clears the preview', async () => {
  global.fetch = async () => ({
    ok: true,
    json: async () => ({
      ...preview,
      issues: [{ path: 'unsupported.md', code: 'unsupported_content' }],
    }),
  });
  const m = await mount();
  try {
    await chooseFile(m.el);
    await act(async () => button(m.el, 'Preview Notion import').click());
    assert.equal(button(m.el, 'Import these drafts').disabled, true);
    assert.match(m.el.querySelector('[role="alert"]').textContent, /unsupported.md/);
    await chooseFile(m.el, 257 * 1024 * 1024);
    assert.equal(button(m.el, 'Import these drafts'), undefined);
    assert.equal(button(m.el, 'Preview Notion import').disabled, true);
  } finally {
    await m.cleanup();
  }
});
