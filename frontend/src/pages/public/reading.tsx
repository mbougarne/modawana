import { EntryList, CollectionCards } from './presentation';
import type {
  DiscoveryPage,
  PublicCollection,
  PublicContent,
  TaxonomyTerm,
} from '../../api/content';
import { AuthorCard, LessonSidebar, renderBio, useI18n } from '@ui';
import { BrowserProgress } from '../../features/learning/BrowserProgress';
import { useState } from 'react';
import { ReaderLearning } from '../../features/learning/ReaderLearning';
import { Link, useSearchParams } from 'react-router-dom';
import { usePublicConfiguration } from '../../features/configuration/PublicConfiguration';
import { MarkdownReader } from '../../features/markdown/MarkdownReader';
import { useResource, type CollectionPage, type PageRoute } from './api';
import { ResourceState } from './ResourceState';

export function CollectionIndex({
  kind,
  limit = 20,
  compact = false,
}: {
  kind: 'learning' | 'docs';
  limit?: number;
  compact?: boolean;
}) {
  const { locale, t } = useI18n();
  const [cursor, setCursor] = useState('');
  const resource = useResource<CollectionPage>(
    `/api/v1/public/collections?locale=${locale}&kind=${kind}&limit=${limit}&cursor=${encodeURIComponent(cursor)}`,
  );
  const [docCursor, setDocCursor] = useState('');
  const documents = useResource<DiscoveryPage>(
    kind === 'docs'
      ? '/api/v1/public/content?' +
          new URLSearchParams({ locale, kind: 'doc', limit: String(limit), cursor: docCursor })
      : undefined,
  );
  const title = t(kind === 'learning' ? 'page.learn' : 'page.docs');
  if (kind === 'docs' && !compact) {
    return <DocsIndex />;
  }
  return (
    <section className="public-section">
      <div className="public-section-heading">
        {compact ? (
          <h2 className="vb-h2">{title}</h2>
        ) : kind === 'docs' ? (
          <h1 className="vb-title">{title}</h1>
        ) : (
          <header className="vb-pagehead">
            <h1>{title}</h1>
            <p>{kind === 'learning' ? t('learning.intro') : ''}</p>
          </header>
        )}
        {compact && (
          <Link className="vb-link" to={'/' + locale + (kind === 'learning' ? '/learn' : '/docs')}>
            {t(kind === 'learning' ? 'home.everything' : 'public.all')}{' '}
            {locale === 'ar' ? '←' : '→'}
          </Link>
        )}
      </div>
      {!compact && kind === 'learning' && <BrowserProgress />}
      <ResourceState resource={resource} />
      {resource.data && (
        <>
          {kind === 'learning' && !resource.data.items.length && (
            <p className="public-empty">{t('public.empty')}</p>
          )}
          {kind === 'learning' ? (
            <CollectionCards items={resource.data.items} />
          ) : (
            <div className="public-cards">
              {resource.data.items.map((c) => (
                <article key={c.key} className="public-card">
                  <p className="vb-meta">
                    {t('public.counts', {
                      published: c.published_count,
                      announced: c.announced_count,
                    })}
                  </p>
                  <h2 className="vb-h2">
                    <Link to={c.url}>{c.title}</Link>
                  </h2>
                  {c.summary && <p>{c.summary}</p>}
                </article>
              ))}
            </div>
          )}
          {resource.data.next_cursor && !compact && (
            <button
              className="vb-btn vb-btn-secondary"
              onClick={() => setCursor(resource.data!.next_cursor)}
            >
              {t('public.more')}
            </button>
          )}
        </>
      )}
      {kind === 'docs' && (
        <>
          <ResourceState resource={documents} />
          {documents.data && <EntryList items={documents.data.items} />}
          {documents.data?.next_cursor && !compact && (
            <button
              className="vb-btn vb-btn-secondary"
              onClick={() => setDocCursor(documents.data!.next_cursor)}
            >
              {t('public.more')}
            </button>
          )}
        </>
      )}
    </section>
  );
}

