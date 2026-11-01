import { Link, useSearchParams } from 'react-router-dom';
import { useI18n, Icon } from '@ui';
import { OwnerPageHeading } from './OwnerWorkspace';
import { useResource } from '../../pages/public/api';
import { ResourceState } from '../../pages/public/ResourceState';

import { BulkPublication, type OwnerEntry } from './BulkPublication';

export function OwnerPosts({ csrf }: { csrf: string }) {
  const { locale, t } = useI18n();
  const [params, setParams] = useSearchParams();
  const q = new URLSearchParams(params);
  q.set('locale', locale);
  const r = useResource<{ items: OwnerEntry[]; next_cursor: string }>('/api/v1/content?' + q);
  const selectionQuery = new URLSearchParams(q);
  selectionQuery.delete('cursor');
  function filter(key: string, value: string) {
    const next = new URLSearchParams(params);
    next.delete('cursor');
    if (value) {
      next.set(key, value);
    } else {
      next.delete(key);
    }
    setParams(next);
  }
  return (
    <>
      <OwnerPageHeading title={t('owner.posts')} description={t('workspace.postsHelp')}>
        <Link className="vb-btn vb-btn-primary" to={`/${locale}/admin/new`}>
          {t('owner.new')}
        </Link>
      </OwnerPageHeading>
      <form
        className="owner-panel posts-filters"
        aria-label={t('workspace.filters')}
        onSubmit={(e) => {
          e.preventDefault();
          filter('q', String(new FormData(e.currentTarget).get('q') || ''));
        }}
      >
        <label className="posts-query">
          {t('public.query')}
          <span className="workspace-search-input">
            <Icon name="search" />
            <input
              key={params.get('q') || ''}
              className="vb-input"
              type="search"
              name="q"
              maxLength={200}
              defaultValue={params.get('q') || ''}
              placeholder={t('workspace.searchPosts')}
            />
          </span>
        </label>
        {(['kind', 'status'] as const).map((k) => (
          <label key={k}>
            {t('owner.' + k)}
            <select
              className="vb-select"
              value={params.get(k) || ''}
              onChange={(e) => filter(k, e.target.value)}
            >
              <option value="">{t('owner.all')}</option>
              {(k === 'kind'
                ? ['article', 'lesson', 'doc']
                : ['draft', 'published', 'changed']
              ).map((v) => (
                <option key={v} value={v}>
                  {t('owner.' + v)}
                </option>
              ))}
            </select>
          </label>
        ))}
        <div className="posts-filter-actions">
          <button type="submit" className="vb-btn vb-btn-secondary">
            {t('public.search')}
          </button>
          {params.toString() && (
            <button
              type="button"
              className="vb-btn vb-btn-quiet"
              onClick={() => setParams(new URLSearchParams())}
            >
              {t('writing.clear')}
            </button>
          )}
        </div>
      </form>
      <ResourceState resource={r} />
      <BulkPublication
        key={selectionQuery.toString()}
        query={selectionQuery.toString()}
        csrf={csrf}
        items={r.data?.items ?? []}
        loading={r.loading}
        onApplied={r.retry}
      >
        {(selected, toggle) => (
          <ul className="owner-list owner-post-list" aria-label={t('owner.posts')}>
            {(r.data?.items ?? []).map((item) => (
              <li key={item.key}>
                <label>
                  <input
                    type="checkbox"
                    checked={!!selected[item.key + '/' + item.locale]}
                    onChange={() => toggle(item)}
                  />
                  <span className="vb-visually-hidden">
                    {t('bulk.selectItem', { title: item.title })}
                  </span>
                </label>
                <div className="owner-post-title">
                  <Link to={`/${locale}/admin/editor/${item.key}/${item.locale}`}>
                    {item.title}
                  </Link>
                  <small>
                    <bdi>{item.key}</bdi> · {t('owner.' + item.kind)}
                  </small>
                </div>
                <span className="workspace-status" data-state={item.status}>
                  {t('owner.' + item.status)}
                </span>
              </li>
            ))}
          </ul>
        )}
      </BulkPublication>
      {r.data && (
        <>
          {!r.data.items.length && <p>{t('owner.empty')}</p>}
          {r.data.next_cursor && (
            <button
              className="vb-btn vb-btn-secondary"
              onClick={() => {
                const next = new URLSearchParams(params);
                next.set('cursor', r.data!.next_cursor);
                setParams(next);
              }}
            >
              {t('public.more')}
            </button>
          )}
        </>
      )}
    </>
  );
}
