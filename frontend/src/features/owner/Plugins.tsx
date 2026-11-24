import { useEffect, useState } from 'react';
import { Link, useLocation } from 'react-router-dom';
import { Button, Tag, useI18n } from '@ui';
import { OwnerPageHeading } from './OwnerWorkspace';
import { useResource } from '../../pages/public/api';
import { ResourceState } from '../../pages/public/ResourceState';
import { authenticatedTransfer } from '../auth/reauthenticate';
import { PluginConfiguration, type PluginManifest } from './PluginConfiguration';
import { localizedContribution, useExtensionCatalog } from './ExtensionCatalog';
import { PluginDeliveries } from './PluginDeliveries';

type PluginItem = {
  slug: string;
  name?: string;
  version?: string;
  api_version?: number;
  job_status?: {
    name: string;
    state: 'idle' | 'scheduled' | 'running' | 'backoff';
    next_run_at?: string;
    consecutive_failures: number;
  }[];
  enabled?: boolean;
  row_version?: number;
  state: 'available' | 'installed' | 'changed' | 'missing' | 'invalid';
  running?: boolean;
  capabilities?: string[];
  added_capabilities?: string[];
  reason?: string;
  manifest?: PluginManifest;
  next_manifest?: PluginManifest;
  candidate_checksum?: string;
};

export function Plugins({ csrf }: { csrf: string }) {
  const { t, locale } = useI18n();
  const { items: catalog, refresh } = useExtensionCatalog();
  const { hash } = useLocation();
  const resource = useResource<{ items: PluginItem[] }>('/api/v1/plugins');
  const [busy, setBusy] = useState('');
  const [notice, setNotice] = useState('');
  const [impact, setImpact] = useState('');
  useEffect(() => {
    if (resource.data && hash.startsWith('#extension-')) {
      document.getElementById(hash.slice(1))?.focus();
    }
  }, [resource.data, hash]);
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
        body: JSON.stringify({
          expected_version: item.row_version || 0,
          candidate_checksum: action === 'install' ? item.candidate_checksum : undefined,
        }),
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
        refresh();
      }
    } catch (error) {
      setFailed(true);
      setNotice(error instanceof DOMException ? 'plugins.reauth' : 'plugins.failed');
    } finally {
      setBusy('');
    }
  }
  const items = resource.data?.items || [];
  const nativeModules = [
    { slug: 'social-publishing', page: 'social' },
    { slug: 'analytics', page: 'analytics' },
  ].flatMap((module) => {
    const adapters = catalog.filter(
      (entry) => entry.extension === module.slug && entry.kind === 'provider_adapter',
    );
    return adapters.length > 0 ? [{ ...module, adapters }] : [];
  });
  return (
    <>
      <OwnerPageHeading title={t('owner.plugins')} description={t('workspace.pluginsHelp')} />
      <ResourceState resource={resource} />
      {nativeModules.map(({ slug, page, adapters }) => (
        <section
          key={slug}
          id={'extension-' + slug}
          tabIndex={-1}
          className="owner-panel owner-form"
        >
          <h2>
            <Link to={`/${locale}/admin/${page}`}>{t('owner.' + page)}</Link>
          </h2>
          <ul>
            {adapters.map((adapter) => (
              <li key={adapter.key}>
                <bdi>{localizedContribution(adapter.label, locale)}</bdi>
              </li>
            ))}
          </ul>
        </section>
      ))}
      {resource.data && items.length === 0 && nativeModules.length === 0 && (
        <p>{t('plugins.empty')}</p>
      )}
      {items.map((item) => {
        const manifest = item.next_manifest || item.manifest;
        const contributions = catalog.filter((entry) => entry.extension === item.slug);
        const settings = contributions.find((entry) => entry.kind === 'settings');
        const approved = item.manifest;
        const jobs = (item.manifest || item.next_manifest)?.jobs;
        return (
          <section
            key={item.slug}
            id={'extension-' + item.slug}
            tabIndex={-1}
            className="owner-panel owner-form"
          >
            <h2>
              {item.name || item.slug}{' '}
              {item.version && <span className="vb-meta">{item.version}</span>}
            </h2>
            <p className="vb-meta">
              {item.slug} · {t('plugins.state.' + item.state)}
              {item.api_version ? (
                <>
                  {' '}
                  · {t('plugins.hostAPI')} {item.api_version}
                </>
              ) : null}
              {item.enabled !== undefined && item.row_version ? (
                <> · {t(item.enabled ? 'plugins.enabled' : 'plugins.disabled')}</>
              ) : null}
              {item.enabled && <Tag>{t(item.running ? 'plugins.running' : 'plugins.stopped')}</Tag>}
            </p>
            {manifest?.mcp_tools?.length ? (
              <p>
                {t('plugins.mcpTools')}{' '}
                {manifest.mcp_tools.map((tool) => (
                  <Tag key={tool.name}>
                    {item.slug}__{tool.name}
                  </Tag>
                ))}
              </p>
            ) : null}
            {manifest?.contributions?.length ? (
              <div>
                <h3>{t('plugins.contributions')}</h3>
                <ul>
                  {manifest.contributions.map((c) => (
                    <li key={c.kind + '.' + c.key}>
                      <bdi>{localizedContribution(c.label, locale)}</bdi>{' '}
                      <Tag>{t('plugins.kind.' + c.kind)}</Tag>{' '}
                      <bdi className="vb-meta">
                        {item.slug}.{c.key} · v{c.version}
                      </bdi>
                    </li>
                  ))}
                </ul>
              </div>
            ) : null}
            {contributions
              .filter((c) => c.kind === 'widget' && c.text)
              .map((c) => (
                <section key={c.key}>
                  <h3 dir="auto">{localizedContribution(c.label, locale)}</h3>
                  {c.text && <p dir="auto">{localizedContribution(c.text, locale)}</p>}
                </section>
              ))}
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
            {manifest?.events?.length ? (
              <p>
                {t('plugins.eventSubscriptions')}{' '}
                {manifest.events.map((event) => (
                  <Tag key={event.name}>
                    <bdi>
                      {event.name} v{event.version}
                    </bdi>
                  </Tag>
                ))}
              </p>
            ) : null}
            {manifest?.https_hosts?.length ? (
              <p>
                {t('plugins.httpsHosts')}{' '}
                {manifest.https_hosts.map((host) => (
                  <Tag key={host}>{host}</Tag>
                ))}
              </p>
            ) : null}
            {jobs?.length ? (
              <div>
                <h3>{t('plugins.jobs')}</h3>
                <ul>
                  {jobs.map((job) => {
                    const status = item.job_status?.find((entry) => entry.name === job.name);
                    return (
                      <li key={job.name}>
                        <bdi>{job.name}</bdi>{' '}
                        {status && (
                          <>
                            <Tag>{t('plugins.job.' + status.state)}</Tag>{' '}
                            {status.next_run_at && (
                              <time dateTime={status.next_run_at}>
                                {new Date(status.next_run_at).toLocaleString(locale)}
                              </time>
                            )}{' '}
                            {status.consecutive_failures > 0 &&
                              t('plugins.job.failures', { n: status.consecutive_failures })}
                          </>
                        )}
                      </li>
                    );
                  })}
                </ul>
              </div>
            ) : null}
            {approved && (approved.settings?.length || approved.secrets?.length) ? (
              <div id={'extension-' + item.slug + ':settings'} tabIndex={-1}>
                {settings && <h3 dir="auto">{localizedContribution(settings.label, locale)}</h3>}
                <PluginConfiguration
                  slug={item.slug}
                  manifest={approved}
                  onUpdate={resource.retry}
                />
              </div>
            ) : null}
            {impact === item.slug && <PluginPageImpact slug={item.slug} />}
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
                    onClick={() =>
                      item.enabled && impact !== item.slug
                        ? setImpact(item.slug)
                        : act(item, item.enabled ? 'disable' : 'enable')
                    }
                  >
                    {t(
                      item.enabled
                        ? impact === item.slug
                          ? 'plugins.disable'
                          : 'plugins.reviewDisable'
                        : 'plugins.enable',
                    )}
                  </Button>
                  {!item.enabled && impact !== item.slug && (
                    <Button variant="quiet" onClick={() => setImpact(item.slug)}>
                      {t('plugins.affectedPages')}
                    </Button>
                  )}
                  <Button variant="quiet" disabled={!!busy} onClick={() => act(item, 'remove')}>
                    {t('plugins.remove')}
                  </Button>
                </>
              )}
            </div>
          </section>
        );
      })}
      <PluginDeliveries csrf={csrf} />
      {notice && (
        <p role="status" className="workspace-alert" data-tone={failed ? 'warning' : 'success'}>
          {t(notice)}
        </p>
      )}
    </>
  );
}

