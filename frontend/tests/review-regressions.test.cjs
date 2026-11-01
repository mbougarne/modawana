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
  url: 'http://localhost/en/admin/new',
});
global.window = dom.window;
global.document = dom.window.document;
global.HTMLElement = dom.window.HTMLElement;
global.IS_REACT_ACT_ENVIRONMENT = true;
window.matchMedia = () => ({ matches: false, addEventListener() {}, removeEventListener() {} });
const React = require('react');
const { act } = React;
const { createRoot } = require('react-dom/client');
const { MemoryRouter, Routes, Route, useNavigate } = require('react-router-dom');
const { I18nProvider } = require('../.test-output/ui/src/i18n/I18nProvider');
const { formatDate } = require('../.test-output/ui/src/i18n/locales');
const { TagInput } = require('../.test-output/ui/src/components/owner');
const { DEFAULT_LABELS } = require('../.test-output/ui/src/i18n/labels');
const { MediaLibrary } = require('../.test-output/frontend/src/features/owner/MediaLibrary');
const { EditorPage } = require('../.test-output/frontend/src/pages/admin/editor/EditorPage');
const response = (json, status = 200) => ({ ok: status < 400, status, json: async () => json });

async function mount(child) {
  const el = document.createElement('div');
  document.body.append(el);
  const root = createRoot(el);
  await act(async () => root.render(React.createElement(I18nProvider, { locale: 'en' }, child)));
  return {
    el,
    cleanup: async () => {
      await act(async () => root.unmount());
      el.remove();
    },
  };
}

test('invalid dates do not crash readers and TagInput renders without CSP-blocked inline styles', async () => {
  for (const locale of ['en', 'ar', 'fr']) {
    assert.equal(formatDate('not-a-date', locale), 'not-a-date');
    assert.ok(formatDate('2026-10-28', locale).length > 0);
  }
  const m = await mount(React.createElement(TagInput, { value: ['cpp'], onChange() {} }));
  try {
    assert.equal(m.el.querySelector('[style]'), null);
  } finally {
    await m.cleanup();
  }
});

test('busy media uploads report the second file, stop parent drops and allow the same file again', async () => {
  const previousFetch = global.fetch;
  const previousXHR = global.XMLHttpRequest;
  const requests = [];
  global.fetch = async () => response({ items: [], next_cursor: '' });
  global.XMLHttpRequest = class {
    upload = {};
    open() {}
    setRequestHeader() {}
    send(body) {
      this.body = body;
      requests.push(this);
    }
    abort() {
      this.onabort?.();
      this.onloadend?.();
    }
  };
  let parentDrops = 0;
  const file = new File(['image'], 'image.png', { type: 'image/png' });
  const m = await mount(
    React.createElement(
      'div',
      { onDrop: () => parentDrops++ },
      React.createElement(MediaLibrary, { csrf: 'test' }),
    ),
  );
  const drop = async () => {
    const e = new dom.window.Event('drop', { bubbles: true, cancelable: true });
    Object.defineProperty(e, 'dataTransfer', { value: { files: [file] } });
    await act(async () => m.el.querySelector('[role="group"]').dispatchEvent(e));
  };
  try {
    await drop();
    await drop();
    assert.equal(requests.length, 1);
    assert.equal(parentDrops, 0);
    assert.equal(
      m.el.querySelector('[role="status"]').textContent,
      DEFAULT_LABELS['owner.uploadBusy'].en,
    );
    await act(async () => requests[0].abort());
    await drop();
    assert.equal(requests.length, 2);
  } finally {
    await m.cleanup();
    global.fetch = previousFetch;
    global.XMLHttpRequest = previousXHR;
  }
});

