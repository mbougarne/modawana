import assert from 'node:assert/strict';
import { confirmAction } from './smoke-dialog.mjs';
import { readFile } from 'node:fs/promises';
import { resolve } from 'node:path';

export async function smokeLearning({ context, origin, root, locale, t }) {
  const { csrf } = await (await context.request.get(origin + '/api/v1/session')).json();
  const headers = { Origin: origin, 'X-CSRF-Token': csrf };
  async function request(method, path, data) {
    const r = await context.request[method](origin + '/api/v1' + path, { headers, data });
    assert.ok(r.ok(), `${path}: ${r.status()} ${await r.text()}`);
    return r.json();
  }
  const course = 'progress-' + locale;
  await request('put', `/collections/${course}/${locale}`, {
    expected_version: 0,
    kind: 'learning',
    slug: course,
    title: 'Progress path',
    visible: true,
  });
  const upload = await context.request.post(origin + '/api/v1/media', {
    headers,
    multipart: {
      file: {
        name: 'lesson.mp4',
        mimeType: 'video/mp4',
        buffer: await readFile(resolve(root, 'backend/internal/media/testdata/clip.mp4')),
      },
    },
  });
  assert.equal(upload.status(), 201);
  const media = await upload.json();
  const quiz = {
    pass_mark: 1,
    show_explanations: true,
    questions: [
      {
        id: 'q',
        type: 'single',
        text: 'Choose A',
        options: [
          { id: 'a', text: 'Option A' },
          { id: 'b', text: 'Option B' },
        ],
        correct: ['a'],
        explanation: 'Withheld explanation',
      },
    ],
  };
  const keys = [1, 2, 3, 4].map((n) => course + '-' + n);
  for (const [i, key] of keys.entries()) {
    const draft = await request('put', `/content/${key}/${locale}`, {
      expected_version: 0,
      kind: 'lesson',
      source_locale: locale,
      collection_key: course,
      position: i + 1,
      slug: key,
      title: 'Lesson ' + (i + 1),
      summary: 'Safe teaser',
      markdown:
        i === 3 ? `PROTECTED FOURTH\n\n[Video](${media.url})` : 'Readable lesson ' + (i + 1),
      ...(i === 0 || i === 2 ? { quiz } : {}),
    });
    if (i === 0 || i === 2) {
      await request('post', `/content/${key}/${locale}/quiz-review`, {
        expected_version: draft.version,
        revision_id: draft.revision_id,
      });
    }
    await request('post', `/content/${key}/${locale}/publish`, {
      expected_version: draft.version,
      revision_id: draft.revision_id,
    });
  }
  let doc = await request('get', '/collections/' + course);
  let plan = await request('put', `/collections/${course}/plan`, {
    expected_version: doc.version,
    members: keys.map((key) => ({ key, announcements: {} })),
  });
  for (const key of [keys[0], keys[2]]) {
    const draft = await request('get', `/content/${key}/${locale}`);
    plan = await request('put', `/content/${key}/${locale}/quiz-gate`, {
      expected_version: plan.version,
      plan_id: plan.plan_id,
      enabled: true,
      assessment_id: draft.revision.quiz.assessment_id,
    });
  }
  await request('post', `/collections/${course}/apply`, {
    expected_version: plan.version,
    plan_id: plan.plan_id,
  });
  const reader = await context.browser().newContext({ ignoreHTTPSErrors: true });
  const page = await reader.newPage();
  const errors = [];
  page.on('pageerror', (e) => errors.push(e.message));
  try {
    const base = `/${locale}/learn/${course}/`;
    for (const path of [
      `/api/v1/public/content/${locale}/${keys[3]}`,
      '/api/v1/public/page?path=' + encodeURIComponent(base + keys[3]),
      `/api/v1/learning/content/${keys[3]}/${locale}/quiz`,
    ]) {
      const r = await reader.request.get(origin + path);
      assert.equal(r.status(), 403);
      assert.doesNotMatch(await r.text(), /PROTECTED FOURTH|Withheld explanation|correctOptionIds/);
    }
    for (const method of ['get', 'head']) {
      assert.equal(
        (
          await reader.request[method](origin + media.url, { headers: { Range: 'bytes=0-9' } })
        ).status(),
        404,
      );
    }
    assert.equal((await page.goto(origin + base + keys[3])).status(), 403);
    await page.getByText(t('progress.prerequisite'), { exact: true }).waitFor();
    assert.equal(await page.locator('meta[name="robots"]').getAttribute('content'), 'noindex');
    assert.doesNotMatch(await page.content(), /PROTECTED FOURTH|Withheld explanation/);
    await page.setViewportSize({ width: 390, height: 844 });
    await page.screenshot({
      path: resolve(root, `frontend/test-results/learning-${locale}-locked.png`),
      fullPage: true,
    });
    await page.goto(origin + base + keys[0]);
    const ungraded = await reader.request.get(
      origin + `/api/v1/learning/content/${keys[0]}/${locale}/quiz`,
    );
    assert.equal(ungraded.status(), 200);
    assert.doesNotMatch(await ungraded.text(), /Withheld explanation|"correct"|correctOptionIds/);
    assert.doesNotMatch(await page.content(), /Withheld explanation/);
    await page.getByRole('radio', { name: 'Option B', exact: true }).check();
    await page.getByRole('button', { name: t('quiz.check'), exact: true }).click();
    await page.locator('.vb-result[data-pass="false"]').waitFor();
    await page.getByRole('button', { name: t('quiz.retry'), exact: true }).click();
    await page.getByRole('radio', { name: 'Option A', exact: true }).check();
    await page.getByRole('button', { name: t('quiz.check'), exact: true }).click();
    await page.locator('.vb-result[data-pass="true"]').waitFor();
    await page.getByRole('link', { name: new RegExp(t('quiz.continue')) }).waitFor();
    await page.screenshot({
      path: resolve(root, `frontend/test-results/learning-${locale}-passed.png`),
      fullPage: true,
    });
    assert.equal(
      (await reader.request.get(origin + base + keys[3])).status(),
      403,
      'all earlier gates required',
    );
    await page.goto(origin + base + keys[1]);
    await page.getByRole('button', { name: t('progress.markRead'), exact: true }).click();
    await page
      .locator('.public-quiz > [role="status"]')
      .filter({ hasText: t('progress.completed') })
      .waitFor();
    await page.goto(origin + base + keys[2]);
    await page.getByRole('radio', { name: 'Option A', exact: true }).check();
    await page.getByRole('button', { name: t('quiz.check'), exact: true }).click();
    await page.locator('.vb-result[data-pass="true"]').waitFor();
    await page.goto(origin + base + keys[3]);
    await page.getByText('PROTECTED FOURTH', { exact: true }).waitFor();
    assert.equal(
      (await reader.request.get(origin + media.url, { headers: { Range: 'bytes=0-9' } })).status(),
      206,
    );
    await page.reload();
    await page.getByText('PROTECTED FOURTH', { exact: true }).waitFor();
    await page.setViewportSize({ width: 1280, height: 900 });
    await page.screenshot({
      path: resolve(root, `frontend/test-results/learning-${locale}-open.png`),
      fullPage: true,
    });
    assert.equal(
      await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth),
      true,
    );
    await confirmAction(page, t('progress.confirm'), () =>
      page.getByRole('button', { name: t('progress.reset'), exact: true }).click(),
    );
    await page.getByText(t('progress.prerequisite'), { exact: true }).waitFor();
    assert.doesNotMatch(await page.content(), /PROTECTED FOURTH/);
    assert.deepEqual(errors, []);
    console.log(
      `${locale}: locked/media denial, fail/retry, ordered gates, mark read, persisted completion and browser reset passed.`,
    );
  } finally {
    await reader.close();
  }
}