function DocsIndex({ current }: { current?: PublicContent }) {
  const { locale, t } = useI18n();
  const [params, setParams] = useSearchParams();
  const [cursor, setCursor] = useState('');
  const [previous, setPrevious] = useState<string[]>([]);
  const documents = useResource<DiscoveryPage>(
    '/api/v1/public/content?' + new URLSearchParams({ locale, kind: 'doc', limit: '20', cursor }),
  );
  const selected = current?.slug || params.get('doc') || documents.data?.items[0]?.slug;
  const page = useResource<PublicContent>(
    !current && selected
      ? '/api/v1/public/content/' + locale + '/' + encodeURIComponent(selected)
      : undefined,
  );
  const content = current || page.data;
  return (
    <div className="vb-split public-reading-layout">
      <nav className="vb-side" aria-label={t('page.docs')}>
        <h4>{t('page.docs')}</h4>
        <ResourceState resource={documents} />
        <ol>
          {documents.data?.items.map((item) => (
            <li key={item.key}>
              <Link to={item.url} aria-current={selected === item.slug ? 'page' : undefined}>
                {item.title}
              </Link>
            </li>
          ))}
        </ol>
        <div className="public-pagination">
          {!!previous.length && (
            <button
              className="vb-btn vb-btn-quiet"
              onClick={() => {
                setCursor(previous[previous.length - 1]);
                setPrevious(previous.slice(0, -1));
              }}
            >
              {t('public.previous')}
            </button>
          )}
          {documents.data?.next_cursor && (
            <button
              className="vb-btn vb-btn-quiet"
              onClick={() => {
                setPrevious([...previous, cursor]);
                setCursor(documents.data!.next_cursor);
                setParams({});
              }}
            >
              {t('public.next')}
            </button>
          )}
        </div>
      </nav>
      <div className="public-reading-main">
        <ResourceState resource={page} />
        {content ? (
          <ContentBody content={content} />
        ) : (
          !documents.loading && !documents.data?.items.length && <p>{t('public.empty')}</p>
        )}
      </div>
    </div>
  );
}

export function Outline({
  collection,
  current,
}: {
  collection: PublicCollection;
  current?: string;
}) {
  const { t } = useI18n();
  return (
    <aside className="public-outline public-reference-outline">
      <LessonSidebar
        title={collection.title}
        progress={
          collection.kind === 'learning'
            ? t('progress.count', {
                n: collection.items.filter((item) => item.completed).length,
                total: collection.published_count,
              })
            : undefined
        }
        lessons={collection.items.map((item) => ({
          id: item.key || 'soon-' + item.position,
          number: item.position,
          title: item.title,
          href: item.url,
          state: item.state,
          completed: item.completed,
          current: !!current && item.key === current,
        }))}
      />
    </aside>
  );
}

export function CollectionView({ collection }: { collection: PublicCollection }) {
  const { t } = useI18n();
  return (
    <>
      <h1 className="vb-title">{collection.title}</h1>
      {collection.summary && <p className="vb-lead">{collection.summary}</p>}
      <p className="vb-meta">
        {t('public.counts', {
          published: collection.published_count,
          announced: collection.announced_count,
        })}
      </p>
      {collection.kind === 'learning' && <BrowserProgress />}
      {collection.kind === 'learning' && (
        <p>
          {t('progress.count', {
            n: collection.items.filter((i) => i.completed).length,
            total: collection.published_count,
          })}
        </p>
      )}
      <Outline collection={collection} />
    </>
  );
}

