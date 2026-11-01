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
const { LabelsForm } = require('../.test-output/frontend/src/pages/admin/labels/LabelsForm');
const { I18nProvider } = require('../.test-output/ui/src/i18n/I18nProvider');
const { DEFAULT_LABELS } = require('../.test-output/ui/src/i18n/labels');
const { resolveLabel } = require('../.test-output/ui/src/i18n/resolve');
const { Logo } = require('../.test-output/ui/src/components/layout');
const { renderToStaticMarkup } = require('react-dom/server');

async function mount(initial = {}, locale = 'en', onImport = async () => {}) {
  const el = document.createElement('div');
  document.body.append(el);
  const root = createRoot(el);
  let latest = initial;
  function Harness() {
    const [value, setValue] = React.useState(initial);
    return React.createElement(
      I18nProvider,
      { locale },
      React.createElement(LabelsForm, {
        value,
        saved: initial,
        onChange(next) {
          latest = next;
          setValue(next);
        },
        onImport,
      }),
    );
  }
  await act(async () => root.render(React.createElement(Harness)));
  return {
    el,
    value: () => latest,
    cleanup: async () => {
      await act(async () => root.unmount());
      el.remove();
    },
  };
}
async function input(node, value) {
  assert.ok(node);
  const setter = Object.getOwnPropertyDescriptor(
    dom.window.HTMLInputElement.prototype,
    'value',
  ).set;
  await act(async () => {
    setter.call(node, value);
    node.dispatchEvent(new dom.window.Event('input', { bubbles: true }));
  });
}
const byLabel = (el, label) =>
  [...el.querySelectorAll('input')].find((n) => n.getAttribute('aria-label') === label);
const button = (el, label) =>
  [...el.querySelectorAll('button')].find((n) => n.textContent === label);

test('Labels shows translation columns, explicit English fallback, edited markers and locale reset', async () => {
  const initial = { 'nav.articles': { en: 'Notes', fr: '', ar: '' } };
  const m = await mount(initial);
  try {
    await input(m.el.querySelector('input[type="search"]'), 'nav.articles');
    assert.equal(m.el.querySelectorAll('[role="columnheader"]').length, 4);
    assert.equal(m.el.querySelectorAll('[role="row"]:not(.vb-lhead)').length, 1);
    const french = byLabel(m.el, 'nav.articles · Français');
    assert.equal(french.dataset.missing, 'true');
    assert.equal(french.placeholder, 'Notes');
    assert.equal(byLabel(m.el, 'nav.articles · العربية').dir, 'rtl');
    await input(byLabel(m.el, 'nav.articles · English'), 'Journal');
    assert.equal(m.value()['nav.articles'].en, 'Journal');
    assert.equal(byLabel(m.el, 'nav.articles · English').dataset.changed, 'true');
    assert.equal(french.placeholder, 'Journal');
    await act(async () =>
      button(french.closest('[role="cell"]'), DEFAULT_LABELS['labels.reset'].en).click(),
    );
    assert.equal(byLabel(m.el, 'nav.articles · Français').value, DEFAULT_LABELS['nav.articles'].fr);
    assert.equal(byLabel(m.el, 'nav.articles · Français').dataset.missing, 'false');
    const fallback = french.closest('[role="cell"]').querySelector('input[type="checkbox"]');
    await act(async () => fallback.click());
    assert.equal(m.value()['nav.articles'].fr, '');
    assert.equal(resolveLabel('nav.articles', 'fr', m.value()).text, 'Journal');
    await input(byLabel(m.el, 'nav.articles · English'), 'Essays');
    assert.equal(resolveLabel('nav.articles', 'fr', m.value()).text, 'Essays');
    await act(async () => fallback.click());
    assert.equal(
      resolveLabel('nav.articles', 'fr', m.value()).text,
      DEFAULT_LABELS['nav.articles'].fr,
    );
    assert.equal(initial['nav.articles'].en, 'Notes');
  } finally {
    await m.cleanup();
  }
});

test('Labels preserves six Arabic plural forms and filters all keys across pages', async () => {
  const initial = {
    'custom.total': { en: { one: 'One', other: '{n} items' }, ar: { other: '{n} عنصر' } },
  };
  const m = await mount(initial);
  try {
    assert.equal(m.el.querySelectorAll('[role="row"]:not(.vb-lhead)').length, 30);
    await act(async () => button(m.el, DEFAULT_LABELS['labels.next'].en).click());
    assert.match(m.el.querySelector('.labels-pagination').textContent, /Page 2/);
    await input(m.el.querySelector('input[type="search"]'), 'custom.total');
    const cell = m.el.querySelector('[role="cell"][lang="ar"]');
    assert.equal(cell.querySelectorAll('.labels-plural input').length, 6);
    await input(cell.querySelectorAll('.labels-plural input')[5], '{n} عناصر');
    assert.equal(m.value()['custom.total'].ar.other, '{n} عناصر');
    assert.equal(initial['custom.total'].ar.other, '{n} عنصر');
    await input(m.el.querySelector('input[type="search"]'), 'there-is-no-such-label');
    assert.ok(m.el.querySelector('.vb-empty'));
    assert.equal(m.el.querySelector('[role="table"]'), null);
  } finally {
    await m.cleanup();
  }
});

