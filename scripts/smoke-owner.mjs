import assert from 'node:assert/strict';
import { resolve } from 'node:path';
import { confirmAction } from './smoke-dialog.mjs';

export async function smokeOwner({ context, origin, root, locale, t }) {
  const page = await context.newPage();
  const errors = [];
  page.on('pageerror', (e) => errors.push(e.message));
  const button = (k) => page.getByRole('button', { name: t(k), exact: true });
  const label = (k) => page.getByLabel(t(k), { exact: true });
  const key = 'editor-' + locale;
  try {
    await page.goto(origin + '/' + locale + '/admin/new');
    await page.getByRole('tab', { name: t('owner.details'), exact: true }).click();
    await label('owner.key').fill(key);
    await label('owner.title').fill('Owner editor ' + locale);
    assert.equal(await label('owner.slug').inputValue(), 'owner-editor-' + locale);
    // Switching kinds must not retain invisible collection-only fields.
    await label('owner.kind').selectOption('lesson');
    await label('owner.collectionKey').fill('discarded-collection');
    await label('owner.kind').selectOption('article');
    await page.getByRole('tab', { name: t('owner.write'), exact: true }).click();
    await label('owner.markdown').fill('First paragraph');
    await label('owner.markdown').evaluate((el) => {
      el.focus();
      el.setSelectionRange(0, 5);
    });
    await page.getByRole('tab', { name: t('owner.preview'), exact: true }).click();
    await page.getByRole('tab', { name: t('owner.write'), exact: true }).click();
    await button('toolbar.bold').click();
    assert.equal(await label('owner.markdown').inputValue(), '**First** paragraph');
    await page.getByRole('tab', { name: t('owner.details'), exact: true }).click();
    await label('owner.slug').fill(key);
    await button('owner.save').click();
    await page
      .getByRole('status')
      .filter({ hasText: t('owner.saved') })
      .waitFor();
    await button('owner.publish').click();
    await page
      .getByRole('status')
      .filter({ hasText: t('owner.publishDone') })
      .waitFor();
    assert.equal(await label('owner.slug').isDisabled(), true);
    await page.getByRole('tab', { name: t('owner.write'), exact: true }).click();
    await label('owner.markdown').fill('Unsaved paragraph');
    const { csrf } = await (await context.request.get(origin + '/api/v1/session')).json();
    const draft = await (
      await context.request.get(origin + `/api/v1/content/${key}/${locale}`)
    ).json();
    const competing = await context.request.put(origin + `/api/v1/content/${key}/${locale}`, {
      headers: { Origin: origin, 'X-CSRF-Token': csrf },
      data: {
        expected_version: draft.version,
        kind: 'article',
        source_locale: locale,
        slug: key,
        title: 'Concurrent edit',
        markdown: 'Concurrent body',
      },
    });
    assert.equal(competing.status(), 200, await competing.text());
    await button('owner.save').click();
    await page
      .getByRole('status')
      .filter({ hasText: t('config.conflict') })
      .waitFor();
    assert.equal(await label('owner.markdown').inputValue(), 'Unsaved paragraph');
    const live = await (
      await context.request.get(origin + `/api/v1/public/content/${locale}/${key}`)
    ).json();
    assert.equal(live.markdown, '**First** paragraph');
    const download = page.waitForEvent('download');
    await button('owner.exportDraft').click();
    assert.equal((await download).suggestedFilename(), key + '-' + locale + '.json');
    await confirmAction(page, t('owner.confirmDiscard'), () => button('config.reload').click());
    await page.waitForFunction(
      () => document.querySelector('.owner-markdown')?.value === 'Concurrent body',
    );
    for (const destination of [
      'posts',
      'collections',
      'taxonomy',
      'media',
      'settings',
      'profile',
      'labels',
      'security',
      'transfer',
      'plugins',
    ]) {
      await page.goto(origin + `/${locale}/admin/${destination}`);
      await page.getByRole('navigation', { name: t('config.owner'), exact: true }).waitFor();
      await page.getByRole('heading', { level: 1 }).first().waitFor();
      assert.equal(await page.locator('.workspace-header .owner-navigation a').count(), 10);
      assert.equal(
        await page
          .locator('.workspace-header .owner-navigation [aria-current="page"]')
          .getAttribute('href'),
        `/${locale}/admin/${destination}`,
      );
      assert.equal(
        await page.locator('.workspace-actions').locator('> *').last().textContent(),
        t('auth.signout'),
      );
      assert.equal(await page.locator('.workspace-actions a').getAttribute('href'), '/' + locale);
      const languageMenu = page.locator('.config-language button');
      await languageMenu.click();
      assert.equal(await page.locator('#config-language-menu a').count(), 3);
      await page.keyboard.press('Escape');
      assert.equal(await languageMenu.evaluate((n) => n === document.activeElement), true);
      for (const width of [390, 1280]) {
        await page.setViewportSize({ width, height: 900 });
        await page.evaluate(() => document.fonts.ready);
        assert.ok(
          await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth),
          `workspace overflow: ${locale}/${destination}/${width}`,
        );
        if (destination === 'posts' || destination === 'labels') {
          const search = page.locator('.workspace-search-input').first();
          const boxes = await search.evaluate((node) => {
            const icon = node.querySelector('svg').getBoundingClientRect();
            const input = node.querySelector('input').getBoundingClientRect();
            return {
              gap: Math.max(input.left - icon.right, icon.left - input.right),
              vertical: Math.abs((icon.top + icon.bottom - input.top - input.bottom) / 2),
            };
          });
          assert.ok(
            boxes.gap >= 4 && boxes.vertical <= 1,
            `search overlap: ${destination}/${locale}/${width}`,
          );
        }
        if (destination === 'settings' || destination === 'profile') {
          const layout = await page.locator('.config-editor-layout').evaluate((node) => {
            const controls = node.querySelector('.config-edit-controls').getBoundingClientRect();
            const preview = node.querySelector('.config-live-preview').getBoundingClientRect();
            return {
              columns: getComputedStyle(node).gridTemplateColumns.split(' ').length,
              aligned: Math.abs(controls.top - preview.top),
              stacked: preview.top >= controls.bottom,
            };
          });
          assert.equal(layout.columns, width > 1000 ? 2 : 1);
          assert.ok(width > 1000 ? layout.aligned <= 2 : layout.stacked);
        }
        if (destination === 'settings') {
          assert.equal(await page.locator('.config-colour-field').count(), 2);
          for (const field of await page.locator('.config-colour-field').all()) {
            assert.equal(await field.locator('.vb-swatches button').count(), 5);
          }
        }
        if (destination === 'settings' || destination === 'profile') {
          const gaps = await page.locator('.config-media').evaluateAll((fields) =>
            fields.map((field) => {
              const drop = field.querySelector('.vb-file-drop').getBoundingClientRect();
              const existing = field.querySelector(':scope > button').getBoundingClientRect();
              return existing.top - drop.bottom;
            }),
          );
          assert.ok(
            gaps.every((gap) => gap >= 12),
            'image upload/existing-image spacing',
          );
          const historyGap = await page
            .locator('.config-history-panel')
            .evaluate(
              (panel) =>
                panel.querySelector('button').getBoundingClientRect().top -
                panel.querySelector('h2').getBoundingClientRect().bottom,
            );
          assert.ok(historyGap >= 16, 'history heading spacing');
        }
        if (destination === 'media') {
          assert.equal(
            await page.getByRole('button', { name: t('upload.chooseImage'), exact: true }).count(),
            1,
          );
          assert.equal(
            await page.getByRole('button', { name: t('upload.chooseVideo'), exact: true }).count(),
            1,
          );
        }
        await page.screenshot({
          path: resolve(
            root,
            `frontend/test-results/workspace-${locale}-${destination}-${width}.png`,
          ),
          fullPage: true,
        });
      }
    }
    await page.setViewportSize({ width: 390, height: 844 });
    await page.goto(origin + `/${locale}/admin/editor/${key}/${locale}`);
    await label('owner.markdown').waitFor();
    assert.equal(
      await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth),
      true,
    );
    await page.screenshot({
      path: resolve(root, `frontend/test-results/owner-${locale}.png`),
      fullPage: true,
    });
    // Metadata moves to the sidebar without replacing the mounted working draft.
    const editorText = await label('owner.markdown').inputValue();
    await page.getByRole('tab', { name: t('owner.details'), exact: true }).click();
    await label('owner.slug').waitFor();
    await page.setViewportSize({ width: 1280, height: 900 });
    await label('owner.markdown').waitFor();
    assert.equal(await label('owner.markdown').inputValue(), editorText);
    assert.equal(await page.getByRole('tab', { name: t('owner.details'), exact: true }).count(), 0);
    const layout = await page.locator('.editor-layout').evaluate((el) => {
      const paper = el.querySelector('.vb-paper').getBoundingClientRect();
      const sidebar = el.querySelector('.editor-sidebar').getBoundingClientRect();
      return { separate: paper.right <= sidebar.left || sidebar.right <= paper.left };
    });
    assert.equal(layout.separate, true);
    await page.screenshot({
      path: resolve(root, `frontend/test-results/editor-${locale}-desktop.png`),
      fullPage: true,
    });
    await page.setViewportSize({ width: 390, height: 844 });
    assert.equal(await label('owner.markdown').inputValue(), editorText);
    // Build and privately grade all three question types through the real API.
    await page.getByRole('tab', { name: t('owner.quiz'), exact: true }).click();
    await page.getByRole('switch', { name: t('builder.addQuiz'), exact: true }).check();
    assert.equal(await button('owner.save').isDisabled(), true);
    for (let i = 0; i < 3; i++) {
      if (i > 0) {
        await page
          .getByRole('button', {
            name: '+ ' + t('builder.' + ['single', 'multi', 'bool'][i]),
            exact: true,
          })
          .click();
      }
      const questions = page.locator('#panel-quiz fieldset');
      const q = questions.nth(i);
      await q
        .getByLabel(t('builder.type'), { exact: true })
        .selectOption(['single', 'multi', 'bool'][i]);
      await q.getByLabel(t('builder.prompt'), { exact: true }).fill('Private question ' + i);
      if (i < 2) {
        await q.getByLabel(t('builder.option', { n: 1 }), { exact: true }).fill('Answer A ' + i);
        await q.getByLabel(t('builder.option', { n: 2 }), { exact: true }).fill('Answer B ' + i);
      }
      await q.getByLabel(t('builder.correct'), { exact: true }).first().check();
      if (i === 1) {
        await q.getByLabel(t('builder.correct'), { exact: true }).nth(1).check();
      }
      await q
        .getByLabel(t('builder.explanation'), { exact: true })
        .fill('Private explanation ' + i);
    }
    await button('owner.save').click();
    await page
      .getByRole('status')
      .filter({ hasText: t('owner.saved') })
      .waitFor();
    const saved = await (
      await context.request.get(origin + `/api/v1/content/${key}/${locale}`)
    ).json();
    assert.equal(saved.revision.quiz.questions.length, 3);
    await button('builder.preview').click();
    const player = page.locator('.vb-quiz');
    await player.getByRole('radio').first().check();
    for (const checkbox of await player.getByRole('checkbox').all()) {
      await checkbox.check();
    }
    await player.getByRole('radio').nth(2).check();
    await player.getByRole('button', { name: t('quiz.check'), exact: true }).click();
    await player.getByText('Private explanation 0', { exact: true }).waitFor();
    const learning = await (await context.request.get(origin + '/api/v1/learning/progress')).json();
    assert.equal(learning.active, false);
    const source = await (
      await context.request.get(origin + `/api/v1/public/content/${locale}/${key}`)
    ).text();
    assert.equal(source.includes('Private question'), false);
    assert.equal(source.includes('correct'), false);
    await button('builder.review').click();
    await page
      .getByRole('status')
      .filter({ hasText: t('builder.reviewed') })
      .waitFor();
    await page.setViewportSize({ width: 390, height: 844 });
    assert.equal(
      await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth),
      true,
    );
    await page.screenshot({
      path: resolve(root, `frontend/test-results/quiz-${locale}.png`),
      fullPage: true,
    });
    // Upload and insert a stable media reference from the editor.
    await page.getByRole('tab', { name: t('owner.media'), exact: true }).click();
    await label('owner.upload').setInputFiles(
      resolve(root, 'backend/internal/media/testdata/poster.png'),
    );
    await page
      .getByRole('status')
      .filter({ hasText: t('owner.uploaded') })
      .waitFor();
    await label('owner.mediaText').fill('Owner upload');
    await button('owner.insert').click();
    assert.match(
      await label('owner.markdown').inputValue(),
      /!\[Owner upload\]\(\/media\/[a-f0-9]{32}\)/,
    );
    await button('owner.save').click();
    await page
      .getByRole('status')
      .filter({ hasText: t('owner.saved') })
      .waitFor();
    if (locale === 'en') {
      await button('owner.publish').click();
      await page
        .getByRole('status')
        .filter({ hasText: t('owner.publishDone') })
        .waitFor();
      const current = await (
        await context.request.get(origin + `/api/v1/content/${key}/en`)
      ).json();
      const request = async (method, path, data, status = 200) => {
        const result = await context.request.fetch(origin + '/api/v1' + path, {
          method,
          headers: { Origin: origin, 'X-CSRF-Token': csrf },
          data,
        });
        assert.equal(result.status(), status, await result.text());
        return result.json();
      };
      await request(
        'PUT',
        `/content/${key}/ar`,
        {
          expected_version: 0,
          kind: 'article',
          source_locale: 'en',
          slug: key + '-arabic',
          title: 'Arabic quiz translation',
          markdown: 'Translated body',
          source_revision_id: current.revision.revision_id,
          quiz: current.revision.quiz,
        },
        201,
      );
      const updated = await request('PUT', `/content/${key}/en`, {
        expected_version: current.version,
        kind: 'article',
        source_locale: 'en',
        slug: key,
        title: current.revision.title,
        markdown: current.revision.markdown,
        quiz: { ...current.revision.quiz, assessment_id: undefined, pass_mark: 2 },
      });
      await request('POST', `/content/${key}/en/publish`, {
        expected_version: updated.version,
        revision_id: updated.revision_id,
      });
      await page.goto(origin + `/en/admin/editor/${key}/ar`);
      await page.getByRole('tab', { name: t('owner.quiz'), exact: true }).click();
      assert.equal(await label('builder.passMark').inputValue(), '1');
      assert.equal(await label('builder.passMark').isDisabled(), true);
      await confirmAction(page, t('builder.refreshConfirm'), () =>
        button('builder.refreshSource').click(),
      );
      await page
        .getByRole('status')
        .filter({ hasText: t('builder.sourceLoaded') })
        .waitFor();
      assert.equal(await label('builder.passMark').inputValue(), '2');
      await button('owner.save').click();
      await page
        .getByRole('status')
        .filter({ hasText: t('owner.saved') })
        .waitFor();
      await button('owner.review').click();
      await page
        .getByRole('status')
        .filter({ hasText: t('owner.reviewDone') })
        .waitFor();
      await button('owner.publish').click();
      await page
        .getByRole('status')
        .filter({ hasText: t('owner.publishDone') })
        .waitFor();
    }

    // Create localized taxonomy and collection metadata through owner forms.
    await page.goto(origin + `/${locale}/admin/taxonomy`);
    await label('owner.key').fill('owner-term-' + locale);
    await label('owner.label').fill('Owner term');
    assert.equal(await label('owner.slug').inputValue(), 'owner-term');
    await label('owner.slug').fill('owner-term-' + locale);
    await button('owner.saveTerm').click();
    await page
      .getByRole('status')
      .filter({ hasText: t('owner.termSaved') })
      .waitFor();
    await page.goto(origin + `/${locale}/admin/collections`);
    await label('owner.key').fill('owner-course-' + locale);
    await label('owner.title').fill('Owner course');
    assert.equal(await label('owner.slug').inputValue(), 'owner-course');
    await label('owner.slug').fill('owner-course-' + locale);
    await button('collection.save').click();
    await page
      .getByRole('status')
      .filter({ hasText: t('collection.saved') })
      .waitFor();
    await button('owner.new').click();
    assert.equal(
      await page
        .getByRole('status')
        .filter({ hasText: t('collection.saved') })
        .count(),
      0,
    );
    assert.equal(await label('owner.slug').inputValue(), '');
    // Explicit learning interaction only; reset preserves the owner session.
    assert.equal(
      (await context.cookies()).some((c) => c.name === '__Host-learning'),
      false,
    );
    const created = await context.request.post(origin + '/api/v1/learning/session', {
      headers: { Origin: origin, 'X-Blog-Request': '1' },
    });
    assert.equal(created.status(), 200, await created.text());
    const identity = await created.json();
    await page.goto(origin + `/${locale}/learn`);
    await confirmAction(page, t('progress.confirm'), () => button('progress.reset').click());
    await page
      .getByRole('button', { name: t('progress.reset'), exact: true })
      .waitFor({ state: 'hidden' });
    assert.equal(
      (await (await context.request.get(origin + '/api/v1/session')).json()).state,
      'owner',
    );
    assert.equal(
      (
        await context.request.delete(origin + '/api/v1/learning/progress', {
          headers: { Origin: origin, 'X-CSRF-Token': identity.csrf },
        })
      ).status(),
      403,
    );

    // Bulk publication uses the saved revisions, then unpublishes without deleting them.
    const batchPrefix = 'bulk-ui-' + locale;
    for (const suffix of ['one', 'two']) {
      const bulkKey = batchPrefix + '-' + suffix;
      const created = await context.request.put(origin + `/api/v1/content/${bulkKey}/${locale}`, {
        headers: { Origin: origin, 'X-CSRF-Token': csrf },
        data: {
          expected_version: 0,
          kind: 'article',
          source_locale: locale,
          slug: bulkKey,
          title: bulkKey,
          markdown: 'Bulk publication fixture',
        },
      });
      assert.equal(created.status(), 201, await created.text());
    }
    await page.goto(origin + `/${locale}/admin/posts?kind=article&q=${batchPrefix}`);
    await page.locator('.owner-list li').first().waitFor();
    assert.equal(await page.locator('.owner-list li').count(), 2);
    await button('bulk.selectAll').click();
    await page.getByText(t('bulk.selected', { n: 2 }), { exact: true }).waitFor();
    await button('bulk.apply').click();
    await button('bulk.confirmApply').click();
    await page.getByText(t('bulk.result', { changed: 2, unchanged: 0 }), { exact: true }).waitFor();
    for (const suffix of ['one', 'two']) {
      assert.equal(
        (
          await context.request.get(
            origin + `/api/v1/public/content/${locale}/${batchPrefix}-${suffix}`,
          )
        ).status(),
        200,
      );
    }
    await button('bulk.selectAll').click();
    await page.getByLabel(t('bulk.status'), { exact: true }).selectOption('unpublish');
    await button('bulk.apply').click();
    await button('bulk.confirmApply').click();
    await page.getByText(t('bulk.result', { changed: 2, unchanged: 0 }), { exact: true }).waitFor();
    for (const suffix of ['one', 'two']) {
      const bulkKey = batchPrefix + '-' + suffix;
      assert.equal(
        (
          await context.request.get(origin + `/api/v1/public/content/${locale}/${bulkKey}`)
        ).status(),
        404,
      );
      const kept = await (
        await context.request.get(origin + `/api/v1/content/${bulkKey}/${locale}`)
      ).json();
      assert.equal(kept.revision.markdown, 'Bulk publication fixture');
    }
    // Convert an already published imported-style article without replacing its revision.
    const conversionKey = batchPrefix + '-one';
    const conversionPath = 'conversion-course-' + locale;
    const conversionHeaders = { Origin: origin, 'X-CSRF-Token': csrf };
    const createdPath = await context.request.put(
      origin + '/api/v1/collections/' + conversionPath + '/' + locale,
      {
        headers: conversionHeaders,
        data: {
          expected_version: 0,
          kind: 'learning',
          slug: conversionPath,
          title: 'Conversion path',
          summary: '',
          visible: true,
        },
      },
    );
    assert.equal(createdPath.ok(), true, await createdPath.text());
    const beforeConversion = await (
      await context.request.get(origin + '/api/v1/content/' + conversionKey + '/' + locale)
    ).json();
    const publication = await context.request.post(
      origin + '/api/v1/content/' + conversionKey + '/' + locale + '/publish',
      {
        headers: conversionHeaders,
        data: {
          expected_version: beforeConversion.version,
          revision_id: beforeConversion.revision.revision_id,
        },
      },
    );
    assert.equal(publication.ok(), true, await publication.text());
    await page.goto(origin + '/' + locale + '/admin/editor/' + conversionKey + '/' + locale);
    await page.getByRole('tab', { name: t('owner.details'), exact: true }).click();
    await page.getByLabel(t('owner.kind'), { exact: true }).selectOption('lesson');
    await page.getByLabel(t('owner.collectionKey'), { exact: true }).fill(conversionPath);
    await page.getByRole('option').filter({ hasText: conversionPath }).click();
    await confirmAction(page, t('conversion.confirm', { kind: t('owner.lesson') }), () =>
      button('owner.save').click(),
    );
    await page
      .getByRole('status')
      .filter({ hasText: t('owner.saved') })
      .waitFor();
    const afterConversion = await (
      await context.request.get(origin + '/api/v1/content/' + conversionKey + '/' + locale)
    ).json();
    assert.equal(afterConversion.kind, 'lesson');
    assert.notEqual(afterConversion.revision.revision_id, beforeConversion.revision.revision_id);
    assert.equal(afterConversion.published_revision_id, beforeConversion.revision.revision_id);
    assert.equal(afterConversion.revision.markdown, beforeConversion.revision.markdown);
    assert.equal(afterConversion.collection_key, conversionPath);
    assert.equal(await label('owner.collectionKey').inputValue(), conversionPath);
    assert.equal(await label('owner.collectionKey').isDisabled(), true);
    await confirmAction(
      page,
      t('progress.planImpact', { path: conversionPath, lessons: conversionKey }),
      () => button('progress.publishPlan').click(),
    );
    await page
      .getByRole('status')
      .filter({ hasText: t('editor.lessonPublished') })
      .waitFor();
    const publicPaths = await (
      await context.request.get(
        origin + '/api/v1/public/collections?locale=' + locale + '&kind=learning&limit=20',
      )
    ).json();
    assert.equal(
      publicPaths.items.some((item) => item.key === conversionPath),
      true,
    );

    // Two published articles can be added to a new learning path without separate plan steps.
    const simplePath = 'simple-course-' + locale;
    for (const suffix of ['one', 'two']) {
      const articleKey = simplePath + '-' + suffix;
      const saved = await context.request.put(
        origin + '/api/v1/content/' + articleKey + '/' + locale,
        {
          headers: conversionHeaders,
          data: {
            expected_version: 0,
            kind: 'article',
            source_locale: locale,
            slug: articleKey,
            title: 'Simple article ' + suffix,
            markdown: 'Published fixture',
          },
        },
      );
      assert.equal(saved.ok(), true, await saved.text());
      const revision = await saved.json();
      const published = await context.request.post(
        origin + '/api/v1/content/' + articleKey + '/' + locale + '/publish',
        {
          headers: conversionHeaders,
          data: { expected_version: revision.version, revision_id: revision.revision_id },
        },
      );
      assert.equal(published.ok(), true, await published.text());
    }
    await page.goto(origin + '/' + locale + '/admin/collections');
    await label('owner.key').fill(simplePath);
    await label('owner.title').fill('Simple path');
    await label('owner.slug').fill(simplePath);
    for (const suffix of ['one', 'two']) {
      await label('collection.addContent').fill(simplePath + '-' + suffix);
      await page
        .getByRole('option')
        .filter({ hasText: simplePath + '-' + suffix })
        .click();
      await button('owner.add').click();
    }
    await confirmAction(page, t('collection.confirm'), () => button('collection.save').click());
    await page
      .getByRole('status')
      .filter({ hasText: t('collection.saved') })
      .waitFor();
    assert.equal(await button('owner.generateKey').count(), 0);
    const simplePublic = await (
      await context.request.get(origin + '/api/v1/public/collections/' + locale + '/' + simplePath)
    ).json();
    assert.equal(simplePublic.published_count, 2);

    // Publish a new long article directly, then publish unsaved edits without a separate Save.
    const longKey = 'long-editor-' + locale;
    const longMarkdown =
      Array.from({ length: 600 }, (_, i) => 'Paragraph ' + i + ' ' + 'longword'.repeat(20)).join(
        '\n\n',
      ) +
      '\n\n' +
      'x'.repeat(4000);
    await page.goto(origin + '/' + locale + '/admin/new');
    await page.getByRole('tab', { name: t('owner.details'), exact: true }).click();
    await label('owner.key').fill(longKey);
    await label('owner.slug').fill(longKey);
    await label('owner.title').fill('Averylongtitle'.repeat(15).slice(0, 200));
    await page.getByRole('tab', { name: t('owner.write'), exact: true }).click();
    await label('owner.markdown').fill(longMarkdown);
    const longLayout = await label('owner.markdown').evaluate((el) => ({
      height: el.getBoundingClientRect().height,
      scroll: el.scrollHeight,
      client: el.clientHeight,
      right: el.getBoundingClientRect().right,
      paperRight: el.closest('.editor-paper').getBoundingClientRect().right,
      viewport: innerHeight,
      pageWidth: document.documentElement.scrollWidth,
      viewportWidth: innerWidth,
    }));
    assert.ok(
      longLayout.scroll > longLayout.client,
      'long Markdown must scroll inside the text area',
    );
    assert.ok(
      longLayout.height <= Math.max(320, longLayout.viewport * 0.75) + 1,
      'text area must remain bounded',
    );
    assert.ok(longLayout.right <= longLayout.paperRight + 1, 'text area must fit its paper');
    assert.ok(
      longLayout.pageWidth <= longLayout.viewportWidth + 1,
      'editor must not overflow the page',
    );
    await button('editor.savePublish').click();
    await page
      .getByRole('status')
      .filter({ hasText: t('owner.publishDone') })
      .waitFor();
    const publishedLong = await (
      await context.request.get(origin + '/api/v1/public/content/' + locale + '/' + longKey)
    ).json();
    assert.equal(publishedLong.markdown, longMarkdown);
    await label('owner.markdown').fill(longMarkdown + '\n\nLatest edit');
    await button('editor.savePublish').click();
    await page
      .getByRole('status')
      .filter({ hasText: t('owner.publishDone') })
      .waitFor();
    const updatedLong = await (
      await context.request.get(origin + '/api/v1/public/content/' + locale + '/' + longKey)
    ).json();
    assert.equal(updatedLong.markdown, longMarkdown + '\n\nLatest edit');
    await page.goto(origin + '/' + locale + '/admin/posts?q=' + longKey);
    await page.locator('.owner-post-title a').waitFor();
    const titleBounds = await page
      .locator('.owner-post-title')
      .first()
      .evaluate((el) => ({
        right: el.getBoundingClientRect().right,
        parentRight: el.closest('li').getBoundingClientRect().right,
        scroll: document.documentElement.scrollWidth,
        width: innerWidth,
      }));
    assert.ok(
      titleBounds.right <= titleBounds.parentRight + 1 &&
        titleBounds.scroll <= titleBounds.width + 1,
      'long content titles must stay within the list',
    );
    assert.deepEqual(errors, []);
    console.log('Owner editor/navigation passed: ' + locale);
  } catch (error) {
    await page
      .screenshot({
        path: resolve(root, `frontend/test-results/owner-${locale}-failure.png`),
        fullPage: true,
      })
      .catch(() => {});
    throw error;
  } finally {
    await page.close();
  }
}
