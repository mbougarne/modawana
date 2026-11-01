import { useI18n } from '@ui';
import type { Resource } from './api';

export function ResourceState({ resource }: { resource: Resource<unknown> }) {
  const { t } = useI18n();
  if (resource.loading) {
    return <p role="status">{t('public.loading')}</p>;
  }
  if (resource.error) {
    return (
      <div role="alert">
        <h1>{t(resource.error === 404 ? 'foundation.notFound' : 'public.error')}</h1>
        <button className="vb-btn vb-btn-secondary" onClick={resource.retry}>
          {t('public.retry')}
        </button>
      </div>
    );
  }
  return null;
}
