import { useEffect, useRef, useState } from 'react';
import { useLocation, useSearchParams } from 'react-router-dom';
import { Icon, useI18n } from '@ui';
import { EntryList } from './presentation';
import type { DiscoveryPage, TaxonomyTerm } from '../../api/content';
import { useResource } from './api';
import { ResourceState } from './ResourceState';

export { EntryList } from './presentation';

export function WritingPage() {
  const { locale, t } = useI18n();
  const searchPage = useLocation().pathname.endsWith('/search');
  const input = useRef<HTMLInputElement>(null);
  const [draftQuery, setDraftQuery] = useState('');
  const [params, setParams] = useSearchParams();
  useEffect(() => {
    setDraftQuery(params.get('q') || '');
  }, [params.get('q')]);
  useEffect(() => {
    if (!searchPage) {
      return;
    }
    const timer = setTimeout(() => {
      const value = draftQuery.trim();
      if (value === (params.get('q') || '')) {
        return;
      }
      const next = new URLSearchParams(params);
      next.delete('cursor');
      next.delete('previous');
      if (value) {
        next.set('q', value);
      } else {
        next.delete('q');
      }
      setParams(next, { replace: true });
    }, 250);
    return () => clearTimeout(timer);
  }, [draftQuery, searchPage, params, setParams]);
  useEffect(() => {
    if (!searchPage) {
      return;
    }
    function focus(event: KeyboardEvent) {
      const el = event.target as HTMLElement;
      if (
        event.key === '/' &&
        !/^(INPUT|TEXTAREA|SELECT)$/.test(el.tagName) &&
        !el.isContentEditable
      ) {
        event.preventDefault();
        input.current?.focus();
      }
    }
    document.addEventListener('keydown', focus);
    return () => document.removeEventListener('keydown', focus);
  }, [searchPage]);
  const query = new URLSearchParams(params);
  query.delete('previous');
  query.set('locale', locale);
  query.set('limit', '20');
  const results = useResource<DiscoveryPage>('/api/v1/public/content?' + query.toString());
  const taxonomy = useResource<{ items: TaxonomyTerm[] }>(
    '/api/v1/public/taxonomy?locale=' + locale,
  );
  function filter(key: string, value: string) {
    const next = new URLSearchParams(params);
    next.delete('cursor');
    next.delete('previous');
    if (next.get(key) === value || !value) {
      next.delete(key);
    } else {
      next.set(key, value);
    }
    setParams(next);
  }
  const active = ['kind', 'category', 'tag', 'q'].filter((k) => params.has(k));
  return (
    <>
      <header className="vb-pagehead">
        <h1>{t(searchPage ? 'page.search' : 'page.writing')}</h1>
        {!searchPage && <p>{t('writing.intro')}</p>}
      </header>
      {searchPage && (
        <section className="public-search-panel search-page">
          <form
            className="public-search public-search-form"
            onSubmit={(e) => {
              e.preventDefault();
              const value = String(new FormData(e.currentTarget).get('q') || '').trim();
              const next = new URLSearchParams(params);
              next.delete('cursor');
              next.delete('previous');
              if (value) {
                next.set('q', value);
              } else {
                next.delete('q');
              }
              setParams(next);
            }}
          >
            <label className="vb-search public-search-field">
              <Icon name="search" />
              <input
                ref={input}
                aria-label={t('public.query')}
                placeholder={t('public.query')}
                name="q"
                type="search"
                maxLength={200}
                value={draftQuery}
                onChange={(event) => setDraftQuery(event.target.value)}
              />
              <kbd aria-hidden="true">/</kbd>
            </label>
          </form>
          <label className="public-kind">
            {t('public.kind')}
            <select
              value={params.get('kind') || ''}
              onChange={(e) => filter('kind', e.target.value)}
            >
              <option value="">{t('public.any')}</option>
              {['article', 'lesson', 'doc'].map((k) => (
                <option key={k} value={k}>
                  {t('public.' + k)}
                </option>
              ))}
            </select>
          </label>
        </section>
      )}
      <ResourceState resource={taxonomy} />
      {!searchPage &&
        (['category', 'tag'] as const).map((kind) => (
          <div
            role="group"
            aria-label={t(kind === 'tag' ? 'public.tags' : 'public.category')}
            key={kind}
            className={'vb-filterrow public-filters public-filter-row ' + kind}
          >
            <span className="vb-meta" aria-hidden="true">
              {t(kind === 'tag' ? 'public.tags' : 'public.category')}
            </span>
            {kind === 'category' && (
              <button
                className="vb-chip"
                type="button"
                aria-pressed={!params.get('category')}
                onClick={() => filter('category', '')}
              >
                {t('writing.all')}
              </button>
            )}
            {taxonomy.data?.items
              .filter((term) => term.kind === kind)
              .map((term) => (
                <button
                  key={term.key}
                  className={kind === 'tag' ? 'vb-tagchip' : 'vb-chip'}
                  type="button"
                  aria-pressed={params.get(kind) === term.slug}
                  onClick={() => filter(kind, term.slug)}
                >
                  <bdi>
                    {kind === 'tag' ? '#' : ''}
                    {term.label}
                  </bdi>{' '}
                  {kind === 'category' && <small>{term.count}</small>}
                </button>
              ))}
          </div>
        ))}
      {!!active.length && (
        <div className="public-active">
          <span>
            {active
              .map((k) =>
                k === 'kind'
                  ? t('public.' + params.get(k))
                  : taxonomy.data?.items.find(
                      (term) => term.kind === k && term.slug === params.get(k),
                    )?.label || params.get(k),
              )
              .join(' · ')}
          </span>
          <button className="vb-btn vb-btn-quiet" onClick={() => setParams({})}>
            {t('public.clear')}
          </button>
        </div>
      )}
      <ResourceState resource={results} />
      {results.data && (
        <>
          <p className="vb-meta" role="status">
            {t('public.results', { n: results.data.total })}
          </p>
          {results.data.items.length ? (
            <EntryList items={results.data.items} terms={taxonomy.data?.items} />
          ) : (
            <p className="public-empty">{t('public.noResults')}</p>
          )}
          <nav className="public-pagination" aria-label={t('public.pages')}>
            {params.has('cursor') && (
              <button
                className="vb-btn vb-btn-secondary"
                onClick={() => {
                  const next = new URLSearchParams(params);
                  const previous = next.getAll('previous');
                  const cursor = previous.pop() || '';
                  next.delete('previous');
                  previous.forEach((value) => next.append('previous', value));
                  if (cursor) {
                    next.set('cursor', cursor);
                  } else {
                    next.delete('cursor');
                  }
                  setParams(next);
                  document.getElementById('content')?.focus();
                }}
              >
                {t('public.previous')}
              </button>
            )}
            {results.data.next_cursor && (
              <button
                className="vb-btn vb-btn-secondary"
                onClick={() => {
                  const next = new URLSearchParams(params);
                  next.append('previous', params.get('cursor') || '');
                  next.set('cursor', results.data!.next_cursor);
                  setParams(next);
                  document.getElementById('content')?.focus();
                }}
              >
                {t('public.next')}
              </button>
            )}
          </nav>
        </>
      )}
    </>
  );
}
