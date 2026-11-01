import assert from 'node:assert/strict';
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { writeFile, readFile } from 'node:fs/promises';
import { resolve } from 'node:path';

const execute = promisify(execFile);

export async function smokePublisher({ context, origin, root, locale, t, cert, data }) {
  const page = await context.newPage();
  const { csrf } = await (await context.request.get(origin + '/api/v1/session')).json();
  const collection = 'publishing-' + locale;
  const created = await context.request.put(
    origin + `/api/v1/collections/${collection}/${locale}`,
    {
      headers: { Origin: origin, 'X-CSRF-Token': csrf },
      data: { expected_version: 0, kind: 'learning', title: 'Publishing test', slug: collection },
    },
  );
  assert.ok(created.ok(), await created.text());
  try {
    await page.goto(origin + `/${locale}/admin/security`);
    await page.getByRole('button', { name: t('auth.reauth'), exact: true }).click();
    await page.getByRole('heading', { name: t('publisher.title'), exact: true }).waitFor();
    await page.getByLabel(t('publisher.label'), { exact: true }).fill('Scoped test runner');
    await page.getByLabel(t('publisher.collections'), { exact: true }).fill(collection);
    for (const scope of ['media', 'quiz', 'publish']) {
      await page.getByLabel(t('publisher.' + scope), { exact: true }).check();
    }
    await page.getByRole('button', { name: t('publisher.create'), exact: true }).click();
    const secretField = page.getByLabel(t('publisher.secret'), { exact: true });
    await secretField.waitFor();
    const token = await secretField.inputValue();
    assert.match(token, /^[A-Za-z0-9_-]{43}$/);
    await page.getByRole('button', { name: t('publisher.dismiss'), exact: true }).click();
    await page.setViewportSize({ width: 390, height: 844 });
    await page.screenshot({
      path: resolve(root, `frontend/test-results/publisher-${locale}.png`),
      fullPage: true,
    });
    const env = {
      ...process.env,
      BLOG_URL: origin,
      BLOG_PUBLISHER_TOKEN: token,
      BLOG_PUBLISHER_CA: cert,
    };
    async function cli(args, expected = 0) {
      let stdout;
      let stderr;
      let code = 0;
      try {
        ({ stdout, stderr } = await execute(resolve(root, 'build/publisher'), args, {
          env,
          timeout: 30000,
        }));
      } catch (e) {
        ({ stdout, stderr, code } = e);
      }
      assert.equal(code, expected, `publisher command status: ${stderr}`);
      assert.ok(
        !String(stdout).includes(token) && !String(stderr).includes(token),
        'token exposed',
      );
      return expected ? { stdout, stderr } : JSON.parse(stdout);
    }
    const file = resolve(root, 'backend/internal/media/testdata/clip.mp4');
    const mediaArgs = [
      'upload',
      '--file',
      file,
      '--mime',
      'video/mp4',
      '--idempotency',
      'upload-one',
    ];
    const media = await cli(mediaArgs);
    assert.equal((await cli(mediaArgs)).id, media.id, 'upload duplicated on retry');
    const key = 'cli-' + locale;
    const draftFile = resolve(data, 'cli-draft.json');
    const draft = {
      expected_version: 0,
      kind: 'lesson',
      source_locale: locale,
      collection_key: collection,
      position: 1,
      slug: key,
      title: 'CLI lesson',
      markdown: `[Clip](${media.url})`,
    };
    await writeFile(draftFile, JSON.stringify(draft), { mode: 0o600 });
    const draftArgs = [
      'draft',
      '--key',
      key,
      '--locale',
      locale,
      '--input',
      draftFile,
      '--idempotency',
      'draft-one',
    ];
    const saved = await cli(draftArgs);
    assert.equal((await cli(draftArgs)).revision_id, saved.revision_id);
    const pub = await cli([
      'publish',
      '--key',
      key,
      '--locale',
      locale,
      '--revision',
      saved.revision_id,
      '--expected-version',
      String(saved.version),
      '--idempotency',
      'publish-one',
    ]);
    const fetched = await cli(['get', '--key', key, '--locale', locale]);
    assert.equal(fetched.published_revision_id, saved.revision_id);
    assert.equal(fetched.version, pub.version);
    // Real CLI + real blog destination. Missing Notion runner credentials are an
    // explicit blocked outcome, not a simulated successful Notion integration.
    const deliveryFile = resolve(data, 'delivery.json');
    const stateDir = resolve(data, 'delivery-state');
    await writeFile(
      deliveryFile,
      JSON.stringify({
        key: 'delivery-' + locale,
        locale,
        source_revision: 'fixture-source-1',
        notion_parent: 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
        auto_publish: false,
        draft: { ...draft, slug: 'delivery-' + locale, position: 2 },
      }),
      { mode: 0o600 },
    );
    const deliveryArgs = ['deliver', '--input', deliveryFile, '--state-dir', stateDir];
    delete env.NOTION_API_KEY;
    await cli(deliveryArgs, 1);
    const first = JSON.parse(
      await readFile(resolve(stateDir, 'delivery-' + locale + '-' + locale + '.json')),
    );
    assert.equal(first.blog.state, 'done');
    assert.equal(first.notion.state, 'failed');
    await cli(deliveryArgs, 1);
    const again = JSON.parse(
      await readFile(resolve(stateDir, 'delivery-' + locale + '-' + locale + '.json')),
    );
    assert.equal(again.blog.id, first.blog.id);
    const denied = await context.request.post(
      origin + '/api/v1/publisher/content/' + key + '/' + locale + '/quiz-review',
      { headers: { Authorization: 'Bearer ' + token }, data: {} },
    );
    assert.ok(denied.status() >= 400);
    const tokenRow = page.getByRole('listitem').filter({
      has: page.getByText('Scoped test runner', { exact: true }),
    });
    await tokenRow.getByRole('button', { name: t('publisher.revoke'), exact: true }).click();
    await tokenRow.waitFor({ state: 'detached' });
    await cli(['get', '--key', key, '--locale', locale], 1);
    console.log(
      `${locale}: scoped token UI, Go CLI media/draft replay, explicit publication, revoked-token denial and durable blocked Notion delivery passed.`,
    );
  } finally {
    await page.close();
  }
}
