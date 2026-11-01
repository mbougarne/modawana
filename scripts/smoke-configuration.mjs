import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { resolve } from 'node:path';

export async function smokeConfiguration({ context, origin, root, locale, t }) {
  const page = await context.newPage();
  page.setDefaultTimeout(30000);
  const errors = [];
  page.on('pageerror', (e) => errors.push(e.message));
  page.on('console', (m) => {
    if (
      m.type() === 'error' &&
      /Content Security Policy|Refused to|violates.*directive/i.test(m.text())
    ) {
      errors.push(m.text());
    }
  });
  const { csrf } = await (await context.request.get(origin + '/api/v1/session')).json();
  const headers = { Origin: origin, 'X-CSRF-Token': csrf };
  const anon = await context.browser().newContext({ ignoreHTTPSErrors: true });
  const button = (k) => page.getByRole('button', { name: t(k), exact: true });
  async function api(method, path, data, status = 200) {
    const r = await context.request.fetch(origin + '/api/v1' + path, { method, headers, data });
    assert.equal(r.status(), status, await r.text());
    return r.json();
  }
  async function publicSite() {
    return (await anon.request.get(origin + '/api/v1/public/site?locale=' + locale)).json();
  }
  async function shot(name) {
    await page.evaluate(() => document.fonts.ready);
    assert.ok(
      await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth),
      'configuration horizontal overflow',
    );
    await page.screenshot({
      path: resolve(root, `frontend/test-results/config-${locale}-${name}.png`),
      fullPage: false,
    });
  }
  async function upload(name, mime, buffer, purpose = 'content') {
    const r = await context.request.post(origin + '/api/v1/media?purpose=' + purpose, {
      headers,
      multipart: { file: { name, mimeType: mime, buffer } },
    });
    assert.equal(r.status(), 201, await r.text());
    return r.json();
  }
  try {
    assert.equal((await anon.request.get(origin + `/${locale}/admin/settings`)).status(), 401);
    await page.goto(origin + `/${locale}/admin/settings`);
    await page.getByRole('heading', { name: t('config.settings'), exact: true }).waitFor();
    await page.getByLabel(t('config.wordmark'), { exact: true }).fill('');
    await button('config.preview').click();
    await page
      .locator('.config-validation')
      .getByText(t('validation.wordmark'), { exact: false })
      .waitFor();
    await page
      .locator('.config-validation')
      .getByRole('button', { name: t('config.wordmark'), exact: true })
      .click();
    assert.equal(
      await page
        .getByLabel(t('config.wordmark'), { exact: true })
        .evaluate((el) => el === document.activeElement),
      true,
    );
    assert.equal((await publicSite()).site.wordmark, 'Blog');
    await page.getByLabel(t('config.wordmark'), { exact: true }).fill('W'.repeat(80));
    await button('config.preview').click();
    await page.getByText(t('config.previewReady'), { exact: true }).waitFor();
    assert.equal((await publicSite()).site.wordmark, 'Blog');
    await page.setViewportSize({ width: 320, height: 844 });
    await shot('settings-narrow');
    await page.setViewportSize({ width: 390, height: 844 });
    await button('config.cancel').click();
    assert.equal(await page.getByLabel(t('config.wordmark'), { exact: true }).inputValue(), 'Blog');
    // Cancel also discards an upload response that arrives after the local reset.
    let releaseUpload;
    let uploaded;
    const held = new Promise((r) => (uploaded = r));
    const release = new Promise((r) => (releaseUpload = r));
    await page.route('**/api/v1/media?purpose=logo', async (route) => {
      const response = await route.fetch();
      uploaded();
      await release;
      try {
        await route.fulfill({ response });
      } catch {
        /* Cancel intentionally aborts the intercepted upload. */
      }
    });
    await page
      .getByRole('group', { name: t('config.logo'), exact: true })
      .getByLabel(t('config.upload'), { exact: true })
      .setInputFiles(resolve(root, 'backend/internal/media/testdata/poster.png'));
    await held;
    assert.equal(await button('config.save').isDisabled(), true);
    const aborted = page.waitForEvent('requestfailed', (r) =>
      r.url().endsWith('/api/v1/media?purpose=logo'),
    );
    await button('config.cancel').click();
    releaseUpload();
    await aborted;
    await page.unroute('**/api/v1/media?purpose=logo');
    assert.equal(
      await page
        .getByRole('group', { name: t('config.logo'), exact: true })
        .locator('img')
        .count(),
      0,
    );
    await page.getByLabel(t('config.wordmark'), { exact: true }).fill('Second Owner');
    await page.getByLabel(t('config.primary'), { exact: true }).fill('#004488');
    await button('config.save').click();
    await page.getByRole('status').getByText(t('config.saved'), { exact: true }).waitFor();
    let pub = await publicSite();
    assert.equal(pub.site.wordmark, 'Second Owner');
    assert.equal(pub.palette.version, 1);
    await page.evaluate(() => scrollTo(0, 0));
    await shot('settings-light');
    await page.emulateMedia({ colorScheme: 'dark' });
    await shot('settings-dark');
    await page.emulateMedia({ colorScheme: 'light' });
    const html = await (await anon.request.get(origin + '/' + locale)).text();
    assert.match(html, /id="initial-brand" nonce="[A-Za-z0-9]+"/);
    assert.ok(html.includes('--majorelle:' + pub.palette.light.majorelle));
    assert.ok(!html.includes('test-owner@example.test'));
    const draftBefore = await api('GET', '/settings');
    await page.getByLabel(t('config.wordmark'), { exact: true }).fill('Keep local edits');
    await api('PUT', '/settings', {
      expected_version: draftBefore.version,
      snapshot: { ...draftBefore.snapshot, wordmark: 'Another session' },
    });
    await button('config.save').click();
    await page.getByText(t('config.conflict'), { exact: true }).waitFor();
    assert.equal(
      await page.getByLabel(t('config.wordmark'), { exact: true }).inputValue(),
      'Keep local edits',
    );
    await button('config.reload').click();
    await page.getByLabel(t('config.wordmark'), { exact: true }).waitFor();
    await page.goto(origin + `/${locale}/admin/profile`);
    await page.getByRole('heading', { name: t('config.profile'), exact: true }).waitFor();
    const name = page.getByRole('group', { name: t('config.name'), exact: true });
    const beforeInvalidProfile = await api('GET', '/profile');
    await name.getByLabel(t('language.en'), { exact: true }).fill('');
    await name.getByLabel(t('language.ar'), { exact: true }).fill('مالك آخر');
    await button('config.preview').click();
    const validation = page.locator('.config-validation');
    await validation.getByText(t('validation.english_source'), { exact: false }).waitFor();
    assert.equal(await name.getByLabel(t('language.ar'), { exact: true }).inputValue(), 'مالك آخر');
    assert.equal((await api('GET', '/profile')).version, beforeInvalidProfile.version);
    await validation
      .getByRole('button', { name: t('config.name') + ' · ' + t('language.en'), exact: true })
      .click();
    assert.equal(
      await name
        .getByLabel(t('language.en'), { exact: true })
        .evaluate((el) => el === document.activeElement),
      true,
    );
    await name.getByLabel(t('language.en'), { exact: true }).fill('Second Owner');
    await name.getByLabel(t('language.ar'), { exact: true }).fill('مالك آخر');
    await button('config.save').click();
    await page.getByLabel(t('config.confirmReview'), { exact: true }).check();
    await button('config.save').click();
    await page.getByRole('status').getByText(t('config.saved'), { exact: true }).waitFor();
    pub = await publicSite();
    assert.equal(pub.profile.name.ar, 'مالك آخر');
    await button('language.' + locale).click();
    await shot('profile');
    // Images referenced by retained history cannot be deleted, but disabled banners remain private.
    const portraitData = await page.evaluate(() => {
      const c = document.createElement('canvas');
      c.width = c.height = 600;
      const x = c.getContext('2d');
      x.fillStyle = '#245599';
      x.fillRect(0, 0, 600, 600);
      return c.toDataURL('image/png').split(',')[1];
    });
    const photo = await upload(
      'portrait.png',
      'image/png',
      Buffer.from(portraitData, 'base64'),
      'portrait',
    );
    let profile = await api('GET', '/profile');
    let snapshot = {
      ...profile.snapshot,
      portrait_media_id: photo.id,
      portrait_alt: { en: 'Portrait' },
      banner_media_id: photo.id,
      banner_alt: { en: 'Banner' },
      banner_enabled: false,
    };
    profile = await api('PUT', '/profile', { expected_version: profile.version, snapshot });
    assert.equal((await publicSite()).profile.portrait_media_id, photo.id);
    assert.equal((await publicSite()).site.logo_media_id, '');
    await api('DELETE', '/media/' + photo.id, undefined, 409);
    await page.goto(origin + `/${locale}/admin/labels`);
    await page
      .getByRole('heading', { name: t('config.labels'), exact: true })
      .first()
      .waitFor();
    const tools = page.locator('.config-actions');
    assert.equal(await tools.locator('> *').last().textContent(), t('auth.signout'));
    assert.equal(
      await tools.getByRole('link', { name: t('auth.viewSite'), exact: true }).getAttribute('href'),
      '/' + locale,
    );
    const language = tools.getByRole('button', { name: t('foundation.language'), exact: true });
    await language.click();
    assert.equal(await page.locator('#config-language-menu a').count(), 3);
    assert.equal(
      await page
        .locator('#config-language-menu a')
        .first()
        .evaluate((n) => n === document.activeElement),
      true,
    );
    await page.keyboard.press('Escape');
    assert.equal(await language.evaluate((n) => n === document.activeElement), true);
    const bundle = {
      'nav.articles': { en: 'Notes', fr: '', ar: 'ملاحظات' },
      'custom.greeting': { en: 'Welcome' },
    };
    await page.locator('.labels-transfer > summary').click();
    await page.getByLabel(t('labels.import'), { exact: true }).setInputFiles({
      name: 'labels.json',
      mimeType: 'application/json',
      buffer: Buffer.from(JSON.stringify(bundle)),
    });
    await page.getByLabel(t('config.confirmReview'), { exact: true }).check();
    await button('config.save').click();
    await page.getByRole('status').getByText(t('config.saved'), { exact: true }).waitFor();
    assert.deepEqual((await publicSite()).labels, bundle);
    await page.getByLabel(t('labels.search'), { exact: true }).fill('nav.articles');
    await page.getByRole('table', { name: t('labels.table') }).scrollIntoViewIfNeeded();
    assert.equal(await page.locator('.vb-lrow:not(.vb-lhead)').count(), 1);
    assert.equal(
      await page
        .getByLabel('nav.articles · ' + t('language.ar'), { exact: true })
        .getAttribute('dir'),
      'rtl',
    );
    await shot('labels');
    await page.setViewportSize({ width: 1440, height: 1000 });
    await shot('labels-desktop');
    await page.setViewportSize({ width: 390, height: 844 });
    await api(
      'PUT',
      '/labels',
      { expected_version: 1, snapshot: { 'nav.articles': { en: '<script>bad</script>' } } },
      422,
    );
    await api('PUT', '/labels', { expected_version: 0, snapshot: {} }, 409);
    // The actual private preview uses exactly the shared public-reader component.
    const image = await upload(
      'poster.png',
      'image/png',
      await readFile(resolve(root, 'backend/internal/media/testdata/poster.png')),
    );
    const video = await upload(
      'clip.mp4',
      'video/mp4',
      await readFile(resolve(root, 'backend/internal/media/testdata/clip.mp4')),
    );
    const track = await upload(
      'captions.vtt',
      'text/vtt',
      Buffer.from('WEBVTT\n\n00:00.000 --> 00:01.000\nCaption\n'),
    );
    await api(
      'PUT',
      '/content/reader-sample/' + locale,
      {
        expected_version: 0,
        kind: 'article',
        source_locale: locale,
        slug: 'reader-sample',
        title: 'Reader sample',
        markdown:
          '# Reading\n\n**Takeaway:** Learn carefully.\n\n| Name | Value |\n|---|---|\n| x | 2 |\n\n```js\nconst x = 1;\n```\n\n$x^2 + 1$\n\n![Blue square](' +
          image.url +
          ')\n\n[Clip](' +
          video.url +
          ')',
        media_presentations: [
          {
            media_id: video.id,
            poster_media_id: image.id,
            caption_track_media_id: track.id,
            caption: 'Clip',
            transcript: 'A short blue clip.',
          },
        ],
      },
      201,
    );
    await page.goto(origin + `/${locale}/admin/preview/reader-sample/${locale}`);
    await page.locator('math').waitFor();
    assert.equal(await page.locator('table').count(), 1);
    assert.equal(await page.locator('video track').count(), 1);
    assert.equal(await page.locator('.vb-code').getAttribute('dir'), 'ltr');
    await shot('reader');
    assert.equal(
      (
        await anon.request.get(origin + `/${locale}/admin/preview/reader-sample/${locale}`)
      ).status(),
      401,
    );
    assert.equal((await anon.request.get(origin + image.url)).status(), 404);
    assert.deepEqual(errors, []);
    console.log(
      `${locale}: configuration preview/cancel/apply/conflicts, reviewed profile/labels, initial CSS and shared Markdown browser rendering passed.`,
    );
  } finally {
    await page.close();
    await anon.close();
  }
}
