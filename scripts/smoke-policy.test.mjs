import assert from 'node:assert/strict';
import test from 'node:test';
import { smokeSuite, smokeLocales, visualCases, acceptanceCases } from './smoke-policy.mjs';

test('quick selection retains every product locale, RTL, both themes and phone/desktop', () => {
  const cases = visualCases('quick');
  assert.deepEqual(
    cases.map((entry) => entry.locale),
    ['en', 'ar', 'fr'],
  );
  assert.ok(cases.some((entry) => entry.locale === 'ar' && entry.width === 320));
  assert.deepEqual(new Set(cases.map((entry) => entry.theme)), new Set(['light', 'dark']));
  assert.ok(cases.some((entry) => entry.width === 1280));
  assert.deepEqual(smokeLocales('quick', 'en'), ['en']);
  assert.deepEqual(acceptanceCases('quick'), [{ width: 320, theme: 'light' }]);
});

test('full selection and explicit locale overrides remain available; typos fail closed', () => {
  assert.equal(smokeSuite('full'), 'full');
  assert.deepEqual(smokeLocales('full', 'en,ar,fr'), ['en', 'ar', 'fr']);
  assert.deepEqual(smokeLocales('quick', 'ar,fr'), ['ar', 'fr']);
  assert.equal(visualCases('full').length, 18);
  assert.equal(acceptanceCases('full').length, 6);
  assert.throws(() => smokeSuite('quik'));
  assert.throws(() => smokeLocales('full', ''));
  assert.throws(() => smokeLocales('full', 'en,xx'));
});
