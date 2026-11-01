const { test } = require('node:test');
const assert = require('node:assert/strict');
const { JSDOM } = require('jsdom');
const {
  socialMetadata,
  updateSocialMetadata,
} = require('../.test-output/frontend/src/pages/public/social');

const origin = 'https://blog.example';
const page = {
  kind: 'article',
  content: {
    cover_media_id: 'cover',
    media: [{ id: 'cover', kind: 'image', mime_type: 'image/png', width: 1200, height: 630 }],
  },
};
const configuration = {
  site: { logo_mode: 'image', logo_media_id: 'logo', wordmark: 'Blog' },
  profile: { banner_enabled: true, banner_media_id: 'banner', portrait_media_id: 'portrait' },
  effective: { name: { text: 'Name' }, banner_alt: { text: 'Banner' } },
};

test('social images use public covers and public branding with a guaranteed fallback', () => {
  assert.equal(socialMetadata(page, configuration, origin, 'Title').image, origin + '/media/cover');
  assert.equal(
    socialMetadata({ ...page, kind: 'lesson' }, configuration, origin, 'Title').image,
    origin + '/media/banner',
  );
  for (const content of [
    undefined,
    { ...page.content, locked: true },
    { ...page.content, media: [] },
  ]) {
    assert.equal(
      socialMetadata({ ...page, content }, configuration, origin, 'Title').image,
      origin + '/media/banner',
    );
  }
  assert.equal(
    socialMetadata({ kind: 'home' }, undefined, origin, 'Title').image,
    origin + '/social-card.png',
  );
});

test('localized SPA metadata replaces old cards and clears images for inaccessible pages', () => {
  const dom = new JSDOM('<html><head></head><body></body></html>');
  global.document = dom.window.document;
  try {
    for (const title of ['Introduction', 'مقدمة', 'Présentation']) {
      updateSocialMetadata(socialMetadata(page, configuration, origin, title), title, 'Summary');
      assert.equal(document.querySelectorAll('meta[property="og:image"]').length, 1);
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
