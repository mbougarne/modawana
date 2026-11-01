const { test } = require('node:test');
const assert = require('node:assert/strict');

test('Smoke selectors interpolate counts and all variables in EN/AR/FR', async () => {
  const { smokeTranslator } = await import('../../scripts/smoke-i18n.mjs');
  for (const [locale, expected] of [
    ['en', 'Selected: 2'],
    ['ar', 'المحدد: 2'],
    ['fr', 'Sélectionnés : 2'],
  ]) {
    const t = smokeTranslator(locale);
    assert.equal(t('bulk.selected', { n: 2 }), expected);
    const result = t('bulk.result', { changed: 2, unchanged: 0 });
    assert.doesNotMatch(result, /\{\w+\}/);
    assert.match(result, /2/);
    assert.match(result, /0/);
    assert.throws(() => t('bulk.selected'), /Missing smoke label variables/);
    assert.throws(() => t('missing.test.key'), /Unknown smoke label/);
  }
});

test('Smoke selectors use production plural rules, including Arabic forms', async () => {
  const { smokeTranslator } = await import('../../scripts/smoke-i18n.mjs');
  const en = smokeTranslator('en');
  assert.equal(en('writing.count', { n: 1 }), '1 post');
  assert.equal(en('writing.count', { n: 2 }), '2 posts');
  const ar = smokeTranslator('ar');
  const expected = ['0 منشورات', '1 منشور', '2 منشوران', '3 منشورات', '11 منشورًا', '100 منشور'];
  [0, 1, 2, 3, 11, 100].forEach((n, i) => {
    assert.equal(ar('writing.count', { n }), expected[i]);
  });
});
