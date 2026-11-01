import type { CollectionDocument, Draft, Locale } from '../../../api/content';
import { call, AuthError } from '../../../features/auth/api';
import { prepareLessonPlan } from './lessonPlan';

export async function publishEditorDraft({
  key,
  locale,
  draft,
  dirty,
  save,
  csrf,
  withPlan,
  confirmPlan,
}: {
  key: string;
  locale: Locale;
  draft?: Draft;
  dirty: boolean;
  save: () => Promise<Draft | undefined>;
  csrf: string;
  withPlan: boolean;
  confirmPlan: (collection: CollectionDocument) => boolean;
}): Promise<Draft | undefined> {
  const current = !draft || dirty ? await save() : draft;
  if (!current) {
    return;
  }
  let plan;
  if (withPlan) {
    if (current.kind !== 'lesson' || !current.collection_key) {
      throw new AuthError('invalid_fields', [
        { path: 'collection_key', code: 'conversion_collection' },
      ]);
    }
    const collection = await call<CollectionDocument>(
      '/collections/' + encodeURIComponent(current.collection_key),
    );
    if (!confirmPlan(collection)) {
      return;
    }
    plan = await prepareLessonPlan(collection, key, csrf);
  }
  const path = '/content/' + encodeURIComponent(key) + '/' + locale;
  await call(
    path + '/publish',
    'POST',
    {
      expected_version: current.version,
      revision_id: current.revision.revision_id,
      ...(plan ? { plan } : {}),
    },
    csrf,
    { 'Idempotency-Key': crypto.randomUUID() },
  );
  return call<Draft>(path);
}
