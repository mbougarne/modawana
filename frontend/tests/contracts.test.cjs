const { test } = require('node:test');
const assert = require('node:assert/strict');
const React = require('react');
const { renderToStaticMarkup } = require('react-dom/server');
const { resolveLabel } = require('../.test-output/ui/src/i18n/resolve');
const { pickLocale } = require('../.test-output/ui/src/i18n/locales');
const { I18nProvider, Label } = require('../.test-output/ui/src/i18n/I18nProvider');

test('empty overrides fall back to effective English; removing override restores shipped locale', () => {
  const overrides = { 'nav.articles': { en: 'Notes', fr: '', ar: '' } };
  assert.deepEqual(resolveLabel('nav.articles', 'ar', overrides), {
    text: 'Notes',
    locale: 'en',
    dir: 'ltr',
    fallback: true,
  });
  assert.equal(resolveLabel('nav.articles', 'fr', overrides).text, 'Notes');
  assert.equal(resolveLabel('nav.articles', 'fr').text, 'Écrits');
  assert.equal(
    resolveLabel('nav.articles', 'ar', { 'nav.articles': { en: 'Notes' } }).text,
    'الكتابات',
  );
  assert.equal(
    resolveLabel('custom.title', 'fr', { 'custom.title': { en: 'Custom' } }).text,
    'Custom',
  );
  const html = renderToStaticMarkup(
    React.createElement(
      I18nProvider,
      { locale: 'ar', overrides },
      React.createElement(Label, { name: 'nav.articles' }),
    ),
  );
  assert.match(html, /lang="en" dir="ltr">Notes/);
});

test('all six Arabic plural categories and effective fallback language select appropriate forms', () => {
  const expected = ['0 منشورات', '1 منشور', '2 منشوران', '3 منشورات', '11 منشورًا', '100 منشور'];
  [0, 1, 2, 3, 11, 100].forEach((n, i) =>
    assert.equal(resolveLabel('writing.count', 'ar', undefined, { n }).text, expected[i]),
  );
  assert.equal(resolveLabel('writing.count', 'en', undefined, { n: 1 }).text, '1 post');
  assert.equal(resolveLabel('writing.count', 'en', undefined, { n: 2 }).text, '2 posts');
  assert.equal(
    resolveLabel(
      'writing.count',
      'ar',
      { 'writing.count': { ar: '', en: { one: '{n} note', other: '{n} notes' } } },
      { n: 2 },
    ).text,
    '2 notes',
  );
});

test('fallback only accepts explicit profile/label text domains', () => {
  assert.equal(pickLocale({ en: 'About', ar: '' }, 'ar', 'profile'), 'About');
  assert.throws(() => pickLocale({ en: 'Private body' }, 'ar', 'article'), /not allowed/);
  assert.throws(() => pickLocale({ en: 'Private body' }, 'ar'), /not allowed/);
});

test('default branding uses generated tokens; custom branding carries the supplied CSP nonce', () => {
  const { BrandStyle } = require('../.test-output/ui/src/theme/ThemeProvider.js');
  const {
    DEFAULT_PRIMARY,
    DEFAULT_SECONDARY,
    brandCss,
  } = require('../.test-output/ui/src/theme/brand.js');
  assert.equal(brandCss(DEFAULT_PRIMARY, DEFAULT_SECONDARY), '');
  const html = renderToStaticMarkup(
    React.createElement(BrandStyle, {
      primary: '#123456',
      secondary: '#ffaa00',
      nonce: 'test-nonce',
    }),
  );
  assert.match(html, /nonce="test-nonce"/);
  assert.match(html, /data-brand/);
});

test('Field keeps its visible label separate from its accessible help description', () => {
  const { Field, TextInput } = require('../.test-output/ui/src/components/primitives');
  const { JSDOM } = require('jsdom');
  const html = renderToStaticMarkup(
    React.createElement(
      Field,
      { label: 'Private email', help: 'Separate from the public profile.' },
      React.createElement(TextInput, { type: 'email' }),
    ),
  );
  const document = new JSDOM(html).window.document;
  const input = document.querySelector('input');
  const label = document.querySelector('label');
  assert.equal(label.textContent, 'Private email');
  assert.equal(label.htmlFor, input.id);
  assert.equal(
    document.getElementById(input.getAttribute('aria-describedby')).textContent,
    'Separate from the public profile.',
  );
});
