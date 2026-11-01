const { test } = require('node:test');
const assert = require('node:assert/strict');
const React = require('react');
const { renderToStaticMarkup } = require('react-dom/server');
const { MemoryRouter } = require('react-router-dom');
const { JSDOM } = require('jsdom');
const {
  EntryList,
  CollectionCards,
} = require('../.test-output/frontend/src/pages/public/presentation');
const { I18nProvider } = require('../.test-output/ui/src/i18n/I18nProvider');
function render(component, locale = 'en') {
  return new JSDOM(
    renderToStaticMarkup(
      React.createElement(
        MemoryRouter,
        null,
        React.createElement(I18nProvider, { locale }, component),
      ),
    ),
  ).window.document;
}
test('Writing rows use the reference year groups, full-row links and translated public taxonomy', () => {
  const d = render(
    React.createElement(EntryList, {
      items: [
        {
          key: 'one',
          slug: 'one',
          kind: 'article',
          title: '<Long title>',
          url: '/en/articles/one',
          published_at: '2026-10-25T00:00:00Z',
          tag_keys: ['go'],
          category_key: 'code',
        },
      ],
      terms: [
        { kind: 'category', key: 'code', slug: 'code', label: 'Code' },
        { kind: 'tag', key: 'go', slug: 'go', label: 'Go' },
      ],
    }),
  );
  assert.equal(d.querySelector('.vb-year h3').textContent, '2026');
  assert.equal(d.querySelectorAll('.vb-post a').length, 1);
  assert.equal(d.querySelector('.vb-post a').getAttribute('href'), '/en/articles/one');
  assert.equal(d.querySelector('.vb-post-main b').textContent, '<Long title>');
  assert.equal(d.querySelector('.vb-post-tags').textContent, '#Go');
  assert.equal(d.querySelector('.vb-tag').textContent, 'Code');
  assert.equal(d.querySelector('script'), null);
});
test('Learning cards are each one semantic focusable link, with actual counts and no nested links', () => {
  const d = render(
    React.createElement(CollectionCards, {
      items: [
        {
          key: 'a',
          url: '/ar/learn/a',
          title: 'مسار التعلم',
          summary: 'خطوات مرتبة',
          published_count: 2,
          announced_count: 1,
        },
      ],
    }),
    'ar',
  );
  const a = d.querySelector('a.vb-course');
  assert.equal(a.getAttribute('href'), '/ar/learn/a');
  assert.equal(a.querySelector('h3').textContent, 'مسار التعلم');
  assert.equal(a.querySelectorAll('a').length, 0);
  assert.equal(a.querySelector('.vb-badge').getAttribute('aria-hidden'), 'true');
});

test('A locked lesson keeps its accessible lock indicator without adding a fifth row column', () => {
  const d = render(
    React.createElement(EntryList, {
      items: [
        {
          key: 'locked',
          slug: 'locked',
          kind: 'lesson',
          locked: true,
          title: 'Protected lesson',
          url: '/en/learn/course/locked',
          published_at: '2026-10-25T00:00:00Z',
          tag_keys: [],
        },
      ],
    }),
  );
  const row = d.querySelector('.vb-post .vb-post-locked');
  assert.equal(d.querySelector('.vb-post a'), null);
  assert.equal(row.hasAttribute('tabindex'), false);
  assert.equal(row.children.length, 4);
  assert.match(row.textContent, /Locked/i);
  assert.equal(row.querySelectorAll('.vb-tag svg').length, 1);
});
