import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { resolve } from 'node:path';

export async function smokePublic({ context, origin, root, locale, t }) {
  const { csrf } = await (await context.request.get(origin + '/api/v1/session')).json();
  const headers = { Origin: origin, 'X-CSRF-Token': csrf };
  const anon = await context.browser().newContext({ ignoreHTTPSErrors: true });
  const page = await anon.newPage();
  page.setDefaultTimeout(15000);
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
  async function api(method, path, data, status = 200) {
    const r = await context.request.fetch(origin + '/api/v1/' + path, { method, headers, data });
    assert.equal(r.status(), status, await r.text());
    return r.json();
  }
  async function save(key, kind = 'article', extra = {}) {
    const r = await api(
      'PUT',
      `content/${key}/${locale}`,
      {
        expected_version: 0,
        kind,
        source_locale: locale,
        slug: key,
        title: 'Public ' + key,
        summary: 'phasefive ' + key,
        markdown:
          '# Reading fixture\n\nA published paragraph.\n\n```go\nif ready {\n  run()\n}\n```\n\n> Authored takeaway.\n\n| A | B |\n|---|---|\n| one | two |',
        ...extra,
      },
      201,
    );
    return r;
  }
  async function publish(key, r) {
    return api('POST', `content/${key}/${locale}/publish`, {
      expected_version: r.version,
      revision_id: r.revision_id,
    });
  }
  async function shot(name, width = 390, theme = 'light') {
    await page.setViewportSize({ width, height: 900 });
    await page.emulateMedia({ colorScheme: theme, reducedMotion: 'reduce' });
    await page.evaluate(() => document.fonts.ready);
    assert.ok(
      await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth),
      `overflow ${locale}/${name}/${width}`,
    );
    await page.screenshot({
      path: resolve(root, `frontend/test-results/public-${locale}-${name}-${width}-${theme}.png`),
      fullPage: true,
    });
  }
  try {
    await api('PUT', `taxonomy/category/public-code/${locale}`, {
      expected_version: 0,
      label: 'Public code',
      slug: 'public-code',
    });
    await api('PUT', `taxonomy/tag/public-go/${locale}`, {
      expected_version: 0,
      label: 'Public Go',
      slug: 'public-go',
    });
    const image = await context.request.post(origin + '/api/v1/media', {
      headers,
      multipart: {
        file: {
          name: 'cover.png',
          mimeType: 'image/png',
          buffer: await readFile(resolve(root, 'backend/internal/media/testdata/poster.png')),
        },
      },
    });
    assert.equal(image.status(), 201);
    const photo = await image.json();
    const article = await save('public-article', 'article', {
      category_key: 'public-code',
      tag_keys: ['public-go'],
      markdown:
        '# Reading fixture\n\nPublished body with an author-written takeaway.\n\n> Keep immutable revisions.\n\n```go\nif ready {\n  run()\n}\n```\n\n![Diagram](' +
        photo.url +
        ')',
    });
    await publish('public-article', article);
    const other = await save('public-other');
    await publish('public-other', other);
    await save('private-article');
    // A draft edit must not alter the published page.
    await api('PUT', `content/public-article/${locale}`, {
      expected_version: 2,
      kind: 'article',
      source_locale: locale,
      slug: 'public-article',
      title: 'PRIVATE TITLE',
      summary: 'PRIVATE SUMMARY',
      markdown: 'PRIVATE BODY',
    });
    for (const [key, kind, contentKind] of [
      ['public-path', 'learning', 'lesson'],
      ['public-guide', 'docs', 'doc'],
    ]) {
      await api('PUT', `collections/${key}/${locale}`, {
        expected_version: 0,
        kind,
        slug: key,
        title: 'Public ' + key,
        summary: 'An ordered collection',
        visible: true,
      });
      const members = [];
      for (const [i, suffix] of ['one', 'two', 'soon', 'hidden'].entries()) {
        const itemKey = key + '-' + suffix;
        const revision = await save(itemKey, contentKind, { collection_key: key, position: i + 1 });
        if (i < 2) {
          await publish(itemKey, revision);
        }
        members.push({
          key: itemKey,
          announcements: i === 2 ? { [locale]: 'Announced topic' } : {},
        });
      }
      const c = await api('GET', `collections/${key}`);
      const plan = await api('PUT', `collections/${key}/plan`, {
        expected_version: c.version,
        members,
      });
      await api('POST', `collections/${key}/apply`, {
        expected_version: plan.version,
        plan_id: plan.plan_id,
      });
    }
    const profile = await api('GET', 'profile');
    const snapshot = {
      ...profile.snapshot,
      about_key: 'public-guide-one',
      headline: { en: 'Building *thoughtfully*.' },
      bio: { en: 'A **second owner** profile for browser verification.' },
      show_writing: true,
      show_learning: true,
      show_now: true,
      now: [{ id: 'now', label: { en: 'Building' }, text: { en: 'A personal publishing space' } }],
      updated_month: '2026-10',
      experience: [
        {
          id: 'work',
          role: { en: 'Engineer' },
          organization: { en: 'Example studio' },
          description: { en: 'An explicit test fixture, never production seed data.' },
          start: '2024-01',
          end: '',
          current: true,
          url: '',
        },
      ],
      public_email: 'reader@example.test',
    };
    await api('PUT', 'profile', { expected_version: profile.version, snapshot });
    const url = `/${locale}/articles/public-article`;
    const html = await anon.request.get(origin + url);
    assert.equal(html.status(), 200);
    const source = await html.text();
    assert.ok(source.includes('<title>Public public-article</title>'));
    assert.ok(source.includes('rel="canonical"'));
    assert.ok(!source.includes('PRIVATE TITLE') && !source.includes('PRIVATE BODY'));
    assert.equal(html.headers()['cache-control'], 'no-store');
    assert.equal(
      (await anon.request.get(origin + `/${locale}/articles/private-article`)).status(),
      404,
    );
    assert.equal(
      (await anon.request.get(origin + `/${locale}/articles/public-path-one`)).status(),
      404,
    );
    // ADR 0002 stage 2: a full load of the article URL is served from Go
    // templates with no SPA mount point; SPA-internal navigation to the same
    // article (exercised below from the search page) keeps using React.
    assert.ok(source.includes('data-rendered="server"'));
    assert.ok(!source.includes('id="root"'));
    await page.goto(origin + url);
    await page.locator('.vb-prose').waitFor();
    assert.equal(await page.evaluate(() => document.body.dataset.rendered), 'server');
    assert.equal(await page.locator('#root').count(), 0);
    assert.equal(
      await page.locator('script[src]').count(),
      0,
      'math-free server article must ship no rendering JavaScript',
    );
    assert.ok(
      (await page.locator('.vb-art-head .vb-sticker').textContent()).includes('Public code'),
    );
    assert.equal(await page.locator('.vb-reader img').getAttribute('alt'), 'Diagram');
    await page.locator('.vb-author').waitFor();
    assert.ok(!(await page.locator('main').textContent()).includes('PRIVATE'));
    for (const width of [320, 390, 1280]) {
      for (const theme of ['light', 'dark']) {
        await shot('article-server', width, theme);
      }
    }
    // Math articles load exactly one script: the manifest-listed KaTeX island.
    const math = await save('public-math', 'article', {
      markdown: 'Euler: $e^{i\\pi}+1=0$',
    });
    await publish('public-math', math);
    await page.goto(origin + `/${locale}/articles/public-math`);
    await page.locator('.vb-math math').waitFor();
    assert.equal(await page.locator('script[src]').count(), 1);
    await page.goto(origin + '/' + locale);
    await page.locator('.public-hero h1').waitFor();
    assert.ok((await page.locator('.public-hero').textContent()).includes('second owner'));
    for (const width of [320, 390, 1280]) {
      for (const theme of ['light', 'dark']) {
        await shot('home', width, theme);
      }
    }
    assert.equal(
      await page.locator('.public-header > .vb-brand').getAttribute('href'),
      '/' + locale,
    );
    assert.equal(await page.locator('.public-header > .vb-brand [aria-expanded]').count(), 0);
    assert.equal(await page.locator('.public-nav svg').count(), 0);
    assert.deepEqual(
      await page
        .locator('.public-tools > *')
        .evaluateAll((nodes) => nodes.slice(0, 3).map((n) => n.getAttribute('aria-label'))),
      [t('nav.search'), t('foundation.language'), t('nav.theme')],
    );
    await page.locator('.public-language').click();
    assert.equal(await page.locator('.public-nav.vb-nav a').count(), 4);
    assert.equal(await page.locator('.public-language').getAttribute('aria-expanded'), 'true');
    assert.equal(
      await page
        .locator('#language-menu a')
        .first()
        .evaluate((node) => node === document.activeElement),
      true,
    );
    assert.equal(await page.locator('#language-menu a[href*="/admin"]').count(), 0);
    await page.keyboard.press('Escape');
    assert.equal(
      await page.locator('.public-language').evaluate((n) => n === document.activeElement),
      true,
    );
    await page.setViewportSize({ width: 390, height: 900 });
    await page.locator('.public-tools .vb-menu').click();
    assert.equal(await page.locator('#navigation-menu a').count(), 5);
    assert.equal(await page.locator('#navigation-menu svg').count(), 0);
    await page.keyboard.press('Escape');
    assert.equal(
      await page.locator('.public-tools .vb-menu').evaluate((n) => n === document.activeElement),
      true,
    );
    await page.setViewportSize({ width: 1280, height: 900 });
    await page.goto(origin + `/${locale}/about`);
    await page.locator('.vb-reader').waitFor();
    assert.ok((await page.locator('main').textContent()).includes('Example studio'));
    await shot('about');
    await page.goto(
      origin + `/${locale}/writing?q=phasefive&kind=article&category=public-code&tag=public-go`,
    );
    await page.locator('.vb-post').waitFor();
    assert.equal(await page.locator('.vb-post').count(), 1);
    assert.ok(!(await page.locator('main').textContent()).includes('PRIVATE'));
    const tag = page.locator('.public-filters.tag button').filter({ hasText: 'Public Go' });
    await tag.click();
    assert.ok(!new URL(page.url()).searchParams.has('tag'));
    await page.goBack();
    await page.locator('.vb-post').waitFor();
    assert.equal(new URL(page.url()).searchParams.get('tag'), 'public-go');
    await shot('writing');
    // Search lives on its own page and updates as the reader types.
    const search = new URL(page.url());
    search.pathname = `/${locale}/search`;
    await page.goto(search.href);
    await page.locator('input[name="q"]').waitFor();
    // An older, delayed query must not paint over the current URL/results.
    let unblock;
    let saw;
    const held = new Promise((r) => {
      saw = r;
    });
    const gate = new Promise((r) => {
      unblock = r;
    });
    await page.route('**/api/v1/public/content?**', async (route) => {
      if (new URL(route.request().url()).searchParams.get('q') === 'slow') {
        saw();
        await gate;
      }
      try {
        await route.continue();
      } catch {
        /* Navigation can cancel an older request. */
      }
    });
    await page.locator('input[name="q"]').fill('slow');
    await held;
    await page.locator('input[name="q"]').fill('phasefive');
    await page.waitForURL((url) => url.searchParams.get('q') === 'phasefive');
    await page.locator('.vb-post').waitFor();
    unblock();
    await page.unroute('**/api/v1/public/content?**');
    assert.equal(new URL(page.url()).searchParams.get('q'), 'phasefive');
    await shot('search');
    // Archive rows are plain anchors, so this click is a full load that now
    // lands on the server-rendered article.
    await page.locator('.vb-post a').filter({ hasText: 'Public public-article' }).click();
    await page.locator('.vb-reader pre').waitFor();
    assert.equal(await page.evaluate(() => document.body.dataset.rendered), 'server');
    assert.equal(await page.locator('.vb-reader img').getAttribute('alt'), 'Diagram');
    assert.ok(!(await page.locator('main').textContent()).includes('PRIVATE BODY'));
    await shot('article', 320);
    await shot('article', 1280, 'dark');
    // The server-rendered language menu is a native <details>: missing
    // translations offer an explicit home link in that site language.
    const missing = ['en', 'ar', 'fr'].find((l) => l !== locale);
    await page.locator('.public-language').click();
    assert.equal(
      await page.locator(`.public-details .public-menu nav a[href="/${missing}"]`).count(),
      1,
    );
    await page.goto(origin + `/${locale}/learn/public-path`);
    await page.locator('.public-outline li').first().waitFor();
    assert.equal(await page.locator('.public-outline li').count(), 3);
    assert.equal(await page.locator('.public-outline li').nth(2).locator('a').count(), 0);
    assert.ok(!(await page.locator('main').textContent()).includes('hidden'));
    await shot('path');
    await page.locator('.public-outline a').filter({ hasText: 'Public public-path-one' }).click();
    await page.locator('.vb-reader').waitFor();
    assert.equal(
      await page.locator('.public-neighbors a').getAttribute('href'),
      `/${locale}/learn/public-path/public-path-two`,
    );
    // Lessons stay on the SPA, where a missing translation shows the
    // explicit notice with site-language choices.
    await page.locator('.public-language').click();
    await page
      .locator('#language-menu')
      .getByRole('button', { name: t('language.' + missing), exact: true })
      .click();
    await page.getByText(t('public.unavailable'), { exact: true }).waitFor();
    await page.keyboard.press('Escape');
    await shot('lesson', 320);
    await page.goto(origin + `/${locale}/docs/public-guide/public-guide-one`);
    await page.locator('.vb-reader').waitFor();
    assert.equal(await page.locator('.public-outline a[aria-current="page"]').count(), 1);
    await shot('doc');
    const otherLocale = locale === 'ar' ? 'fr' : 'ar';
    assert.equal(
      (await anon.request.get(origin + `/${otherLocale}/learn/public-path`)).status(),
      404,
    );
    // Explicit error and recovery state on a failed metadata request.
    await page.route('**/api/v1/public/content?**', (route) =>
      route.fulfill({ status: 503, json: { error: { code: 'unavailable' } } }),
    );
    await page.goto(origin + `/${locale}/writing`);
    await page.getByRole('alert').waitFor();
    await page.unroute('**/api/v1/public/content?**');
    await page.getByRole('button', { name: t('public.retry'), exact: true }).click();
    await page.locator('.vb-post').first().waitFor();
    if (locale === 'en') {
      for (let i = 0; i < 21; i++) {
        const key = 'page-item-' + String(i).padStart(2, '0');
        const saved = await save(key, 'article', { summary: 'pagination-only' });
        await publish(key, saved);
      }
      await page.goto(origin + '/en/writing?q=pagination-only');
      await page.getByRole('button', { name: t('public.next'), exact: true }).waitFor();
      assert.equal(await page.locator('.vb-post').count(), 20);
      const firstTitle = await page.locator('.vb-post-main b').first().textContent();
      await page.getByRole('button', { name: t('public.next'), exact: true }).click();
      await page.waitForFunction(() => document.querySelectorAll('.vb-post').length === 1);
      assert.ok(new URL(page.url()).searchParams.has('cursor'));
      await page.getByRole('button', { name: t('public.previous'), exact: true }).click();
      await page.waitForFunction(() => document.querySelectorAll('.vb-post').length === 20);
      assert.equal(new URL(page.url()).searchParams.has('cursor'), false);
      assert.equal(new URL(page.url()).searchParams.get('q'), 'pagination-only');
      assert.equal(await page.locator('.vb-post-main b').first().textContent(), firstTitle);
    }
    assert.deepEqual(errors, []);
    console.log(
      `${locale}: public profile, archive filters/Back/stale requests, readers, metadata, private isolation, outlines, missing translations, retry and mobile/theme visuals passed.`,
    );
  } finally {
    await anon.close();
  }
}
