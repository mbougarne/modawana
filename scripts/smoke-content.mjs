import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { resolve } from 'node:path';

export async function smokeContent({ context, origin, root }) {
  const { csrf } = await (await context.request.get(origin + '/api/v1/session')).json();
  const headers = { Origin: origin, 'X-CSRF-Token': csrf };
  const anon = await context.browser().newContext({ ignoreHTTPSErrors: true });
  async function request(method, path, data, status = 200, retry) {
    const r = await context.request.fetch(origin + '/api/v1/' + path, {
      method,
      headers: { ...headers, ...(retry ? { 'Idempotency-Key': retry } : {}) },
      data,
    });
    assert.equal(r.status(), status, await r.text());
    return r.json();
  }
  async function publicRead(path, status = 200) {
    const r = await anon.request.get(origin + '/api/v1/public/' + path);
    assert.equal(r.status(), status, await r.text());
    return r.json();
  }
  async function mediaStatus(id, status) {
    assert.equal(
      (
        await anon.request.get(origin + '/media/' + id, { headers: { Range: 'bytes=0-9' } })
      ).status(),
      status,
    );
  }
  async function upload(name, mime, buffer) {
    const r = await context.request.post(origin + '/api/v1/media', {
      headers,
      multipart: { file: { name, mimeType: mime, buffer } },
    });
    assert.equal(r.status(), 201, await r.text());
    return r.json();
  }
  try {
    await request('PUT', 'taxonomy/category/systems/en', {
      expected_version: 0,
      label: 'Systems',
      slug: 'systems',
    });
    await request('PUT', 'taxonomy/tag/go/en', { expected_version: 0, label: 'Go', slug: 'go' });
    const video = await upload(
      'clip.mp4',
      'video/mp4',
      await readFile(resolve(root, 'backend/internal/media/testdata/clip.mp4')),
    );
    const poster = await upload(
      'poster.png',
      'image/png',
      await readFile(resolve(root, 'backend/internal/media/testdata/poster.png')),
    );
    const track = await upload(
      'captions.vtt',
      'text/vtt',
      Buffer.from('WEBVTT\n\n00:00.000 --> 00:01.000\nHello\n'),
    );
    const draft = {
      expected_version: 0,
      kind: 'article',
      source_locale: 'en',
      slug: 'phase-three',
      title: 'Phase three',
      summary: 'Go storage',
      markdown: `# Draft\n\n[Clip](${video.url})`,
      source_revision_id: '',
      category_key: 'systems',
      tag_keys: ['go'],
      cover_media_id: poster.id,
      media_presentations: [
        {
          media_id: video.id,
          poster_media_id: poster.id,
          caption_track_media_id: track.id,
          caption: 'Clip',
          transcript: 'Hello',
        },
      ],
    };
    const first = await request('PUT', 'content/article/en', draft, 201, 'draft-1');
    assert.deepEqual(await request('PUT', 'content/article/en', draft, 201, 'draft-1'), first);
    await request('PUT', 'content/article/en', { ...draft, title: 'Different' }, 409, 'draft-1');
    await publicRead('content/en/phase-three', 404);
    await mediaStatus(video.id, 404);
    const second = await request(
      'PUT',
      'content/article/en',
      { ...draft, expected_version: 1, title: 'Go publication' },
      200,
      'draft-2',
    );
    const published = await request(
      'POST',
      'content/article/en/publish',
      { expected_version: 2, revision_id: second.revision_id },
      200,
      'publish-1',
    );
    assert.deepEqual(
      await request(
        'POST',
        'content/article/en/publish',
        { expected_version: 2, revision_id: second.revision_id },
        200,
        'publish-1',
      ),
      published,
    );
    const live = await publicRead('content/en/phase-three');
    assert.equal(live.title, 'Go publication');
    assert.equal(live.media.length, 3);
    assert.ok(live.media.every((m) => !('filename' in m) && !('storage_key' in m)));
    await mediaStatus(video.id, 206);
    await mediaStatus(track.id, 206);
    const french = {
      ...draft,
      slug: 'phase-trois',
      title: 'Phase trois',
      markdown: '# Bonjour',
      source_revision_id: second.revision_id,
      category_key: '',
      tag_keys: [],
      cover_media_id: '',
      media_presentations: [],
    };
    const fr = await request('PUT', 'content/article/fr', french, 201);
    await request(
      'POST',
      'content/article/fr/publish',
      { expected_version: 1, revision_id: fr.revision_id },
      422,
    );
    const review = await request('POST', 'content/article/fr/review', {
      expected_version: 1,
      revision_id: fr.revision_id,
      source_revision_id: second.revision_id,
    });
    await request('POST', 'content/article/fr/publish', {
      expected_version: review.version,
      revision_id: fr.revision_id,
    });
    const next = await request('PUT', 'content/article/en', {
      ...draft,
      expected_version: 3,
      title: 'Go publication updated',
    });
    await request('POST', 'content/article/en/publish', {
      expected_version: next.version,
      revision_id: next.revision_id,
    });
    assert.equal((await publicRead('content/fr/phase-trois')).translation_stale, true);
    const list = await publicRead(
      'search?locale=en&kind=article&category=systems&tag=go&q=updated&limit=1',
    );
    assert.equal(list.total, 1);
    assert.equal(list.items[0].title, 'Go publication updated');
    assert.equal((await publicRead('search?locale=ar')).total, 0);
    const restored = await request('POST', 'content/article/en/restore', {
      expected_version: 5,
      revision_id: first.revision_id,
    });
    assert.notEqual(restored.revision_id, first.revision_id);
    assert.equal((await publicRead('content/en/phase-three')).revision_id, next.revision_id);
    await request('POST', 'content/article/en/unpublish', {
      expected_version: restored.version,
      revision_id: next.revision_id,
    });
    await publicRead('content/en/phase-three', 404);
    await mediaStatus(video.id, 404);
    await request('DELETE', 'media/' + video.id, undefined, 409);
    assert.equal((await publicRead('alternatives/article')).items.length, 1);
    const arabic = await request(
      'PUT',
      'content/arabic/ar',
      {
        ...draft,
        source_locale: 'ar',
        slug: 'مقال-عربي',
        title: 'مقال عربي',
        markdown: '# مرحباً',
        category_key: '',
        tag_keys: [],
        cover_media_id: '',
        media_presentations: [],
      },
      201,
    );
    await request('POST', 'content/arabic/ar/publish', {
      expected_version: 1,
      revision_id: arabic.revision_id,
    });
    assert.equal(
      (await publicRead('content/ar/' + encodeURIComponent('مقال-عربي'))).title,
      'مقال عربي',
    );
    await request('PUT', 'collections/course/en', {
      expected_version: 0,
      kind: 'learning',
      slug: 'course',
      title: 'Course',
      summary: 'A real course',
      visible: true,
    });
    const lessonDraft = {
      expected_version: 0,
      kind: 'lesson',
      source_locale: 'en',
      summary: '',
      category_key: '',
      tag_keys: [],
      cover_media_id: '',
      media_presentations: [],
      collection_key: 'course',
    };
    let one;
    for (const [i, key] of ['one', 'two', 'hidden'].entries()) {
      const r = await request(
        'PUT',
        `content/${key}/en`,
        {
          ...lessonDraft,
          slug: key,
          title: 'PRIVATE ' + key,
          markdown: `[Clip](${video.url})`,
          position: i + 1,
        },
        201,
      );
      if (i === 0) {
        one = r;
      }
    }
    const ownerCollection = await request('GET', 'collections/course');
    const members = [
      { key: 'one', announcements: {} },
      { key: 'two', announcements: { en: 'Next topic' } },
      { key: 'hidden', announcements: {} },
    ];
    const plan = await request('PUT', 'collections/course/plan', {
      expected_version: ownerCollection.version,
      members,
    });
    await request('POST', 'content/one/en/publish', {
      expected_version: 1,
      revision_id: one.revision_id,
    });
    // A published lesson outside the active plan is hidden, including its teaser.
    const hidden = await publicRead('content/en/one', 404);
    assert.equal(hidden.error.code, 'not_found');
    assert.ok(!hidden.teaser);
    assert.doesNotMatch(JSON.stringify(hidden), /PRIVATE one|markdown|media/);
    await mediaStatus(video.id, 404);
    const active = await request('POST', 'collections/course/apply', {
      expected_version: plan.version,
      plan_id: plan.plan_id,
    });
    await publicRead('content/en/one');
    await mediaStatus(video.id, 206);
    const course = await publicRead('collections/en/course');
    assert.equal(course.published_count, 1);
    assert.equal(course.announced_count, 1);
    assert.equal(course.total, 2);
    assert.equal(course.items[1].title, 'Next topic');
    assert.ok(!course.items[1].url && !course.items[1].key);
    assert.ok(
      !JSON.stringify(course).includes('PRIVATE two') && !JSON.stringify(course).includes('hidden'),
    );
    await publicRead('content/en/two', 404);
    await publicRead('collections/fr/course', 404);
    const reorder = await request('PUT', 'collections/course/plan', {
      expected_version: active.version,
      members: [members[1], members[0], members[2]],
    });
    assert.equal((await publicRead('collections/en/course')).items[0].state, 'open');
    await request(
      'POST',
      'collections/course/apply',
      { expected_version: active.version, plan_id: reorder.plan_id },
      409,
    );
    await request('POST', 'collections/course/apply', {
      expected_version: reorder.version,
      plan_id: reorder.plan_id,
    });
    assert.equal((await publicRead('collections/en/course')).items[0].state, 'soon');
    await request('POST', 'content/one/en/unpublish', {
      expected_version: 2,
      revision_id: one.revision_id,
    });
    await mediaStatus(video.id, 404);
    assert.equal((await anon.request.get(origin + '/api/v1/content')).status(), 401);
    assert.equal(
      (
        await context.request.put(origin + '/api/v1/content/no/en', {
          headers: { Origin: origin },
          data: draft,
        })
      ).status(),
      403,
    );
    console.log(
      'Content: authenticated drafts/retries/history, translation review/publication, media privacy, discovery and active/Soon collection plans passed.',
    );
  } finally {
    await anon.close();
  }
}
