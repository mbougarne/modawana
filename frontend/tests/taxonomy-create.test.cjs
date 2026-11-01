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
const dom = new JSDOM('<!doctype html><html><body></body></html>');
global.window = dom.window;
global.document = dom.window.document;
global.IS_REACT_ACT_ENVIRONMENT = true;
const React = require('react');
const { act } = React;
const { createRoot } = require('react-dom/client');
const { I18nProvider } = require('../.test-output/ui/src/i18n/I18nProvider');
const { TaxonomyCreate } = require('../.test-output/frontend/src/features/owner/TaxonomyCreate');
const api = require('../.test-output/frontend/src/features/auth/api');
async function input(node, value) {
  await act(async () => {
    Object.getOwnPropertyDescriptor(dom.window.HTMLInputElement.prototype, 'value').set.call(
      node,
      value,
    );
    node.dispatchEvent(new dom.window.Event('input', { bubbles: true }));
  });
}
test('Inline taxonomy creation uses the content locale, CSRF and saved result; failure preserves input', async () => {
  for (const kind of ['category', 'tag']) {
    const el = document.createElement('div');
    document.body.append(el);
    const root = createRoot(el);
    let created;
    let fail = true;
    api.call = async (url, method, body, csrf) => {
      assert.match(url, new RegExp('/taxonomy/' + kind + '/key-.*?/ar$'));
      assert.equal(method, 'PUT');
      assert.equal(csrf, 'test-csrf');
      assert.equal(body.expected_version, 0);
      assert.equal(body.label, 'موضوع');
      if (fail) {
        throw new Error('network');
      }
      return {
        id: 'saved-id',
        key: 'saved-key',
        kind,
        locale: 'ar',
        label: body.label,
        slug: body.slug,
        version: 1,
        count: 0,
      };
    };
    try {
      await act(async () =>
        root.render(
          React.createElement(
            I18nProvider,
            { locale: 'ar' },
            React.createElement(TaxonomyCreate, {
              kind,
              locale: 'ar',
              csrf: 'test-csrf',
              disabled: false,
              onCreated: (term) => {
                created = term;
              },
            }),
          ),
        ),
      );
      await act(async () => el.querySelector('button').click());
      await input(el.querySelector('input'), 'موضوع');
      await act(async () => el.querySelector('fieldset button').click());
      assert.ok(el.querySelector('[role="alert"]'));
      assert.equal(created, undefined);
      assert.equal(el.querySelector('input').value, 'موضوع');
      fail = false;
      await act(async () => el.querySelector('fieldset button').click());
      assert.equal(created.id, 'saved-id');
      assert.equal(created.key, 'saved-key');
      assert.equal(el.querySelector('fieldset'), null);
    } finally {
      await act(async () => root.unmount());
      el.remove();
    }
  }
});
