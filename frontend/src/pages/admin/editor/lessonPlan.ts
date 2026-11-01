import type { ApplyPlanInput, CollectionDocument, CollectionResult } from '../../../api/content';
import { call, AuthError } from '../../../features/auth/api';

// Saving a plan is private. Publication and activation remain one server transaction.
export async function prepareLessonPlan(
  collection: CollectionDocument,
  key: string,
  csrf: string,
): Promise<ApplyPlanInput> {
  if (collection.kind !== 'learning' || !collection.assigned_members.some((m) => m.key === key)) {
    throw new AuthError('invalid_fields', [
      { path: 'collection_key', code: 'conversion_collection' },
    ]);
  }
  if (collection.draft_plan_id && collection.draft_members.some((m) => m.key === key)) {
    return { expected_version: collection.version, plan_id: collection.draft_plan_id };
  }
  const members = collection.draft_plan_id
    ? [...collection.draft_members, { key, announcements: {} }]
    : collection.assigned_members;
  const saved = await call<CollectionResult>(
    '/collections/' + encodeURIComponent(collection.key) + '/plan',
    'PUT',
    { expected_version: collection.version, members },
    csrf,
  );
  if (!saved.plan_id) {
    throw new AuthError('invalid_fields');
  }
  return { expected_version: saved.version, plan_id: saved.plan_id };
}
