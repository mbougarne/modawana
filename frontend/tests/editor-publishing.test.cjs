const { test } = require('node:test');
const assert = require('node:assert/strict');
const {
  publishEditorDraft,
} = require('../.test-output/frontend/src/pages/admin/editor/publishDraft');
const draft = (version = 2, revision = 'old') => ({
  key: 'article',
  kind: 'article',
  collection_key: '',
  version,
  revision: { revision_id: revision },
});
const response = (data) => ({ ok: true, json: async () => data });
const options = () => ({
  key: 'article',
  locale: 'en',
  csrf: 'csrf',
  dirty: false,
  draft: draft(),
  withPlan: false,
  save: async () => assert.fail('unexpected save'),
  confirmPlan: () => assert.fail('unexpected confirmation'),
});
test('Publish saves dirty content first and uses the returned exact revision/version', async () => {
  const calls = [];
  const published = { ...draft(9, 'new'), published_revision_id: 'new' };
  global.fetch = async (url, init) => {
    calls.push({ url, init });
    return response(url.endsWith('/publish') ? { version: 9 } : published);
  };
  let saved = 0;
  const result = await publishEditorDraft({
    ...options(),
    dirty: true,
    save: async () => {
      saved++;
      return draft(8, 'new');
    },
  });
  assert.equal(saved, 1);
  assert.equal(result.published_revision_id, 'new');
  assert.deepEqual(JSON.parse(calls[0].init.body), { expected_version: 8, revision_id: 'new' });
  assert.equal(calls[0].init.headers['X-CSRF-Token'], 'csrf');
  assert.ok(calls[0].init.headers['Idempotency-Key']);
  assert.equal(calls[1].init.method, 'GET');
});
test('A new article can publish directly; a failed or cancelled save never publishes the old revision', async () => {
  let requests = 0;
  global.fetch = async () => {
    requests++;
    return response(draft(2, 'new'));
  };
  await publishEditorDraft({ ...options(), draft: undefined, save: async () => draft(1, 'new') });
  assert.equal(requests, 2);
  requests = 0;
  await assert.rejects(
    publishEditorDraft({
      ...options(),
      dirty: true,
      save: async () => {
        throw new Error('validation');
      },
    }),
    /validation/,
  );
  assert.equal(requests, 0);
  assert.equal(
    await publishEditorDraft({ ...options(), dirty: true, save: async () => undefined }),
    undefined,
  );
  assert.equal(requests, 0);
});
test('Lesson publication uses its newly saved collection and plan version, rather than stale editor state', async () => {
  const calls = [];
  const lesson = { ...draft(5, 'saved'), kind: 'lesson', collection_key: 'new-course' };
  const collection = {
    key: 'new-course',
    kind: 'learning',
    version: 7,
    draft_plan_id: '',
    draft_members: [],
    assigned_members: [{ key: 'article', announcements: {} }],
  };
  global.fetch = async (url, init) => {
    calls.push({ url, init });
    return response(
      url.endsWith('/plan')
        ? { version: 8, plan_id: 'plan' }
        : url.endsWith('/new-course')
          ? collection
          : url.endsWith('/publish')
            ? {}
            : lesson,
    );
  };
  await publishEditorDraft({
    ...options(),
    dirty: true,
    save: async () => lesson,
    withPlan: true,
    confirmPlan: (c) => {
      assert.equal(c.key, 'new-course');
      return true;
    },
  });
  assert.equal(calls[0].url, '/api/v1/collections/new-course');
  const publication = calls.find((c) => c.url.endsWith('/publish'));
  assert.deepEqual(JSON.parse(publication.init.body), {
    expected_version: 5,
    revision_id: 'saved',
    plan: { expected_version: 8, plan_id: 'plan' },
  });
});
test('Cancelling a path activation performs no publication; denied publication retains the saved draft', async () => {
  const lesson = { ...draft(5, 'saved'), kind: 'lesson', collection_key: 'course' };
  let writes = 0;
  global.fetch = async (_url, init) => {
    if (init.method !== 'GET') {
      writes++;
    }
    return response({ key: 'course', kind: 'learning', draft_members: [], assigned_members: [] });
  };
  assert.equal(
    await publishEditorDraft({
      ...options(),
      draft: lesson,
      withPlan: true,
      confirmPlan: () => false,
    }),
    undefined,
  );
  assert.equal(writes, 0);
  let stored;
  global.fetch = async () => ({
    ok: false,
    status: 422,
    json: async () => ({ error: { code: 'translation_review_required' } }),
  });
  await assert.rejects(
    publishEditorDraft({
      ...options(),
      dirty: true,
      save: async () => {
        stored = draft(6, 'preserved');
        return stored;
      },
    }),
    (e) => e.code === 'translation_review_required',
  );
  assert.equal(stored.revision.revision_id, 'preserved');
  assert.equal(stored.version, 6);
});
