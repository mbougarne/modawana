const { test } = require('node:test');
const assert = require('node:assert/strict');
const { JSDOM } = require('jsdom');
const dom = new JSDOM('<!doctype html><html><body></body></html>', {
  url: 'http://localhost/en/admin/posts',
});
global.window = dom.window;
global.document = dom.window.document;
global.HTMLElement = dom.window.HTMLElement;
global.IS_REACT_ACT_ENVIRONMENT = true;
const React = require('react');
const { act } = React;
const { createRoot } = require('react-dom/client');
const { OwnerWorkspace } = require('../.test-output/frontend/src/features/owner/OwnerWorkspace');
const { I18nProvider } = require('../.test-output/ui/src/i18n/I18nProvider');
const { ThemeProvider } = require('../.test-output/ui/src/theme/ThemeProvider');
const { DEFAULT_LABELS } = require('../.test-output/ui/src/i18n/labels');
const pages = [
  'posts',
  'collections',
  'taxonomy',
  'media',
  'settings',
  'profile',
  'labels',
  'security',
  'transfer',
];
async function mount(current = 'posts', locale = 'en', extra = {}) {
  const el = document.createElement('div');
  document.body.append(el);
  const root = createRoot(el);
  await act(async () =>
    root.render(
      React.createElement(
        ThemeProvider,
        { initial: 'light' },
        React.createElement(
          I18nProvider,
          { locale },
          React.createElement(
            OwnerWorkspace,
            { current, csrf: 'test-csrf', languagePath: '/admin/' + current, ...extra },
            React.createElement('h1', {}, current),
          ),
        ),
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

test('Every owner page has the same navigation and tools, with one active destination and sign-out last', async () => {
  for (const current of pages) {
    const m = await mount(current);
    try {
      assert.deepEqual(
        [...m.el.querySelectorAll('.owner-navigation a')].map((a) => a.pathname),
        pages.map((p) => '/en/admin/' + p),
      );
      assert.equal(m.el.querySelectorAll('nav').length, 1);
      assert.equal(m.el.querySelectorAll('.owner-navigation [aria-current="page"]').length, 1);
      assert.equal(
        m.el.querySelector('.owner-navigation [aria-current="page"]').pathname,
        '/en/admin/' + current,
      );
      assert.equal(m.el.querySelectorAll('.owner-navigation svg').length, 0);
      assert.equal(
        m.el.querySelector('.workspace-actions').lastElementChild.textContent,
        DEFAULT_LABELS['auth.signout'].en,
      );
      assert.equal(m.el.querySelector('.workspace-actions a').pathname, '/en');
      assert.equal(m.el.querySelector('.vb-skip').hash, '#' + m.el.querySelector('main').id);
    } finally {
      await m.cleanup();
    }
  }
});

test('Arabic workspace language menu preserves the editor content locale, focuses a link and returns focus on Escape', async () => {
  window.history.replaceState({}, '', '/ar/admin/editor/example/fr?source=en');
  const m = await mount('posts', 'ar', { languagePath: '/admin/editor/example/en' });
  try {
    assert.equal(m.el.querySelector('.owner-workspace').dir, 'rtl');
    const trigger = m.el.querySelector('.config-language button');
    assert.equal(trigger.textContent, 'ع');
    await act(async () => trigger.click());
    const menu = m.el.querySelector('#config-language-menu');
    assert.deepEqual(
      [...menu.querySelectorAll('a')].map((a) => a.pathname),
      ['/en/admin/editor/example/en', '/fr/admin/editor/example/en', '/ar/admin/editor/example/en'],
    );
    assert.deepEqual(
      [...menu.querySelectorAll('a')].map((a) => a.search),
      ['?source=en', '?source=en', '?source=en'],
    );
    assert.equal(menu.querySelector('a'), document.activeElement);
    await act(async () =>
      document.dispatchEvent(
        new dom.window.KeyboardEvent('keydown', { key: 'Escape', bubbles: true }),
      ),
    );
    assert.equal(m.el.querySelector('#config-language-menu'), null);
    assert.equal(document.activeElement, trigger);
    await act(async () => trigger.click());
    await act(async () =>
      document.body.dispatchEvent(new dom.window.Event('pointerdown', { bubbles: true })),
    );
    assert.equal(trigger.getAttribute('aria-expanded'), 'false');
    await act(async () =>
      m.el.querySelector('[aria-label="' + DEFAULT_LABELS['nav.theme'].ar + '"]').click(),
    );
    assert.equal(document.documentElement.dataset.theme, 'dark');
  } finally {
    await m.cleanup();
    window.history.replaceState({}, '', '/en/admin/posts');
  }
});

test('Workspace invokes the supplied security sign-out and reports failures without navigating or exposing details', async () => {
  let calls = 0;
  const m = await mount('security', 'fr', {
    onSignOut: async () => {
      calls++;
      throw new Error('private failure details');
    },
  });
  try {
    await act(async () => m.el.querySelector('.workspace-actions').lastElementChild.click());
    assert.equal(calls, 1);
    assert.equal(
      m.el.querySelector('[role="alert"]').textContent,
      DEFAULT_LABELS['auth.network'].fr,
    );
    assert.ok(!m.el.textContent.includes('private failure details'));
  } finally {
    await m.cleanup();
  }
  const disabled = await mount('security', 'en', { csrf: undefined });
  try {
    assert.equal(disabled.el.querySelector('.workspace-actions').lastElementChild.disabled, true);
  } finally {
    await disabled.cleanup();
  }
});
