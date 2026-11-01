const { test } = require('node:test');
const assert = require('node:assert/strict');
const React = require('react');
const { renderToStaticMarkup } = require('react-dom/server');
const { JSDOM } = require('jsdom');
const { I18nProvider } = require('../.test-output/ui/src/i18n/I18nProvider');
const {
  MarkdownReader,
  safeLink,
} = require('../.test-output/frontend/src/features/markdown/MarkdownReader');
const id = 'a'.repeat(32);
const video = 'b'.repeat(32);
const track = 'c'.repeat(32);
function render(markdown, locale = 'en', extra = {}) {
  return renderToStaticMarkup(
    React.createElement(
      I18nProvider,
      { locale },
      React.createElement(MarkdownReader, { markdown, ...extra }),
    ),
  );
}
test('one safe Markdown pipeline handles GFM, code, math and authored takeaway in every locale', () => {
  for (const locale of ['en', 'ar', 'fr']) {
    const html = render(
      '# Read\n\n~~old~~ **new**\n\n| Name | Value |\n|---|---|\n| x | 2 |\n\n```go\nx := 1\n```\n\n$x^2 + 1$\n\n- [x] Done',
      locale,
      { takeaway: '**Remember this.**' },
    );
    const doc = new JSDOM(html).window.document;
    assert.ok(doc.querySelector('del'));
    assert.ok(doc.querySelector('table'));
    assert.ok(doc.querySelector('math'));
    assert.equal(doc.querySelector('pre').textContent, 'x := 1');
    assert.equal(doc.querySelector('code').closest('[dir]').dir, 'ltr');
    assert.match(html, /Remember this/);
    assert.equal(doc.querySelector('.vb-reader').dir, locale === 'ar' ? 'rtl' : 'ltr');
  }
});
test('raw HTML, executable URLs, remote images, missing media and math trust commands cannot create active content', () => {
  const html = render(
    '<script>alert(1)</script>\n\n<iframe src="https://evil.test"></iframe>\n\n[x](javascript:alert%281%29)\n\n![x](https://evil.test/a.png)\n\n![x](/media/' +
      id +
      ')\n\n$\\href{javascript:alert(1)}{x}$',
  );
  const doc = new JSDOM(html).window.document;
  assert.equal(doc.querySelectorAll('script,iframe,img,a[href^="javascript:"]').length, 0);
  for (const x of [
    '//evil.test',
    '/\\evil.test',
    '/%2fevil.test',
    'javascript:alert(1)',
    'data:text/html,x',
    'https://user:pw@example.test',
    '/a%0ab',
  ]) {
    assert.equal(safeLink(x), undefined);
  }
});
test('authorized manifests control images, video poster/captions/transcripts and downloads', () => {
  const media = [
    { id, kind: 'image', width: 32, height: 32 },
    { id: video, kind: 'video' },
    { id: track, kind: 'captions' },
  ];
  const html = render('![Blue square](/media/' + id + ')\n\n[Clip](/media/' + video + ')', 'ar', {
    media,
    presentations: [
      {
        media_id: video,
        poster_media_id: id,
        caption_track_media_id: track,
        caption: 'Caption',
        transcript: 'Transcript',
      },
    ],
  });
  const doc = new JSDOM(html).window.document;
  assert.equal(doc.querySelector('img').alt, 'Blue square');
  assert.equal(doc.querySelector('video').getAttribute('poster'), '/media/' + id);
  assert.equal(doc.querySelector('track').getAttribute('src'), '/media/' + track);
  assert.match(doc.querySelector('details').textContent, /Transcript/);
  assert.equal(doc.querySelectorAll('p video').length, 0);
});
