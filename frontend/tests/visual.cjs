const { chromium } = require('playwright');
const fs = require('node:fs/promises');
const path = require('node:path');
const assert = require('node:assert/strict');
(async () => {
  const root = path.resolve(__dirname, '../dist');
  const base = process.env.BLOG_VISUAL_BASE_URL || 'http://blog.test';
  const output = path.resolve(__dirname, '../test-results');
  await fs.mkdir(output, { recursive: true });
  const browser = await chromium.launch({ headless: true });
  try {
    const errors = [];
    const page = await browser.newPage();
    page.on('pageerror', (e) => errors.push(e.message));
    page.on('console', (m) => {
      if (
        m.type() === 'error' &&
        /Content Security Policy|Refused to|violates.*directive/i.test(m.text())
      ) {
        errors.push(m.text());
      }
    });
    if (!process.env.BLOG_VISUAL_BASE_URL) {
      await page.route('http://blog.test/**', async (route) => {
        const pathname = new URL(route.request().url()).pathname;
        const file = pathname.startsWith('/assets/')
          ? path.join(root, pathname)
          : path.join(root, 'index.html');
        const extension = path.extname(file);
        const contentType =
          {
            '.html': 'text/html',
            '.js': 'application/javascript',
            '.css': 'text/css',
            '.woff2': 'font/woff2',
          }[extension] || 'application/octet-stream';
        try {
          await route.fulfill({ status: 200, contentType, body: await fs.readFile(file) });
        } catch {
          await route.fulfill({ status: 404, body: '' });
        }
      });
    }
    const { visualCases } = await import('../../scripts/smoke-policy.mjs');
    const cases = visualCases();
    for (const { locale, width, theme } of cases) {
      await page.setViewportSize({ width, height: 900 });
      await page.emulateMedia({ colorScheme: theme, reducedMotion: 'reduce' });
      await page.goto(base + '/' + locale);
      await page.locator('h1').waitFor();
      await page.evaluate(() => document.fonts.ready);
      assert.equal(await page.locator('html').getAttribute('lang'), locale);
      assert.equal(await page.locator('html').getAttribute('dir'), locale === 'ar' ? 'rtl' : 'ltr');
      assert.ok(
        await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth),
        `overflow: ${locale}/${width}/${theme}`,
      );
      const header = page.locator('.public-header');
      const language = header.locator('button[aria-controls="language-menu"]');
      await language.click();
      const languageMenu = page.locator('#language-menu');
      assert.equal(await language.getAttribute('aria-expanded'), 'true');
      assert.equal(await languageMenu.getByRole('link').count(), 3);
      assert.equal(
        await languageMenu.locator('[lang="' + locale + '"]').textContent(),
        { en: 'English', ar: 'العربية', fr: 'Français' }[locale],
      );
      assert.equal(await languageMenu.locator('a[href*="/admin"]').count(), 0);
      await page.keyboard.press('Escape');
      assert.equal(await language.getAttribute('aria-expanded'), 'false');
      assert.equal(await languageMenu.count(), 0);
      assert.equal(await language.evaluate((n) => n === document.activeElement), true);
      assert.equal(await header.locator('a.vb-brand').getAttribute('href'), '/' + locale);
      assert.equal(await header.locator('a.vb-brand [aria-expanded]').count(), 0);
      if (width < 768) {
        const navigation = header.locator('button[aria-controls="navigation-menu"]');
        await navigation.click();
        const navigationMenu = page.locator('#navigation-menu');
        assert.equal(await navigationMenu.getByRole('link').count(), 5);
        assert.equal(await navigationMenu.locator('a[href*="/admin"]').count(), 0);
        await page.keyboard.press('Escape');
        assert.equal(await navigation.getAttribute('aria-expanded'), 'false');
        assert.equal(await navigationMenu.count(), 0);
        assert.equal(await navigation.evaluate((n) => n === document.activeElement), true);
      } else {
        assert.equal(await header.locator('.public-nav a').count(), 4);
      }
      await page.screenshot({
        path: path.join(output, `${locale}-${width}-${theme}.png`),
        fullPage: true,
      });
    }
    assert.deepEqual(errors, []);
    console.log(`${cases.length} locale/viewport/theme combinations passed; screenshots captured.`);
  } finally {
    await browser.close();
  }
})().catch((e) => {
  console.error(e);
  process.exitCode = 1;
});
