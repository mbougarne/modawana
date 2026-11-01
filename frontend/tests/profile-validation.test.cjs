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
const { I18nProvider } = require('../.test-output/ui/src/i18n/I18nProvider');
const {
  ValidationIssues,
  FieldIssues,
  useFieldIssue,
} = require('../.test-output/frontend/src/features/configuration/ValidationIssues');
const { call, AuthError } = require('../.test-output/frontend/src/features/auth/api');

test('API retains structured profile errors without discarding the existing error code', async () => {
  global.fetch = async () => ({
    ok: false,
    json: async () => ({
      error: {
        code: 'invalid_fields',
        fields: [{ path: 'portrait_alt.en', code: 'image_alt' }, null, { unexpected: true }],
      },
    }),
  });
  await assert.rejects(call('/profile/preview', 'POST', {}), (error) => {
    assert.ok(error instanceof AuthError);
    assert.equal(error.code, 'invalid_fields');
    assert.deepEqual(error.fields, [{ path: 'portrait_alt.en', code: 'image_alt' }]);
    return true;
  });
});

test('Profile error list explains the field in EN/AR/FR and focuses preserved working input', async () => {
  for (const locale of ['en', 'ar', 'fr']) {
    const issues = [
      { path: 'portrait_alt.en', code: 'image_alt' },
      { path: 'links.1.url', code: 'url' },
    ];
    function Control() {
      return React.createElement('input', {
        ...useFieldIssue('portrait_alt.en'),
        defaultValue: 'my working text',
      });
    }
    const el = document.createElement('div');
    document.body.append(el);
    const root = createRoot(el);
    await act(async () =>
      root.render(
        React.createElement(
          I18nProvider,
          { locale },
          React.createElement(
            FieldIssues.Provider,
            { value: issues },
            React.createElement(ValidationIssues, { issues }),
            React.createElement(Control),
          ),
        ),
      ),
    );
    const input = el.querySelector('input');
    assert.equal(input.getAttribute('aria-invalid'), 'true');
    assert.equal(input.getAttribute('aria-describedby'), 'config-error-portrait_alt.en');
    assert.equal(el.querySelectorAll('li').length, 2);
    assert.equal(el.textContent.includes('validation.'), false);
    assert.equal(el.textContent.includes('config.'), false);
    input.scrollIntoView = () => {};
    await act(async () => el.querySelector('button').click());
    assert.equal(document.activeElement, input);
    assert.equal(input.value, 'my working text');
    await act(async () => root.unmount());
    el.remove();
  }
});

test('Localized controls retain label bindings, error description and typed edits', async () => {
  const Module = require('node:module');
  const original = Module._load;
  Module._load = function (name, ...args) {
    if (name === '@ui') {
      return {
        ...require('../.test-output/ui/src/components/primitives'),
        ...require('../.test-output/ui/src/i18n/I18nProvider'),
      };
    }
    return original.call(this, name, ...args);
  };
  const {
    LocalizedField,
  } = require('../.test-output/frontend/src/features/configuration/controls');
  Module._load = original;
  const issues = [{ path: 'name.en', code: 'english_source' }];
  let changed;
  const el = document.createElement('div');
  document.body.append(el);
  const root = createRoot(el);
  await act(async () =>
    root.render(
      React.createElement(
        I18nProvider,
        { locale: 'en' },
        React.createElement(
          FieldIssues.Provider,
          { value: issues },
          React.createElement(ValidationIssues, { issues }),
          React.createElement(LocalizedField, {
            name: 'config.name',
            path: 'name',
            value: { en: '', ar: 'نور' },
            onChange: (value) => {
              changed = value;
            },
          }),
        ),
      ),
    ),
  );
  const input = el.querySelector('[data-config-path="name.en"]');
  assert.equal(input.getAttribute('aria-invalid'), 'true');
  assert.equal(input.getAttribute('aria-describedby'), 'config-error-name.en');
  assert.equal(
    [...el.querySelectorAll('label')].some(
      (label) => label.htmlFor === input.id && label.textContent === 'English',
    ),
    true,
  );
  await act(async () => {
    Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value').set.call(
      input,
      'Noor',
    );
    input.dispatchEvent(new window.Event('input', { bubbles: true }));
  });
  assert.deepEqual(changed, { en: 'Noor', ar: 'نور' });
  await act(async () => root.unmount());
  el.remove();
});

test('Request messages distinguish validation, expired session, denied request, rate limit and server/network failures', async () => {
  const { requestErrorKey } = require('../.test-output/frontend/src/features/auth/api');
  for (const [code, key] of Object.entries({
    invalid_fields: 'invalid',
    unauthenticated: 'session',
    forbidden: 'confirm',
    rate_limited: 'rate',
    internal_error: 'server',
    unavailable: 'unavailable',
  })) {
    assert.equal(requestErrorKey(new AuthError(code)), 'request.' + key);
  }
  assert.equal(requestErrorKey(new TypeError('fetch failed')), 'request.network');
  global.fetch = async () => ({
    status: 503,
    ok: false,
    json: async () => {
      throw new SyntaxError('upstream HTML');
    },
  });
  await assert.rejects(
    call('/settings/preview'),
    (error) => requestErrorKey(error) === 'request.unavailable',
  );
});

test('Settings and account summaries explain exact constraints in every language and focus the submitted form', async () => {
  for (const locale of ['en', 'ar', 'fr']) {
    const el = document.createElement('div');
    document.body.append(el);
    const root = createRoot(el);
    const scope = document.createElement('form');
    document.body.append(scope);
    const input = document.createElement('input');
    input.dataset.configPath = 'security.password';
    input.value = 'working secret';
    input.scrollIntoView = () => {};
    scope.append(input);
    const issues = [
      { path: 'wordmark', code: 'wordmark' },
      { path: 'primary', code: 'colour' },
      { path: 'security.password', code: 'password_length' },
    ];
    await act(async () =>
      root.render(
        React.createElement(
          I18nProvider,
          { locale },
          React.createElement(ValidationIssues, { issues, scope: { current: scope } }),
        ),
      ),
    );
    assert.equal(el.querySelectorAll('li').length, 3);
    assert.equal(el.textContent.includes('validation.'), false);
    assert.equal(el.textContent.includes('config.'), false);
    assert.ok(el.textContent.includes('80'));
    assert.ok(el.textContent.includes('1024'));
    assert.ok(el.textContent.includes('#RRGGBB'));
    await act(async () => el.querySelectorAll('button')[2].click());
    assert.equal(document.activeElement, input);
    assert.equal(input.value, 'working secret');
    assert.equal(el.textContent.includes('working secret'), false);
    await act(async () => root.unmount());
    el.remove();
    scope.remove();
  }
});
