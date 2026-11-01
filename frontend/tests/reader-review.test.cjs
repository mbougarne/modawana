const { test } = require('node:test');
const assert = require('node:assert/strict');
const path = require('node:path');
const Module = require('node:module');
const original = Module._resolveFilename;
Module._resolveFilename = function (request, ...args) {
  return original.call(
    this,
    request === '@ui' ? path.resolve(__dirname, '../.test-output/ui/src/index.js') : request,
    ...args,
  );
};
const React = require('react');
const { renderToStaticMarkup } = require('react-dom/server');
const { MemoryRouter } = require('react-router-dom');
const { JSDOM } = require('jsdom');
const ui = require('../.test-output/ui/src/index');
const config = require('../.test-output/frontend/src/features/configuration/PublicConfiguration');
const api = require('../.test-output/frontend/src/pages/public/api');
let footer = '';
config.usePublicConfiguration = () => ({
  data: {
    site: { logo_mode: 'monogram', monogram: 'TW' },
    profile: {
      name: { en: 'Test Writer' },
      bio: { en: 'A writer.' },
      footer: { en: footer },
      public_email: 'writer@example.test',
    },
  },
});
api.useResource = (url) => ({
  data: url.includes('taxonomy')
    ? { items: [{ key: 'tv', kind: 'category', slug: 'tv', label: 'TV platforms' }] }
    : undefined,
  loading: false,
});
const { ReaderPage } = require('../.test-output/frontend/src/pages/public/reading');
const { PublicLayout } = require('../.test-output/frontend/src/pages/public/PublicLayout');
const { WritingPage } = require('../.test-output/frontend/src/pages/public/writing');
function render(component, route = '/en/articles/test') {
  return new JSDOM(
    renderToStaticMarkup(
      React.createElement(
        MemoryRouter,
        { initialEntries: [route] },
        React.createElement(
          ui.ThemeProvider,
          { initial: 'light' },
          React.createElement(ui.I18nProvider, { locale: 'en' }, component),
        ),
      ),
    ),
  ).window.document;
}
test('Article and lesson metadata use category, author, date and reading time without duplicate languages', () => {
  for (const kind of ['article', 'lesson']) {
    const content = {
      key: 'test',
      kind,
      locale: 'en',
      title: 'Test title',
      summary: 'Summary',
      published_at: '2026-10-25T00:00:00Z',
      reading_minutes: 4,
      category_key: 'tv',
      alternatives: [{ locale: 'en', url: '/en/articles/test' }],
      markdown: 'Visible body',
    };
    const d = render(React.createElement(ReaderPage, { page: { content } }));
    assert.equal(d.querySelector('.vb-catlink').getAttribute('href'), '/en/writing?category=tv');
    assert.match(d.querySelector('.vb-sticker').textContent, /TV platforms/);
    assert.equal(d.querySelector('.public-article-author').textContent, 'TWTest Writer');
    assert.equal(d.querySelector('time').dateTime, content.published_at);
    assert.match(d.querySelector('.vb-art-meta').textContent, /4 min read/);
    assert.doesNotMatch(d.querySelector('.vb-art-head').textContent, /Updated/);
    assert.equal(d.querySelector('.public-translations'), null);
    const locked = render(
      React.createElement(ReaderPage, {
        page: {
          content: {
            ...content,
            locked: true,
            markdown: 'SECRET BODY',
            media: [{ id: 'secret', kind: 'image', url: '/media/secret' }],
            cover_media_id: 'secret',
          },
        },
      }),
    );
    assert.doesNotMatch(locked.body.textContent, /SECRET BODY/);
    assert.equal(locked.querySelector('img'), null);
  }
});
test('Configured footer replaces the default sentence and clearing restores the email action', () => {
  footer = 'Got a question or a TV bug story? Say hi.';
  let d = render(React.createElement(PublicLayout, null, 'Content'));
  assert.equal(d.querySelectorAll('footer p').length, 1);
  assert.equal(d.querySelector('footer .big').textContent, footer);
  footer = '';
  d = render(React.createElement(PublicLayout, null, 'Content'));
  assert.equal(d.querySelectorAll('footer p').length, 1);
  assert.equal(d.querySelector('footer .big a').getAttribute('href'), 'mailto:writer@example.test');
});
test('Writing has taxonomy filters without a second search form; dedicated Search retains its input', () => {
  const writing = render(React.createElement(WritingPage), '/en/writing');
  assert.equal(writing.querySelector('input[type="search"]'), null);
  assert.ok(writing.querySelector('[role="group"]'));
  const search = render(React.createElement(WritingPage), '/en/search');
  assert.ok(search.querySelector('input[type="search"]'));
});

