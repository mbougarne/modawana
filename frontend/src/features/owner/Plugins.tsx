import { useState } from 'react';
import { Button, Tag, useI18n } from '@ui';
import { OwnerPageHeading } from './OwnerWorkspace';
import { useResource } from '../../pages/public/api';
import { ResourceState } from '../../pages/public/ResourceState';
import { authenticatedTransfer } from '../auth/reauthenticate';

type PluginItem = {
  slug: string;
  name?: string;
  version?: string;
  enabled?: boolean;
  row_version?: number;
  state: 'available' | 'installed' | 'changed' | 'missing' | 'invalid';
  running?: boolean;
  capabilities?: string[];
  added_capabilities?: string[];
  reason?: string;
};

export function Plugins() {
  const { t } = useI18n();
  const resource = useResource<{ items: PluginItem[] }>('/api/v1/plugins');
  const [busy, setBusy] = useState('');
  const [notice, setNotice] = useState('');
  const [failed, setFailed] = useState(false);
  async function act(item: PluginItem, action: 'install' | 'enable' | 'disable' | 'remove') {
    if (busy) {
      return;
    }
    if (action === 'remove' && !window.confirm(t('plugins.removeConfirm'))) {
      return;
    }
    setBusy(item.slug + ':' + action);
    setNotice('');
    try {
      const response = await authenticatedTransfer(`/api/v1/plugins/${item.slug}/${action}`, {
        method: 'POST',
        credentials: 'same-origin',
        cache: 'no-store',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ expected_version: item.row_version || 0 }),
      });
      setFailed(!response.ok);
      setNotice(
        response.ok
          ? 'plugins.saved'
          : response.status === 401 || response.status === 403
            ? 'plugins.reauth'
            : response.status === 409
              ? 'plugins.conflict'
              : 'plugins.failed',
      );
      if (response.ok) {
        resource.retry();
      }
    } catch (error) {
      setFailed(true);
      setNotice(error instanceof DOMException ? 'plugins.reauth' : 'plugins.failed');
    } finally {
      setBusy('');
    }
  }
  const items = resource.data?.items || [];
  return (
    <>
      <OwnerPageHeading title={t('owner.plugins')} description={t('workspace.pluginsHelp')} />
      <ResourceState resource={resource} />
      {resource.data && items.length === 0 && <p>{t('plugins.empty')}</p>}
      {items.map((item) => (
        <section key={item.slug} className="owner-panel owner-form">
          <h2>
            {item.name || item.slug}{' '}
            {item.version && <span className="vb-meta">{item.version}</span>}
          </h2>
          <p className="vb-meta">
            {item.slug} · {t('plugins.state.' + item.state)}
            {item.state === 'installed' && (
              <> · {t(item.enabled ? 'plugins.enabled' : 'plugins.disabled')}</>
            )}
          </p>
          {item.capabilities && item.capabilities.length > 0 && (
            <p>
              {t('plugins.capabilities')}:{' '}
              {item.capabilities.map((capability) => (
                <Tag key={capability}>{capability}</Tag>
              ))}
            </p>
          )}
          {item.added_capabilities && item.added_capabilities.length > 0 && (
            <p role="alert" className="workspace-alert" data-tone="warning">
              {t('plugins.addedCapabilities')}:{' '}
              {item.added_capabilities.map((capability) => (
                <Tag key={capability}>{capability}</Tag>
              ))}
            </p>
          )}
          {item.reason && <p className="vb-meta">{item.reason}</p>}
          <div className="owner-fields">
            {(item.state === 'available' || item.state === 'changed') && (
              <Button disabled={!!busy} onClick={() => act(item, 'install')}>
                {t(item.state === 'available' ? 'plugins.install' : 'plugins.reapprove')}
              </Button>
            )}
            {item.state !== 'available' && item.state !== 'invalid' && (
              <>
                <Button
                  disabled={!!busy || (!item.enabled && item.state !== 'installed')}
                  onClick={() => act(item, item.enabled ? 'disable' : 'enable')}
                >
                  {t(item.enabled ? 'plugins.disable' : 'plugins.enable')}
                </Button>
                <Button variant="quiet" disabled={!!busy} onClick={() => act(item, 'remove')}>
                  {t('plugins.remove')}
                </Button>
              </>
            )}
          </div>
        </section>
      ))}
      {notice && (
        <p role="status" className="workspace-alert" data-tone={failed ? 'warning' : 'success'}>
          {t(notice)}
        </p>
      )}
    </>
  );
}
