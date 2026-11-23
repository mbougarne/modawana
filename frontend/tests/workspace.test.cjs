const { test } = require('node:test');
const assert = require('node:assert/strict');
const { JSDOM } = require('jsdom');
const dom = new JSDOM('<!doctype html><html><body></body></html>', {
  url: 'http://localhost/en/admin/posts',
});
global.window = dom.window;
global.document = dom.window.document;
global.HTMLElement = dom.window.HTMLElement;
global.localStorage = dom.window.localStorage;
global.IS_REACT_ACT_ENVIRONMENT = true;
const React = require('react');
const { act } = React;
const { createRoot } = require('react-dom/client');
const { createMemoryRouter, RouterProvider } = require('react-router-dom');
const { OwnerWorkspace } = require('../.test-output/frontend/src/features/owner/OwnerWorkspace');
const {
  OwnerAppearanceSettings,
} = require('../.test-output/frontend/src/features/owner/OwnerAppearance');
const {
  useOwnerLeaveGuard,
} = require('../.test-output/frontend/src/features/owner/useOwnerLeaveGuard');
const { I18nProvider } = require('../.test-output/ui/src/i18n/I18nProvider');
const { ThemeProvider } = require('../.test-output/ui/src/theme/ThemeProvider');
const { DEFAULT_LABELS } = require('../.test-output/ui/src/i18n/labels');
const pages = [
  'dashboard',
  'posts',
  'pages',
  'collections',
  'taxonomy',
  'media',
  'settings',
  'profile',
  'labels',
  'security',
  'transfer',
  'plugins',
  'layout',
];
async function mount(current = 'posts', locale = 'en', extra = {}, children) {
  const el = document.createElement('div');
  document.body.append(el);
  const root = createRoot(el);
  const router = createMemoryRouter(
    [
      {
        path: '*',
        element: React.createElement(
          OwnerWorkspace,
          { current, csrf: 'test-csrf', languagePath: '/admin/' + current, ...extra },
          children ?? React.createElement('h1', {}, current),
        ),
      },
    ],
    { initialEntries: [window.location.pathname + window.location.search] },
  );
  await act(async () =>
    root.render(
      React.createElement(
        ThemeProvider,
        { initial: 'light' },
        React.createElement(
          I18nProvider,
          { locale },
          React.createElement(RouterProvider, { router }),
        ),
      ),
    ),
  );
  return {
    el,
    router,
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

test('Personal Arabic layout groups and renames registered links while retaining protected controls', async () => {
  const fetch = global.fetch;
  global.fetch = async (url) => {
    if (url === '/api/v1/dashboard-layout') {
      return Response.json({
        site: { version: 1, layout: { appearance: { theme: 'classic' } } },
        personal: {
          version: 1,
          layout: {
            appearance: { theme: 'slate', density: 'compact' },
            navigation: [
              { id: 'media', rename: { ar: 'الملفات' }, group: 'content' },
              { id: 'extension:example.notes', group: 'content' },
              { id: 'posts', hidden: true },
              { id: 'extension:missing.notes', rename: { en: 'Unavailable' } },
            ],
          },
        },
        registry: { navigation: [], widgets: [] },
      });
    }
    if (url === '/api/v1/extension-contributions') {
      return Response.json({
        items: [
          {
            kind: 'admin_navigation',
            extension: 'example',
            key: 'notes',
            version: 1,
            label: { en: 'Notes', ar: 'الملاحظات' },
            target: 'overview',
          },
        ],
      });
    }
    return new Response('', { status: 404 });
  };
  const m = await mount('media', 'ar');
  try {
    const workspace = m.el.querySelector('.owner-workspace');
    assert.equal(workspace.dir, 'rtl');
    assert.equal(workspace.dataset.adminTheme, 'slate');
    assert.equal(workspace.dataset.dashboardDensity, 'compact');
    const nav = workspace.querySelector('.owner-navigation');
    assert.equal(nav.querySelector('[href="/ar/admin/posts"]'), null);
    assert.ok(!nav.textContent.includes('Unavailable'));
    assert.ok(nav.querySelector('[href="/ar/admin/profile"]'));
    assert.ok(nav.querySelector('[href="/ar/admin/security"]'));
    assert.ok(nav.querySelector('[href="/ar/admin/layout"]'));
    const links = [...nav.querySelectorAll('[role="group"] a')];
    assert.deepEqual(
      links.map((link) => link.textContent),
      ['الملفات', 'الملاحظات'],
    );
    links[1].focus();
    assert.equal(document.activeElement, links[1]);
    assert.equal(links[1].hash, '#extension-example');
  } finally {
    await m.cleanup();
    global.fetch = fetch;
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

test('The admin theme switches in place, persists between pages and stays independent of color mode', async () => {
  const key = 'modawana.adminTheme';
  localStorage.setItem(key, 'unknown');
  const settings = await mount('settings', 'en', {}, React.createElement(OwnerAppearanceSettings));
  const workspace = settings.el.querySelector('.owner-workspace');
  const select = settings.el.querySelector('.owner-appearance select');
  const choose = async (choice) => {
    await act(async () => {
      select.value = choice;
      select.dispatchEvent(new dom.window.Event('change', { bubbles: true }));
    });
  };
  try {
    assert.equal(workspace.dataset.adminTheme, 'classic');
    await choose('slate');
    assert.equal(workspace.dataset.adminTheme, 'slate');
    assert.equal(localStorage.getItem(key), 'slate');
    assert.equal(document.documentElement.dataset.theme, 'light');
    assert.equal(
      workspace.querySelector('.owner-navigation').querySelectorAll('svg').length,
      pages.length + 2,
    );
    assert.deepEqual(
      [...workspace.querySelectorAll('.workspace-quick-links a')].map((link) => link.pathname),
      ['/en/admin/new', '/en'],
    );
    assert.equal(workspace.querySelector('.workspace-rail-brand').pathname, '/en/admin/posts');
    localStorage.setItem(key, 'sidebar');
    const posts = await mount('posts', 'ar');
    try {
      const sidebar = posts.el.querySelector('.owner-workspace');
      assert.equal(sidebar.dataset.adminTheme, 'slate');
      assert.equal(sidebar.dir, 'rtl');
      const settingsIndex = pages.indexOf('settings');
      assert.deepEqual(
        [...sidebar.querySelectorAll('.owner-navigation a')].map((a) => a.pathname),
        [...pages.slice(0, settingsIndex), 'themes', 'menus', ...pages.slice(settingsIndex)].map(
          (page) => '/ar/admin/' + page,
        ),
      );
      assert.equal(
        sidebar.querySelector('.owner-navigation [aria-current="page"]').pathname,
        '/ar/admin/posts',
      );
    } finally {
      await posts.cleanup();
    }
    await choose('classic');
    assert.equal(workspace.dataset.adminTheme, 'classic');
    assert.equal(localStorage.getItem(key), 'classic');
  } finally {
    await settings.cleanup();
    localStorage.removeItem(key);
  }
});

test('Admin links use the client router and give unsaved changes a chance to stay', async () => {
  function UnsavedSettings() {
    useOwnerLeaveGuard(true);
    return React.createElement('input', { defaultValue: 'Unsaved setting' });
  }
  window.history.replaceState({}, '', '/en/admin/settings');
  localStorage.setItem('modawana.adminTheme', 'slate');
  const m = await mount('settings', 'en', {}, React.createElement(UnsavedSettings));
  const previousConfirm = window.confirm;
  let confirmed = false;
  let prompts = 0;
  window.confirm = (message) => {
    assert.equal(message, DEFAULT_LABELS['owner.confirmLeave'].en);
    prompts++;
    return confirmed;
  };
  const newContent = m.el.querySelector('.workspace-quick-links a[href="/en/admin/new"]');
  const posts = m.el.querySelector('.owner-navigation a[href="/en/admin/posts"]');
  try {
    await act(async () => newContent.click());
    assert.equal(prompts, 1);
    assert.equal(m.router.state.location.pathname, '/en/admin/settings');
    assert.equal(m.el.querySelector('input').value, 'Unsaved setting');

    confirmed = true;
    await act(async () => posts.click());
    assert.equal(prompts, 2);
    assert.equal(m.router.state.location.pathname, '/en/admin/posts');
    assert.equal(window.location.pathname, '/en/admin/settings');
  } finally {
    window.confirm = previousConfirm;
    await m.cleanup();
    localStorage.removeItem('modawana.adminTheme');
    window.history.replaceState({}, '', '/en/admin/posts');
  }
});