type PageUse = { key: string; locale: string; title: string; draft: boolean; published: boolean };

function PluginPageImpact({ slug }: { slug: string }) {
  const { locale, t } = useI18n();
  const [cursors, setCursors] = useState(['']);
  const cursor = cursors[cursors.length - 1];
  const resource = useResource<{ items: PageUse[]; next?: string }>(
    `/api/v1/plugin-usage/${slug}?cursor=${encodeURIComponent(cursor)}`,
  );
  const page = resource.data;
  const next = page?.next;
  return (
    <div>
      <h3>{t('plugins.affectedPages')}</h3>
      <ResourceState resource={resource} />
      {page && (
        <>
          {page.items.length === 0 ? (
            <p>{t(page.next || cursor ? 'plugins.noPageUsesBatch' : 'plugins.noPageUses')}</p>
          ) : (
            <ul>
              {page.items.map((item) => (
                <li key={item.key + ':' + item.locale}>
                  <Link to={`/${locale}/admin/editor/${item.key}/${item.locale}`}>
                    <bdi>{item.title}</bdi>
                  </Link>{' '}
                  <Tag>{item.locale}</Tag> {item.draft && <Tag>{t('plugins.pageDraft')}</Tag>}{' '}
                  {item.published && <Tag>{t('plugins.pagePublished')}</Tag>}
                </li>
              ))}
            </ul>
          )}
          {cursors.length > 1 && (
            <Button variant="quiet" onClick={() => setCursors(cursors.slice(0, -1))}>
              {t('plugins.previousPageUses')}
            </Button>
          )}
          {next && (
            <Button variant="quiet" onClick={() => setCursors([...cursors, next])}>
              {t('plugins.nextPageUses')}
            </Button>
          )}
        </>
      )}
    </div>
  );
}
