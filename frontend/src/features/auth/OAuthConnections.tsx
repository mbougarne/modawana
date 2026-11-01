import { useState } from 'react';
import { Button, useI18n } from '@ui';
import { call, requestErrorKey } from './api';
import { useResource } from '../../pages/public/api';
import { ResourceState } from '../../pages/public/ResourceState';
import styles from './auth.module.css';

export function OAuthConnections({ csrf }: { csrf: string }) {
  const { t, date } = useI18n();
  const resource =
    useResource<{ id: string; label: string; expires_at: string }[]>('/api/v1/security/oauth');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  async function revoke(id: string) {
    setBusy(true);
    setError('');
    try {
      await call('/security/oauth', 'DELETE', { id }, csrf);
      resource.retry();
    } catch (failure) {
      setError(requestErrorKey(failure));
    } finally {
      setBusy(false);
    }
  }
  return (
    <section className={styles.panel}>
      <h2>{t('oauth.connections')}</h2>
      <ResourceState resource={resource} />
      {error && <p role="alert">{t(error)}</p>}
      {resource.data?.length === 0 && <p>{t('oauth.none')}</p>}
      {resource.data?.map((grant) => (
        <div key={grant.id}>
          <p>
            {grant.label} · {date(grant.expires_at)}
          </p>
          <Button disabled={busy} onClick={() => void revoke(grant.id)}>
            {t('oauth.disconnect')}
          </Button>
        </div>
      ))}
    </section>
  );
}
