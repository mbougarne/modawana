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
const {
  CollectionContents,
} = require('../.test-output/frontend/src/features/owner/CollectionContents');
const { I18nProvider } = require('../.test-output/ui/src/i18n/I18nProvider');
const { DEFAULT_LABELS } = require('../.test-output/ui/src/i18n/labels');
const text = (key, locale = 'en') => DEFAULT_LABELS[key][locale];
async function click(el) {
  await act(async () => el.dispatchEvent(new dom.window.MouseEvent('click', { bubbles: true })));
}
async function tick() {
  await act(async () => new Promise((resolve) => setTimeout(resolve, 180)));
}
async function mount(initial = [], locale = 'en') {
  const el = document.createElement('div');
  document.body.append(el);
  const root = createRoot(el);
  let current = initial;
  function Harness() {
    const [members, setMembers] = React.useState(initial);
    return React.createElement(CollectionContents, {
      members,
      onChange: (m) => {
        current = m;
        setMembers(m);
      },
      kind: 'learning',
      locale,
      collectionKey: 'course',
    });
  }
  await act(async () =>
    root.render(React.createElement(I18nProvider, { locale }, React.createElement(Harness))),
  );
  return {
    el,
    current: () => current,
    close: async () => {
      await act(async () => root.unmount());
      el.remove();
    },
  };
}
test('Collection content picker adds a second and third article, excluding selections before the next search', async () => {
  const urls = [];
  const items = [
    { key: 'one', title: 'First article', kind: 'article', version: 2 },
    { key: 'two', title: 'Second article', kind: 'article', version: 3 },
    { key: 'three', title: 'Third lesson', kind: 'lesson', version: 4 },
  ];
  global.fetch = async (url) => {
    urls.push(url);
    const p = new URL(url, 'http://test').searchParams;
    assert.equal(p.get('kind'), 'learning_item');
    assert.equal(p.get('collection_key'), 'course');
    return {
      ok: true,
      json: async () => ({
        items: items.filter((i) => !p.getAll('exclude_key').includes(i.key)),
        more: false,
      }),
    };
  };
  const m = await mount();
  for (let index = 0; index < 3; index++) {
    const input = m.el.querySelector('[role="combobox"]');
    await act(async () => {
      input.blur();
      input.focus();
    });
    await tick();
    assert.equal(m.el.querySelectorAll('[role="option"]').length, 3 - index);
    await click(m.el.querySelector('[role="option"]'));
    const add = [...m.el.querySelectorAll('button')].find(
      (b) => b.textContent === text('owner.add'),
    );
    assert.equal(add.disabled, false);
    await click(add);
  }
  assert.deepEqual(
    m.current().map((i) => i.key),
    ['one', 'two', 'three'],
  );
  assert.deepEqual(
    m.current().map((i) => i.expected_version),
    [2, 3, 4],
  );
  assert.equal(m.el.querySelectorAll('ol > li').length, 3);
  assert.match(m.el.textContent, /Second article/);
  assert.ok(
    urls.some((url) => new URL(url, 'http://test').searchParams.getAll('exclude_key').length === 2),
  );
  await m.close();
});
test('Collection reorder preserves optional announcements; optional fields start collapsed in Arabic', async () => {
  const initial = [
    { key: 'one', title: 'الأول', announcements: { ar: 'قريباً' } },
    { key: 'two', title: 'الثاني', announcements: {} },
  ];
  const m = await mount(initial, 'ar');
  assert.equal(m.el.querySelector('details').open, false);
  const down = [...m.el.querySelectorAll('button')].find(
    (b) => b.textContent === text('owner.down', 'ar'),
  );
  await click(down);
  assert.deepEqual(
    m.current().map((i) => i.key),
    ['two', 'one'],
  );
  assert.equal(m.current()[1].announcements.ar, 'قريباً');
  const add = [...m.el.querySelectorAll('button')].find(
    (b) => b.textContent === text('owner.add', 'ar'),
  );
  assert.equal(add.disabled, true);
  await m.close();
});
