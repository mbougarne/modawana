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
const { BulkPublication } = require('../.test-output/frontend/src/features/owner/BulkPublication');
const { I18nProvider } = require('../.test-output/ui/src/i18n/I18nProvider');
const { DEFAULT_LABELS } = require('../.test-output/ui/src/i18n/labels');
const text = (key) => DEFAULT_LABELS[key].en;
const button = (el, key) =>
  [...el.querySelectorAll('button')].find((b) => b.textContent === text(key));
const entry = (key, extra = {}) => ({
  key,
  locale: 'en',
  title: key,
  kind: 'article',
  status: 'draft',
  version: 3,
  draft_revision_id: 'a'.repeat(32),
  published_revision_id: '',
  ...extra,
});
const response = (json, ok = true) => ({ ok, json: async () => json });
async function mount(items, query = 'locale=en&kind=article&q=notion') {
  const el = document.createElement('div');
  document.body.append(el);
  const root = createRoot(el);
  let applied = 0;
  await act(async () =>
    root.render(
      React.createElement(
        I18nProvider,
        { locale: 'en' },
        React.createElement(BulkPublication, {
          items,
          query,
          csrf: 'owner-csrf',
          loading: false,
          onApplied: () => applied++,
          children: (selected, toggle) =>
            React.createElement(
              'div',
              {},
              items.map((item) =>
                React.createElement(
                  'label',
                  { key: item.key },
                  item.title,
                  React.createElement('input', {
                    type: 'checkbox',
                    checked: !!selected[item.key + '/' + item.locale],
                    onChange: () => toggle(item),
                    'data-key': item.key,
                  }),
                ),
              ),
            ),
        }),
      ),
    ),
  );
  return {
    el,
    applied: () => applied,
    cleanup: async () => {
      await act(async () => root.unmount());
      el.remove();
    },
  };
}

test('Bulk status label excludes option text before and after changing selection', async () => {
  const m = await mount([entry('one')]);
  try {
    const select = m.el.querySelector('select');
    const label = document.getElementById(select.getAttribute('aria-labelledby'));
    assert.ok(label);
    assert.equal(label.textContent, 'New status');
    assert.equal(label.contains(select), false);
    await act(async () => {
      select.value = 'unpublish';
      select.dispatchEvent(new dom.window.Event('change', { bubbles: true }));
    });
    assert.equal(select.value, 'unpublish');
    assert.equal(label.textContent, 'New status');
  } finally {
    await m.cleanup();
  }
});

test('Bulk actions require confirmation and send exact selected versions/revisions with CSRF', async () => {
  const old = global.fetch;
  const requests = [];
  global.fetch = async (url, init) => {
    requests.push({ url, init });
    return response({ changed: 1, unchanged: 0 });
  };
  const m = await mount([entry('one'), entry('two')]);
  try {
    await act(async () => m.el.querySelector('input[type="checkbox"]').click());
    await act(async () => m.el.querySelector('[data-key="one"]').click());
    await act(async () => button(m.el, 'bulk.apply').click());
    assert.equal(requests.length, 0);
    assert.ok(
      m.el
        .querySelector('[aria-label="' + text('bulk.confirm') + '"] details')
        .textContent.includes('two'),
    );
    assert.ok(
      !m.el
        .querySelector('[aria-label="' + text('bulk.confirm') + '"] details')
        .textContent.includes('one'),
    );
    await act(async () => button(m.el, 'config.cancel').click());
    assert.equal(requests.length, 0);
    await act(async () => button(m.el, 'bulk.apply').click());
    await act(async () => button(m.el, 'bulk.confirmApply').click());
    assert.equal(requests[0].url, '/api/v1/content/bulk-publication');
    assert.equal(requests[0].init.headers['X-CSRF-Token'], 'owner-csrf');
    assert.deepEqual(JSON.parse(requests[0].init.body), {
      operation: 'publish',
      items: [{ key: 'two', locale: 'en', expected_version: 3, revision_id: 'a'.repeat(32) }],
    });
    assert.equal(m.applied(), 1);
    assert.equal(button(m.el, 'bulk.apply').disabled, true);
  } finally {
    await m.cleanup();
    global.fetch = old;
  }
});

test('Select all follows every filtered page from the beginning and does not publish during selection', async () => {
  const old = global.fetch;
  const requests = [];
  global.fetch = async (url, init) => {
    requests.push({ url, init });
    const q = new URL(url, 'http://localhost').searchParams;
    assert.equal(q.get('q'), 'notion');
    assert.equal(q.get('kind'), 'article');
    assert.equal(q.get('locale'), 'en');
    return response(
      q.has('cursor')
        ? { items: [entry('two')], next_cursor: '' }
        : { items: [entry('one')], next_cursor: 'one' },
    );
  };
  const m = await mount([entry('two')], 'locale=en&kind=article&q=notion&cursor=old');
  try {
    await act(async () => button(m.el, 'bulk.selectAll').click());
    assert.equal(requests.length, 2);
    assert.ok(requests.every((r) => r.init.method === 'GET'));
    assert.equal(new URL(requests[0].url, 'http://localhost').searchParams.has('cursor'), false);
    assert.ok(m.el.textContent.includes('Selected: 2'));
    assert.equal(m.el.querySelector('[data-key="two"]').checked, true);
    await act(async () => button(m.el, 'bulk.clear').click());
    assert.ok(m.el.textContent.includes('Selected: 0'));
  } finally {
    await m.cleanup();
    global.fetch = old;
  }
});

test('Unpublish uses the live revision even when the saved draft differs and reports conflicts', async () => {
  const old = global.fetch;
  let body;
  global.fetch = async (_, init) => {
    body = JSON.parse(init.body);
    return response({ error: { code: 'revision_conflict' } }, false);
  };
  const m = await mount([
    entry('one', { published_revision_id: 'b'.repeat(32), status: 'changed' }),
  ]);
  try {
    await act(async () => m.el.querySelector('[data-key="one"]').click());
    await act(async () => {
      const select = m.el.querySelector('select');
      select.value = 'unpublish';
      select.dispatchEvent(new dom.window.Event('change', { bubbles: true }));
    });
    await act(async () => button(m.el, 'bulk.apply').click());
    await act(async () => button(m.el, 'bulk.confirmApply').click());
    assert.equal(body.operation, 'unpublish');
    assert.equal(body.items[0].revision_id, 'b'.repeat(32));
    assert.equal(m.el.querySelector('[role="alert"]').textContent, text('bulk.conflict'));
    assert.equal(m.el.querySelector('[data-key="one"]').checked, true);
    assert.equal(m.applied(), 1);
  } finally {
    await m.cleanup();
    global.fetch = old;
  }
});

test('Select all refuses an oversized result without silently selecting a partial batch', async () => {
  const old = global.fetch;
  global.fetch = async () =>
    response({ items: Array.from({ length: 501 }, (_, i) => entry('item-' + i)), next_cursor: '' });
  const m = await mount([entry('one')]);
  try {
    await act(async () => button(m.el, 'bulk.selectAll').click());
    assert.equal(m.el.querySelector('[role="alert"]').textContent, text('bulk.limit'));
    assert.equal(button(m.el, 'bulk.apply').disabled, true);
    assert.equal(m.el.querySelector('[data-key="one"]').checked, false);
  } finally {
    await m.cleanup();
    global.fetch = old;
  }
});
