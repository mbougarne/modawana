import { useState } from 'react';
import { Button, Tag, useI18n } from '@ui';
import { useResource } from '../../pages/public/api';
import { ResourceState } from '../../pages/public/ResourceState';
import { call } from '../auth/api';

type Delivery = {
  id: string;
  plugin: string;
  event: string;
  state: 'pending' | 'running' | 'succeeded' | 'failed' | 'paused';
  attempts: number;
  updated_at: string;
  next_attempt?: string;
  last_error?: string;
  can_retry: boolean;
};

export function PluginDeliveries({ csrf }: { csrf: string }) {
  const { locale, t } = useI18n();
  const [cursors, setCursors] = useState(['']);
  const [retrying, setRetrying] = useState('');
  const [notice, setNotice] = useState('');
  const cursor = cursors[cursors.length - 1];
  const resource = useResource<{ items: Delivery[]; next?: string }>(
    '/api/v1/plugin-deliveries?cursor=' + encodeURIComponent(cursor),
  );
  const next = resource.data?.next;

  async function retry(item: Delivery) {
    setRetrying(item.id);
    setNotice('');
    try {
      await call('/plugin-deliveries/' + item.id + '/retry', 'POST', undefined, csrf);
      setNotice('plugins.deliveries.requeued');
      setCursors(['']);
    } catch {
      setNotice('plugins.failed');
    } finally {
      setRetrying('');
      resource.retry();
    }
  }

  const date = (value: string) => new Date(value).toLocaleString(locale);
  return (
    <section className="owner-panel owner-form" aria-labelledby="plugin-delivery-heading">
      <h2 id="plugin-delivery-heading">{t('plugins.deliveries')}</h2>
      <div className="owner-fields">
        <Button variant="quiet" onClick={resource.retry} disabled={resource.loading}>
          {t('plugins.deliveries.refresh')}
        </Button>
      </div>
      <ResourceState resource={resource} />
      {notice && <p role="status">{t(notice)}</p>}
      {resource.data?.items.length === 0 && <p>{t('plugins.deliveries.empty')}</p>}
      {resource.data?.items.map((item) => (
        <section key={item.id}>
          <h3>
            <bdi>{item.plugin}</bdi> · <bdi>{item.event}</bdi>
          </h3>
          <p>
            <Tag>{t('plugins.deliveries.' + item.state)}</Tag>{' '}
            {t('plugins.deliveries.attempts', { n: item.attempts })}{' '}
            <time dateTime={item.updated_at}>{date(item.updated_at)}</time>
          </p>
          <p className="vb-meta">
            <bdi>{item.id}</bdi>
          </p>
          {item.next_attempt && (
            <p>
              {t('plugins.deliveries.nextAttempt')}{' '}
              <time dateTime={item.next_attempt}>{date(item.next_attempt)}</time>
            </p>
          )}
          {item.last_error && <p>{t('plugins.deliveries.error.' + item.last_error)}</p>}
          {item.can_retry && (
            <Button variant="quiet" onClick={() => void retry(item)} disabled={!!retrying}>
              {t('plugins.deliveries.retry')}
            </Button>
          )}
        </section>
      ))}
      <div className="owner-fields">
        {cursors.length > 1 && (
          <Button variant="quiet" onClick={() => setCursors(cursors.slice(0, -1))}>
            {t('plugins.deliveries.previous')}
          </Button>
        )}
        {next && (
          <Button variant="quiet" onClick={() => setCursors([...cursors, next])}>
            {t('plugins.deliveries.more')}
          </Button>
        )}
      </div>
    </section>
  );
}