test('Logo is a plain home link for wordmarks and uploaded images', () => {
  for (const logo of [
    { kind: 'wordmark', text: 'owner.b' },
    { kind: 'image', url: '/logo.png', alt: 'Owner' },
  ]) {
    const html = renderToStaticMarkup(React.createElement(Logo, { brand: { logo }, href: '/en' }));
    const doc = new JSDOM(html).window.document;
    assert.equal(doc.querySelectorAll('a').length, 1);
    assert.equal(doc.querySelector('a').getAttribute('href'), '/en');
    assert.equal(doc.querySelectorAll('button, svg, [aria-expanded]').length, 0);
  }
});

test('Labels preserves existing multiline translations instead of flattening their content', async () => {
  const initial = { 'custom.multiline': { en: 'First line\nSecond line' } };
  const m = await mount(initial);
  try {
    await input(m.el.querySelector('input[type="search"]'), 'custom.multiline');
    const field = m.el.querySelector('textarea[lang="en"]');
    assert.ok(field);
    assert.equal(field.value, 'First line\nSecond line');
    assert.equal(m.value()['custom.multiline'].en, initial['custom.multiline'].en);
  } finally {
    await m.cleanup();
  }
});

async function chooseJSON(el, file, drop = false) {
  await act(async () => {
    if (drop) {
      const event = new dom.window.Event('drop', { bubbles: true, cancelable: true });
      Object.defineProperty(event, 'dataTransfer', { value: { files: [file] } });
      el.querySelector('.labels-transfer .vb-file-drop').dispatchEvent(event);
    } else {
      const field = el.querySelector('input[type="file"]');
      Object.defineProperty(field, 'files', { configurable: true, value: [file] });
      field.dispatchEvent(new dom.window.Event('change', { bubbles: true }));
    }
  });
}

test('Labels imports a JSON override map by picker or drop and rejects malformed or oversized files', async () => {
  const imported = [];
  const bundle = { 'nav.articles': { en: 'Notes', fr: '', ar: 'ملاحظات' } };
  const m = await mount({}, 'en', async (v) => {
    imported.push(v);
  });
  try {
    const file = { size: 100, text: async () => JSON.stringify(bundle) };
    await chooseJSON(m.el, file);
    await chooseJSON(m.el, file, true);
    assert.deepEqual(imported, [bundle, bundle]);
    assert.deepEqual(m.value(), {});
    await chooseJSON(m.el, { size: 10, text: async () => '{invalid' });
    assert.ok(m.el.querySelector('[role="alert"]'));
    await chooseJSON(m.el, {
      size: 262145,
      text: async () => {
        throw new Error('must not read oversized file');
      },
    });
    assert.equal(imported.length, 2);
    assert.ok(m.el.querySelector('[role="alert"]'));
  } finally {
    await m.cleanup();
  }
});

test('Labels exports the current unsaved override map in the same JSON format accepted by import', async () => {
  const initial = { 'nav.articles': { en: 'Notes', fr: '' } };
  const m = await mount(initial);
  const create = URL.createObjectURL;
  const revoke = URL.revokeObjectURL;
  const click = dom.window.HTMLAnchorElement.prototype.click;
  let blob;
  let filename;
  URL.createObjectURL = (value) => {
    blob = value;
    return 'blob:labels';
  };
  URL.revokeObjectURL = () => {};
  dom.window.HTMLAnchorElement.prototype.click = function () {
    filename = this.download;
  };
  try {
    await input(m.el.querySelector('input[type="search"]'), 'nav.articles');
    await input(byLabel(m.el, 'nav.articles · English'), 'Journal');
    await act(async () => button(m.el, DEFAULT_LABELS['labels.export'].en).click());
    assert.equal(filename, 'interface-labels.json');
    assert.deepEqual(JSON.parse(await blob.text()), { 'nav.articles': { en: 'Journal', fr: '' } });
    assert.equal(initial['nav.articles'].en, 'Notes');
  } finally {
    URL.createObjectURL = create;
    URL.revokeObjectURL = revoke;
    dom.window.HTMLAnchorElement.prototype.click = click;
    await m.cleanup();
  }
});
