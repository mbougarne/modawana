import { acceptanceCases, smokeSuite } from './smoke-policy.mjs';
import assert from 'node:assert/strict';
import { createRequire } from 'node:module';
import { writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';

const require = createRequire(resolve(import.meta.dirname, '../frontend/package.json'));
const { default: AxeBuilder } = require('@axe-core/playwright');

export async function smokeAcceptance({ context, origin, root, locale }) {
  const anon = await context.browser().newContext({ ignoreHTTPSErrors: true });
  const roles = [
    ['home', '', false],
    ['writing', '/writing', false],
    ['article', '/articles/public-article', false],
    ['learning', '/learn/public-path', false],
    ['lesson-quiz', `/learn/progress-${locale}/progress-${locale}-1`, false],
    ['locked', `/learn/progress-${locale}/progress-${locale}-4`, false],
    ['docs', '/docs/public-guide/public-guide-one', false],
    ['about', '/about', false],
    ['search', '/search?q=phasefive', false],
    ['editor', `/admin/editor/editor-${locale}/${locale}`, true],
    ['settings', '/admin/settings', true],
    ['profile', '/admin/profile', true],
    ['labels', '/admin/labels', true],
    ['sign-in', '/login', false],
    ['posts', '/admin/posts', true],
    ['media', '/admin/media', true],
    ['collections', '/admin/collections', true],
    ['security', '/admin/security', true],
    ['transfer', '/admin/transfer', true],
  ];
  const evidence = [];
  try {
    for (const [role, path, owner] of roles) {
      const page = await (owner ? context : anon).newPage();
      try {
        await page.addInitScript(() => localStorage.removeItem('theme'));
        await page.goto(origin + '/' + locale + path);
        await page.locator('main').waitFor();
        await page.waitForLoadState('networkidle');
        for (const { width, theme } of acceptanceCases()) {
          await page.setViewportSize({ width, height: 900 });
          await page.emulateMedia({ colorScheme: theme });
          await page.evaluate(() => new Promise((r) => requestAnimationFrame(r)));
          assert.equal(
            await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth),
            true,
            `${locale}/${role}/${width}/${theme} overflow`,
          );
          assert.equal(
            await page.locator('html').getAttribute('dir'),
            locale === 'ar' ? 'rtl' : 'ltr',
          );
          if (width === 320) {
            const result = await new AxeBuilder({ page })
              .withTags(['wcag2a', 'wcag2aa', 'wcag21aa', 'wcag22aa'])
              .analyze();
            const violations = result.violations.map((v) => ({
              id: v.id,
              impact: v.impact,
              nodes: v.nodes.map((n) => ({ target: n.target, reason: n.failureSummary })),
            }));
            evidence.push({ role, locale, width, theme, violations });
            assert.deepEqual(violations, [], `${locale}/${role}/${theme} accessibility`);
            if (theme === 'light') {
              await page.keyboard.press('Tab');
              assert.equal(
                await page.evaluate(() => document.activeElement !== document.body),
                true,
                'keyboard focus reaches a control',
              );
              if (await page.evaluate(() => document.activeElement?.matches('.vb-skip'))) {
                const focused = await new AxeBuilder({ page })
                  .include('.vb-skip')
                  .withRules(['color-contrast'])
                  .analyze();
                assert.deepEqual(focused.violations, [], 'focused skip link has readable contrast');
              }
              await page.screenshot({
                path: resolve(root, `frontend/test-results/acceptance-${locale}-${role}.png`),
                fullPage: true,
              });
              await page.evaluate(() => document.activeElement?.blur());
            }
          }
        }
      } finally {
        await page.close();
      }
    }
    console.log(
      `${locale}: 14 reference roles plus Posts/Media/Collections/Security/Transfer passed ${smokeSuite()} viewport/theme cases, keyboard-focus and automated WCAG checks.`,
    );
  } finally {
    await writeFile(
      resolve(root, `frontend/test-results/acceptance-${locale}.json`),
      JSON.stringify(evidence, null, 2),
    );
    await anon.close();
  }
}
