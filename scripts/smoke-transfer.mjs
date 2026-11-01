import assert from 'node:assert/strict';
import { resolve } from 'node:path';
import { confirmAction } from './smoke-dialog.mjs';

export async function smokeTransfer({ context, origin, root, locale, t }) {
  const page = await context.newPage();
  try {
    await page.goto(origin + `/${locale}/admin/transfer`);
    await page.getByRole('heading', { name: t('owner.transfer'), exact: true }).waitFor();
    const download = page.waitForEvent('download');
    await page.getByRole('button', { name: t('transfer.export'), exact: true }).click();
    const file = await download;
    assert.equal(file.suggestedFilename(), 'blog-content.zip');
    const path = await file.path();
    await page.getByLabel(t('transfer.file'), { exact: true }).setInputFiles(path);
    await confirmAction(page, t('transfer.confirm'), () =>
      page.getByRole('button', { name: t('transfer.import'), exact: true }).click(),
    );
    await page
      .getByRole('status')
      .filter({ hasText: t('transfer.empty') })
      .waitFor();
    for (const width of [320, 390, 1280]) {
      await page.setViewportSize({ width, height: 900 });
      assert.equal(
        await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth),
        true,
      );
      await page.screenshot({
        path: resolve(root, `frontend/test-results/transfer-${locale}-${width}.png`),
        fullPage: true,
      });
    }
    const anon = await context.browser().newContext({ ignoreHTTPSErrors: true });
    assert.equal(
      (
        await anon.request.post(origin + '/api/v1/portability/export', {
          headers: { Origin: origin },
        })
      ).status(),
      401,
    );
    await anon.close();
    console.log(
      locale +
        ': private content export, populated-target import rejection and responsive transfer UI passed.',
    );
  } finally {
    await page.close();
  }
}
