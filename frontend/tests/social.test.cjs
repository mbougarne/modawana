const { test } = require('node:test');
const assert = require('node:assert/strict');
const { JSDOM } = require('jsdom');
const { updateSocialMetadata } = require('../.test-output/frontend/src/pages/public/social');

test('SPA metadata displays the server-selected card and clears it for inaccessible pages', () => {
  const dom = new JSDOM('<html><head></head><body></body></html>');
  global.document = dom.window.document;
  try {
    const image = 'https://blog.example/media/chosen-by-server';
    for (const title of ['Introduction', 'مقدمة', 'Présentation']) {
      updateSocialMetadata(
        { image, alt: title, type: 'article', site_name: 'Blog', width: 640, height: 480 },
        title,
        'Summary',
      );
      assert.equal(document.querySelectorAll('meta[property="og:image"]').length, 1);
      assert.equal(document.querySelector('meta[property="og:image"]').content, image);
      assert.equal(document.querySelector('meta[property="og:image:width"]').content, '640');
      assert.equal(document.querySelector('meta[property="og:site_name"]').content, 'Blog');
      assert.equal(document.querySelector('meta[name="twitter:title"]').content, title);
      assert.equal(
        document.querySelector('meta[name="twitter:card"]').content,
        'summary_large_image',
      );
    }
    updateSocialMetadata(undefined, 'Missing', '');
    assert.equal(document.querySelector('meta[property="og:image"]'), null);
    assert.equal(document.querySelector('meta[name^="twitter:"]'), null);
  } finally {
    delete global.document;
    dom.window.close();
  }
});