test('late editor and translation-source responses cannot replace a newer route', async () => {
  const previousFetch = global.fetch;
  for (const translation of [false, true]) {
    let resolveOld;
    let navigate;
    const old = new Promise((resolve) => {
      resolveOld = resolve;
    });
    const draft = (title) => ({
      key: title,
      kind: 'article',
      source_locale: 'en',
      locale: 'en',
      version: 1,
      published_revision_id: 'source-revision',
      revision: {
        revision_id: 'draft-id',
        title,
        slug: title,
        summary: '',
        markdown: '',
        tag_keys: [],
        media: [],
        media_presentations: [],
      },
    });
    global.fetch = async (url) => {
      if (url === '/api/v1/content/old/fr' && translation) {
        return response({ error: { code: 'not_found' } }, 404);
      }
      if (url === '/api/v1/content/old/en') {
        return old;
      }
      if (url === '/api/v1/content/new/en') {
        return response(draft('New title'));
      }
      return response({ items: [], next_cursor: '' });
    };
    function Harness() {
      navigate = useNavigate();
      return React.createElement(
        Routes,
        {},
        React.createElement(Route, {
          path: '/:locale/admin/editor/:key/:lang',
          element: React.createElement(EditorPage, { csrf: 'test' }),
        }),
        React.createElement(Route, {
          path: '/:locale/admin/new',
          element: React.createElement(EditorPage, { csrf: 'test' }),
        }),
      );
    }
    const m = await mount(
      React.createElement(
        MemoryRouter,
        {
          initialEntries: [
            translation ? '/en/admin/editor/old/fr?source=en' : '/en/admin/editor/old/en',
          ],
        },
        React.createElement(Harness),
      ),
    );
    try {
      await act(async () => navigate(translation ? '/en/admin/new' : '/en/admin/editor/new/en'));
      await act(async () => resolveOld(response(draft('Old title'))));
      const values = [...m.el.querySelectorAll('input')].map((input) => input.value);
      assert.ok(!values.includes('Old title'));
      if (!translation) {
        assert.ok(values.includes('New title'));
      } else {
        assert.ok(!values.includes('old'));
      }
    } finally {
      await m.cleanup();
    }
  }
  global.fetch = previousFetch;
});

test('first save retains its success notice through the new-to-existing editor route load only', async () => {
  const previousFetch = global.fetch;
  let navigate;
  let body;
  const draft = () => ({
    key: 'created',
    kind: 'article',
    source_locale: 'en',
    locale: 'en',
    version: 1,
    revision: { ...body, revision_id: 'saved-revision', media: [], media_presentations: [] },
  });
  global.fetch = async (url, options) => {
    if (url === '/api/v1/content/created/en') {
      if (options.method === 'PUT') {
        body = JSON.parse(options.body);
        return response({ revision_id: 'saved-revision', version: 1 }, 201);
      }
      return response(draft());
    }
    return response({ items: [], next_cursor: '' });
  };
  function Harness() {
    navigate = useNavigate();
    return React.createElement(
      Routes,
      {},
      React.createElement(Route, {
        path: '/en/admin/new',
        element: React.createElement(EditorPage, { csrf: 'test' }),
      }),
      React.createElement(Route, {
        path: '/en/admin/editor/:key/:lang',
        element: React.createElement(EditorPage, { csrf: 'test' }),
      }),
    );
  }
  const m = await mount(
    React.createElement(
      MemoryRouter,
      { initialEntries: ['/en/admin/new'] },
      React.createElement(Harness),
    ),
  );
  try {
    async function fill(node, value) {
      await act(async () => {
        Object.getOwnPropertyDescriptor(Object.getPrototypeOf(node), 'value').set.call(node, value);
        node.dispatchEvent(new dom.window.Event('input', { bubbles: true }));
      });
    }
    await fill(m.el.querySelector('.stable-key-field input'), 'created');
    await fill(
      m.el.querySelector('[aria-label="' + DEFAULT_LABELS['owner.title'].en + '"]'),
      'Created title',
    );
    const save = [...m.el.querySelectorAll('button')].find(
      (button) => button.textContent === DEFAULT_LABELS['owner.save'].en,
    );
    await act(async () => save.click());
    assert.equal(body.title, 'Created title');
    assert.ok(
      [...m.el.querySelectorAll('[role="status"]')].some((node) =>
        node.textContent.includes(DEFAULT_LABELS['owner.saved'].en),
      ),
    );
    assert.equal(m.el.querySelector('.stable-key-field input').disabled, true);
    await act(async () => navigate('/en/admin/new'));
    assert.ok(
      ![...m.el.querySelectorAll('[role="status"]')].some((node) =>
        node.textContent.includes(DEFAULT_LABELS['owner.saved'].en),
      ),
    );
  } finally {
    await m.cleanup();
    global.fetch = previousFetch;
  }
});