function ContentBody({
  content,
  position,
  total,
}: {
  content: PublicContent;
  position?: number;
  total?: number;
}) {
  const { t, locale, date } = useI18n();
  const { data } = usePublicConfiguration();
  const terms = useResource<{ items: TaxonomyTerm[] }>('/api/v1/public/taxonomy?locale=' + locale);
  const author = data?.profile.name[locale] || data?.profile.name.en || data?.site.wordmark;
  const category = terms.data?.items.find(
    (term) => term.kind === 'category' && term.key === content.category_key,
  );
  const cover = content.media?.find((m) => m.id === content.cover_media_id && m.kind === 'image');
  return (
    <article className={'public-reader public-reader-' + content.kind}>
      <header className="vb-art-head">
        {content.kind === 'lesson' && position !== undefined && position >= 0 && (
          <span className="vb-sticker blue">
            {t('public.lessonPosition', { n: position + 1, total: total || 0 })}
          </span>
        )}
        {category ? (
          <Link
            className="vb-catlink"
            to={'/' + locale + '/writing?category=' + encodeURIComponent(category.slug)}
          >
            <span className="vb-sticker plain">
              {category.label} · {t('public.' + content.kind)}
            </span>
          </Link>
        ) : (
          <span className="vb-sticker plain">{t('public.' + content.kind)}</span>
        )}
        <h1 className="vb-title">{content.title}</h1>
        {content.summary && <p className="vb-lead">{content.summary}</p>}
        <div className="vb-art-meta">
          {author && (
            <span className="public-article-author ds-ui">
              <span className="vb-avatar" aria-hidden="true" dir="ltr">
                {data?.profile.portrait_media_id ? (
                  <img src={'/media/' + data.profile.portrait_media_id} alt="" />
                ) : (
                  data?.site.monogram
                )}
              </span>
              {author}
            </span>
          )}
          <span className="vb-meta">
            <time dateTime={content.published_at}>
              {date(content.published_at, { year: 'numeric', month: 'short', day: 'numeric' })}
            </time>
            {!!content.reading_minutes && (
              <> · {t('public.readTime', { n: content.reading_minutes })}</>
            )}
          </span>
        </div>
      </header>
      {content.translation_stale && <p className="public-notice">{t('public.stale')}</p>}
      {content.locked ? (
        <div className="public-locked" role="status">
          <h2>{t('public.locked')}</h2>
          <p>{t('public.lockHelp')}</p>
          <ReaderLearning content={content} />
        </div>
      ) : (
        <>
          {cover && (
            <img
              className="public-cover"
              src={cover.url}
              alt=""
              width={cover.width}
              height={cover.height}
            />
          )}
          <MarkdownReader
            markdown={content.markdown || ''}
            media={content.media}
            presentations={content.media_presentations}
          />
          <ReaderLearning content={content} />
          <div className="vb-tags public-tags">
            {content.tag_keys?.map((key) => {
              const term = terms.data?.items.find((v) => v.kind === 'tag' && v.key === key);
              return (
                term && (
                  <Link
                    key={key}
                    to={'/' + locale + '/writing?tag=' + encodeURIComponent(term.slug)}
                  >
                    <bdi>{term.label}</bdi>
                  </Link>
                )
              );
            })}
          </div>
        </>
      )}
      {!content.locked && author && (
        <AuthorCard
          name={author}
          monogram={data?.site.monogram || ''}
          avatarUrl={
            data?.profile.portrait_media_id ? '/media/' + data.profile.portrait_media_id : undefined
          }
          bio={renderBio(data?.profile.bio[locale] || data?.profile.bio.en || '')}
          moreHref={'/' + locale + '/writing'}
        />
      )}
    </article>
  );
}

export function ReaderPage({ page }: { page: PageRoute }) {
  const { t } = useI18n();
  const content = page.content!;
  const c = page.collection;
  const position = c?.items.findIndex((i) => i.key === content.key) ?? -1;
  if (content.kind === 'doc' && !c) {
    return <DocsIndex current={content} />;
  }
  const neighbors = position >= 0 ? [c?.items[position - 1], c?.items[position + 1]] : [];
  return (
    <>
      <div className={c ? 'vb-split public-reading-layout' : ''}>
        {c && <Outline collection={c} current={content.key} />}
        <div className="public-reading-main">
          <ContentBody content={content} position={position} total={c?.total} />
          {c && (
            <nav className="vb-lessonnav public-neighbors" aria-label={t('public.outline')}>
              {neighbors.map(
                (item, i) =>
                  item &&
                  (item.url && item.state === 'open' ? (
                    <Link key={i} className={i === 1 ? 'next' : undefined} to={item.url}>
                      <span className="vb-meta">
                        {t(i === 0 ? 'public.previous' : 'public.next')} ·{' '}
                        {t('public.' + item.state)}
                      </span>
                      <b>{item.title}</b>
                    </Link>
                  ) : (
                    <div key={i} className="public-neighbor-soon">
                      <span className="vb-meta">
                        {t(i === 0 ? 'public.previous' : 'public.next')} ·{' '}
                        {t('public.' + item.state)}
                      </span>
                      <b>{item.title}</b>
                    </div>
                  )),
              )}
            </nav>
          )}
        </div>
      </div>
    </>
  );
}
