const { test } = require('node:test');
const assert = require('node:assert/strict');
const { prepareLessonPlan } = require('../.test-output/frontend/src/pages/admin/editor/lessonPlan');
const doc = () => ({
  key: 'course',
  kind: 'learning',
  version: 8,
  draft_plan_id: '',
  draft_members: [],
  assigned_members: [
    { key: 'first', announcements: {} },
    { key: 'second', announcements: {} },
  ],
});
test('First lesson publication saves an ordered private plan and uses its returned version', async () => {
  const collection = doc();
  const calls = [];
  global.fetch = async (url, options) => {
    calls.push({ url, options });
    return { ok: true, json: async () => ({ version: 9, plan_id: 'saved-plan' }) };
  };
  const result = await prepareLessonPlan(collection, 'first', 'csrf');
  assert.deepEqual(result, { expected_version: 9, plan_id: 'saved-plan' });
  assert.equal(calls.length, 1);
  assert.equal(calls[0].url, '/api/v1/collections/course/plan');
  assert.equal(calls[0].options.headers['X-CSRF-Token'], 'csrf');
  assert.deepEqual(JSON.parse(calls[0].options.body), {
    expected_version: 8,
    members: collection.assigned_members,
  });
  assert.equal(collection.draft_plan_id, '');
});
test('Existing saved plan is reused without rewriting its order, announcements or gates', async () => {
  const collection = {
    ...doc(),
    draft_plan_id: 'reviewed-plan',
    draft_members: [
      { key: 'second', announcements: { ar: 'لاحقاً' } },
      { key: 'first', announcements: {} },
    ],
  };
  global.fetch = async () => {
    throw new Error('must not save');
  };
  assert.deepEqual(await prepareLessonPlan(collection, 'first', 'csrf'), {
    expected_version: 8,
    plan_id: 'reviewed-plan',
  });
});
test('An assigned lesson absent from the saved plan is appended without replacing existing members', async () => {
  const collection = {
    ...doc(),
    draft_plan_id: 'older-plan',
    draft_members: [{ key: 'second', announcements: { fr: 'Bientôt' } }],
  };
  global.fetch = async (_url, options) => {
    assert.deepEqual(JSON.parse(options.body).members, [
      ...collection.draft_members,
      { key: 'first', announcements: {} },
    ]);
    return { ok: true, json: async () => ({ version: 9, plan_id: 'updated-plan' }) };
  };
  assert.equal((await prepareLessonPlan(collection, 'first', 'csrf')).plan_id, 'updated-plan');
  assert.equal(collection.draft_members.length, 1);
});
test('Unassigned lessons and rejected plan saves never produce an activation request', async () => {
  global.fetch = async () => {
    throw new Error('must not save');
  };
  await assert.rejects(
    prepareLessonPlan(doc(), 'unassigned', 'csrf'),
    (e) => e.code === 'invalid_fields',
  );
  global.fetch = async () => ({
    ok: false,
    status: 409,
    json: async () => ({ error: { code: 'revision_conflict' } }),
  });
  await assert.rejects(
    prepareLessonPlan(doc(), 'first', 'csrf'),
    (e) => e.code === 'revision_conflict',
  );
});