test('Footer uses localized section routes and only safe visible configured social links', () => {
  const prior = config.usePublicConfiguration;
  config.usePublicConfiguration = () => {
    const value = prior();
    return {
      ...value,
      data: {
        ...value.data,
        profile: {
          ...value.data.profile,
          links: [
            { id: 'gh', icon: 'github', visible: true, url: 'https://github.com/example' },
            {
              id: 'hidden',
              icon: 'linkedin',
              visible: false,
              url: 'https://linkedin.com/in/hidden',
            },
            { id: 'bad', icon: 'linkedin', visible: true, url: 'javascript:alert(1)' },
            { id: 'li', icon: 'linkedin', visible: true, url: 'https://linkedin.com/in/example' },
            { id: 'rss', icon: 'rss', visible: true, url: '/en/feed.xml' },
          ],
        },
      },
    };
  };
  try {
    const d = render(React.createElement(PublicLayout, null, 'Content'));
    assert.deepEqual(
      [...d.querySelectorAll('footer nav a')].map((a) => a.getAttribute('href')),
      [
        '/en/writing',
        '/en/learn',
        '/en/docs',
        'https://github.com/example',
        'https://linkedin.com/in/example',
        '/en/feed.xml',
      ],
    );
  } finally {
    config.usePublicConfiguration = prior;
  }
});

test('Docs index includes published standalone documents when no collection plan is active', () => {
  const originalResource = api.useResource;
  const urls = [];
  api.useResource = (url) => {
    urls.push(url);
    if (url?.includes('/public/content/en/')) {
      return { loading: false };
    }
    return {
      loading: false,
      data: {
        items: url.includes('/public/content?')
          ? [
              {
                key: 'guide',
                kind: 'doc',
                title: 'Standalone guide',
                slug: 'guide',
                url: '/en/doc/guide',
                published_at: '2026-10-26T00:00:00Z',
                tag_keys: [],
              },
            ]
          : [],
      },
    };
  };
  try {
    const { CollectionIndex } = require('../.test-output/frontend/src/pages/public/reading');
    const doc = render(React.createElement(CollectionIndex, { kind: 'docs' }), '/en/docs');
    assert.ok(doc.querySelector('a[href="/en/doc/guide"]'));
    assert.ok(urls.some((url) => url.includes('kind=doc&')));
    assert.doesNotMatch(doc.body.textContent, /Nothing published/);
  } finally {
    api.useResource = originalResource;
  }
});

test('Home writing includes the archive kinds and About retains a portrait placeholder without a name', () => {
  const oldConfig = config.usePublicConfiguration;
  const oldResource = api.useResource;
  const requests = [];
  config.usePublicConfiguration = () => ({
    data: {
      site: { monogram: 'B' },
      profile: {
        name: {},
        headline: {},
        bio: {},
        links: [],
        experience: [
          {
            id: 'role',
            role: { en: 'Engineer' },
            organization: { en: 'Team' },
            start: '2020-01',
            current: true,
          },
        ],
        now: [],
        show_writing: true,
        writing_limit: 5,
      },
    },
  });
  api.useResource = (url) => {
    requests.push(url);
    return { loading: false, data: url ? { items: [] } : undefined };
  };
  try {
    const { HomePage } = require('../.test-output/frontend/src/pages/public/home');
    render(React.createElement(HomePage), '/en');
    assert.ok(requests.includes('/api/v1/public/content?locale=en&limit=5'));
    const about = render(React.createElement(HomePage, { about: true }), '/en/about');
    assert.ok(about.querySelector('.public-about-aside .vb-portrait'));
    assert.equal(about.querySelector('.vb-exp b').textContent, 'Engineer');
  } finally {
    config.usePublicConfiguration = oldConfig;
    api.useResource = oldResource;
  }
});

test('Writing provides Previous and Next and excludes navigation history from discovery requests', () => {
  const originalResource = api.useResource;
  const urls = [];
  api.useResource = (url) => {
    urls.push(url);
    return { loading: false, data: { items: [], total: 40, next_cursor: 'page3' } };
  };
  try {
    const d = render(React.createElement(WritingPage), '/en/writing?cursor=page2&previous=&tag=tv');
    assert.deepEqual(
      [...d.querySelectorAll('.public-pagination button')].map((b) => b.textContent),
      ['Previous', 'Next'],
    );
    assert.ok(urls.some((url) => url.includes('cursor=page2') && url.includes('tag=tv')));
    assert.ok(urls.every((url) => !url.includes('previous=')));
  } finally {
    api.useResource = originalResource;
  }
});
