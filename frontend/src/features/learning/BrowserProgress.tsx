import { useState } from 'react';
import { useI18n, Button } from '@ui';
import { useResource } from '../../pages/public/api';
import { ResourceState } from '../../pages/public/ResourceState';
import { call } from '../auth/api';

export interface Progress {
  active: boolean;
  csrf?: string;
  idle_days: number;
  scope: 'browser';
  progress: unknown[];
}

export function BrowserProgress() {
  const { t } = useI18n();
  const state = useResource<Progress>('/api/v1/learning/progress', false, true);
  const [error, setError] = useState(false);
  const [busy, setBusy] = useState(false);
  return (
    <aside className="public-notice public-browser-progress">
      <h2 className="vb-h3">{t('progress.title')}</h2>
      <p>{t('progress.scope')}</p>
      <ResourceState resource={state} />
      {state.data && <p>{t('progress.retention', { n: state.data.idle_days })}</p>}
      {state.data && !state.data.active && (
        <Button
          disabled={busy}
          onClick={async () => {
            setBusy(true);
            setError(false);
            try {
              await call('/learning/session', 'POST', {}, undefined, { 'X-Blog-Request': '1' });
              window.dispatchEvent(new Event('learning-progress'));
            } catch {
              setError(true);
            } finally {
              setBusy(false);
            }
          }}
        >
          {t('progress.start')}
        </Button>
      )}
      {state.data?.active && (
        <Button
          disabled={busy}
          onClick={async () => {
            if (!window.confirm(t('progress.confirm'))) {
              return;
            }
            setBusy(true);
            setError(false);
            try {
              await call('/learning/progress', 'DELETE', undefined, state.data!.csrf);
              window.location.reload();
            } catch {
              setError(true);
            } finally {
              setBusy(false);
            }
          }}
        >
          {t('progress.reset')}
        </Button>
      )}
      {error && <p role="alert">{t('progress.failed')}</p>}
    </aside>
  );
}
