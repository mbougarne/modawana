const { test } = require('node:test');
const assert = require('node:assert/strict');
const { JSDOM } = require('jsdom');
const dom = new JSDOM('<!doctype html><html><body></body></html>');
global.window = dom.window;
global.document = dom.window.document;
global.HTMLElement = dom.window.HTMLElement;
global.IS_REACT_ACT_ENVIRONMENT = true;
const React = require('react');
const { act } = React;
const { createRoot } = require('react-dom/client');
const { ReferencePicker } = require('../.test-output/frontend/src/features/owner/ReferencePicker');
const { I18nProvider } = require('../.test-output/ui/src/i18n/I18nProvider');
const { DEFAULT_LABELS } = require('../.test-output/ui/src/i18n/labels');
const tick = () =>
  act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 180));
  });
const entry = (key) => ({ key, title: 'Title ' + key, kind: 'learning', version: 7 });
const response = (items, more = false) => ({ ok: true, json: async () => ({ items, more }) });
async function mount(props = {}, locale = 'en') {
  const el = document.createElement('div');
  document.body.append(el);
  const root = createRoot(el);
  let chosen;
  function Harness() {
    const [value, setValue] = React.useState('');
    return React.createElement(ReferencePicker, {
      label: 'Collection',
      value,
      kind: 'learning',
      locale,
      onChange: (key, choice) => {
        setValue(key);
        chosen = choice;
      },
      ...props,
    });
  }
  await act(async () =>
    root.render(React.createElement(I18nProvider, { locale }, React.createElement(Harness))),
  );
  const input = el.querySelector('input');
  await act(async () => input.focus());
  return {
    el,
    input,
    chosen: () => chosen,
    close: async () => {
      await act(async () => root.unmount());
      el.remove();
    },
  };
}
async function fill(input, value) {
  await act(async () => {
    Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value').set.call(
      input,
      value,
    );
    input.dispatchEvent(new window.Event('input', { bubbles: true }));
  });
}
async function key(input, value) {
  await act(async () =>
    input.dispatchEvent(
      new window.KeyboardEvent('keydown', { key: value, bubbles: true, cancelable: true }),
    ),
  );
}

test('Reference picker shows five suggestions and stores the selected key/version instead of the searched title', async () => {
  const urls = [];
  global.fetch = async (url) => {
    urls.push(url);
    return new URL(url, 'http://test').searchParams.get('q') === 'needle'
      ? response([entry('stable-key')])
      : response(
          Array.from({ length: 6 }, (_, i) => entry('key-' + i)),
          true,
        );
  };
  const m = await mount({}, 'ar');
  await tick();
  assert.equal(m.el.querySelectorAll('[role="option"]').length, 5);
  await fill(m.input, 'needle');
  assert.equal(m.el.querySelectorAll('[role="option"]').length, 0);
  await tick();
  assert.equal(m.el.querySelectorAll('[role="option"]').length, 1);
  await key(m.input, 'Enter');
  assert.equal(m.input.value, 'stable-key');
  assert.equal(m.chosen().version, 7);
  assert.equal(m.input.getAttribute('aria-expanded'), 'false');
  assert.equal(new URL(urls.at(-1), 'http://test').searchParams.get('kind'), 'learning');
  await m.close();
});
test('Out-of-order reference responses cannot replace newer search results; kind and collection filters reach the API', async () => {
  let stale;
  let oldSignal;
  global.fetch = (url, options) => {
    const params = new URL(url, 'http://test').searchParams;
    assert.equal(params.get('kind'), 'lesson');
    assert.equal(params.get('collection_key'), 'course');
    if (!params.get('q')) {
      oldSignal = options.signal;
      return new Promise((resolve) => {
        stale = resolve;
      });
    }
    return Promise.resolve(response([entry('new-result')]));
  };
  const m = await mount({ kind: 'lesson', collectionKey: 'course' });
  await tick();
  await fill(m.input, 'new');
  await tick();
  await act(async () => stale(response([entry('stale-result')])));
  assert.equal(oldSignal.aborted, true);
  assert.equal(m.el.querySelectorAll('[role="option"]').length, 1);
  assert.ok(m.el.textContent.includes('new-result'));
  assert.equal(m.el.textContent.includes('stale-result'), false);
  await key(m.input, 'Escape');
  assert.equal(m.input.getAttribute('aria-expanded'), 'false');
  await m.close();
});
test('Reference picker explains load errors, retries, empty results and supports keyboard selection', async () => {
  let failing = true;
  global.fetch = async (url) =>
    failing
      ? { ok: false, status: 503 }
      : response(
          new URL(url, 'http://test').searchParams.get('q') ? [entry('one'), entry('two')] : [],
        );
  const m = await mount();
  await tick();
  assert.equal(
    m.el.querySelector('[role="alert"]').textContent,
    DEFAULT_LABELS['request.unavailable'].en,
  );
  failing = false;
  await act(async () => m.el.querySelector('button').click());
  await tick();
  assert.ok(m.el.textContent.includes(DEFAULT_LABELS['picker.empty'].en));
  await fill(m.input, 'query');
  await tick();
  await key(m.input, 'ArrowUp');
  await key(m.input, 'Enter');
  assert.equal(m.input.value, 'two');
  await m.close();
});

test('Missing/older backend search reports restart guidance rather than a vague server error', async () => {
  for (const status of [404, 422]) {
    global.fetch = async () => ({ ok: false, status, headers: { get: () => null } });
    const m = await mount({ kind: 'learning_item', collectionKey: 'course' });
    await tick();
    assert.equal(
      m.el.querySelector('[role="alert"]').textContent,
      DEFAULT_LABELS['picker.restartBackend'].en,
    );
    await m.close();
  }
});
test('An invalid in-progress new collection key does not break eligible-content search', async () => {
  global.fetch = async (url) => {
    assert.equal(new URL(url, 'http://test').searchParams.get('collection_key'), '');
    return response([entry('article')]);
  };
  const m = await mount({ kind: 'learning_item', collectionKey: 'Still Typing' });
  await tick();
  assert.equal(m.el.querySelectorAll('[role="option"]').length, 1);
  await m.close();
});

test('A current backend validation error is not mistaken for an outdated server', async () => {
  global.fetch = async () => ({ ok: false, status: 422, headers: { get: () => '2' } });
  const m = await mount({ kind: 'learning_item', collectionKey: 'course' });
  await tick();
  assert.equal(
    m.el.querySelector('[role="alert"]').textContent,
    DEFAULT_LABELS['request.invalid'].en,
  );
  await m.close();
});
