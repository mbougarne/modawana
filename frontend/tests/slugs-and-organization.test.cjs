const { test } = require('node:test');
const assert = require('node:assert/strict');
const path = require('node:path');
const Module = require('node:module');
const original = Module._resolveFilename;
Module._resolveFilename = function (request, ...args) {
  return original.call(
    this,
    request === '@ui'
      ? path.resolve(__dirname, '../.test-output/ui/src/index.js')
      : request.startsWith('@ui/')
        ? path.resolve(__dirname, '../.test-output/ui/src', request.slice(4))
        : request,
    ...args,
  );
};
const { JSDOM } = require('jsdom');
const dom = new JSDOM('<!doctype html><html><body></body></html>', {
  url: 'http://localhost/en/admin/collections',
});
global.window = dom.window;
global.document = dom.window.document;
global.HTMLElement = dom.window.HTMLElement;
global.IS_REACT_ACT_ENVIRONMENT = true;
const React = require('react');
const { act } = React;
const { createRoot } = require('react-dom/client');
const { Organization } = require('../.test-output/frontend/src/features/owner/Organization');
const { I18nProvider } = require('../.test-output/ui/src/i18n/I18nProvider');
const { DEFAULT_LABELS } = require('../.test-output/ui/src/i18n/labels');
const {
  slugFromTitle,
  titleSlugChange,
} = require('../.test-output/frontend/src/features/content/slug');
const text = (key) => DEFAULT_LABELS[key].en;
async function fill(el, value) {
  await act(async () => {
    Object.getOwnPropertyDescriptor(dom.window.HTMLInputElement.prototype, 'value').set.call(
      el,
      value,
    );
    el.dispatchEvent(new dom.window.Event('input', { bubbles: true }));
  });
}
async function click(el) {
  await act(async () => el.dispatchEvent(new dom.window.MouseEvent('click', { bubbles: true })));
}
async function mount(page = 'collections') {
  const el = document.createElement('div');
  document.body.append(el);
  const root = createRoot(el);
  await act(async () =>
    root.render(
      React.createElement(
        I18nProvider,
        { locale: 'en' },
        React.createElement(Organization, { page, csrf: 'csrf' }),
      ),
    ),
  );
  const field = (key) =>
    document.getElementById(
      [...el.querySelectorAll('label')].find((n) => n.textContent === text(key))?.htmlFor,
    );
  const button = (key) =>
    [...el.querySelectorAll('button')].find((n) => n.textContent === text(key));
  return {
    el,
    field,
    button,
    close: async () => {
      await act(async () => root.unmount());
      el.remove();
    },
  };
}
test('Slugs preserve Arabic, fold French accents and never overwrite manually edited or locked URLs', () => {
  assert.equal(slugFromTitle('  Café & Go: début ! '), 'cafe-go-debut');
  assert.equal(slugFromTitle('تعلّم ++C بسهولة'), 'تعلم-c-بسهولة');
  assert.equal(Array.from(slugFromTitle('ع'.repeat(200))).length, 160);
  assert.equal(titleSlugChange('Old', 'old', 'New', false), 'new');
  assert.equal(titleSlugChange('Old', 'custom', 'New', false), 'custom');
  assert.equal(titleSlugChange('Old', 'old', 'New', true), 'old');
  assert.equal(titleSlugChange('Old', 'old', 'New', false, true), 'old');
});
test('Collection title generates an editable slug; New clears the previous success notice and saved keys stay fixed', async () => {
  let saved;
  global.fetch = async (url, options = {}) => ({
    ok: true,
    json: async () => {
      if (options.method === 'PUT') {
        saved = JSON.parse(options.body);
        return { version: 3, plan_id: 'plan' };
      }
      if (url.includes('?')) {
        return {
          items: saved ? [{ key: 'opaque-key', kind: 'learning', title: saved.title }] : [],
        };
      }
      return {
        key: 'opaque-key',
        kind: 'learning',
        version: 3,
        draft_plan_id: 'plan',
        active_plan_id: 'plan',
        draft_members: [],
        assigned_members: [],
        member_titles: {},
        locales: [{ locale: 'en', ...saved }],
      };
    },
  });
  const m = await mount();
  await fill(m.field('owner.key'), 'opaque-key');
  await fill(m.field('owner.title'), 'Café & Go');
  assert.equal(m.field('owner.slug').value, 'cafe-go');
  await fill(m.field('owner.slug'), 'manual-address');
  await fill(m.field('owner.title'), 'Readable collection title');
  assert.equal(m.field('owner.slug').value, 'manual-address');
  await click(m.button('collection.save'));
  assert.equal(m.el.querySelector('[role="status"]').textContent, text('collection.saved'));
  assert.match(m.el.querySelector('.organization-list').textContent, /Readable collection title/);
  assert.equal(m.field('owner.key').disabled, true);
  await click(m.button('owner.new'));
  assert.equal(m.el.querySelector('[role="status"]'), null);
  assert.equal(m.field('owner.title').value, '');
  assert.equal(m.field('owner.slug').value, '');
  await fill(m.field('owner.title'), 'Another collection');
  assert.equal(m.field('owner.slug').value, 'another-collection');
  await m.close();
});
test('Category and tag labels generate editable slugs too', async () => {
  global.fetch = async () => ({ ok: true, json: async () => ({ items: [] }) });
  const m = await mount('taxonomy');
  await fill(m.field('owner.label'), 'React & Go');
  assert.equal(m.field('owner.slug').value, 'react-go');
  await fill(m.field('owner.slug'), 'custom-tag');
  await fill(m.field('owner.label'), 'New label');
  assert.equal(m.field('owner.slug').value, 'custom-tag');
  await m.close();
});

test('A collection slug error explains that exact problem without unrelated quiz advice', async () => {
  global.fetch = async (_url, options = {}) =>
    options.method === 'PUT'
      ? {
          ok: false,
          status: 422,
          json: async () => ({
            error: { code: 'invalid_fields', fields: [{ path: 'slug', code: 'collection_slug' }] },
          }),
        }
      : { ok: true, json: async () => ({ items: [] }) };
  const m = await mount();
  await fill(m.field('owner.key'), 'new-key');
  await fill(m.field('owner.title'), 'New collection');
  await fill(m.field('owner.slug'), 'bad slug');
  await click(m.button('collection.save'));
  assert.equal(
    m.el.querySelector('[role="status"]').textContent,
    text('conversion.collection_slug'),
  );
  assert.ok(!m.el.querySelector('[role="status"]').textContent.includes('quiz'));
  await m.close();
});
