import { Icon } from './Icon';
import { useState, type ReactNode } from 'react';
import type { Category, PostSummary } from '../types';
import { useI18n } from '../i18n/I18nProvider';
import { Chip, TagChip, Tag } from './primitives';

/** Posts grouped by year, newest first. Articles show their category; lessons and docs show their kind. */
export function PostList({ posts }: { posts: PostSummary[] }) {
  const { t, date } = useI18n();
  const years = [...new Set(posts.map((p) => p.publishedAt.slice(0, 4)))].sort().reverse();
  return (
    <>
      {years.map((y) => (
        <div className="vb-year" key={y}>
          <h3>{y}</h3>
          <ul className="vb-posts">
            {posts
              .filter((p) => p.publishedAt.startsWith(y))
              .map((p) => {
                const Row = p.locked ? 'div' : 'a';
                return (
                  <li className="vb-post" key={p.id}>
                    <Row
                      href={p.locked ? undefined : p.href}
                      className={p.locked ? 'vb-post-locked' : undefined}
                    >
                      <time dateTime={p.publishedAt}>
                        {date(p.publishedAt, { day: 'numeric', month: 'short' })}
                      </time>
                      <span className="vb-post-main">
                        <b>{p.title}</b>
                        {p.tags.length > 0 && (
                          <span className="vb-post-tags" dir="ltr">
                            {p.tags.map((x) => '#' + x).join(' · ')}
                          </span>
                        )}
                      </span>
                      {p.kind === 'article' ? (
                        p.category ? (
                          <Tag>{p.category.label}</Tag>
                        ) : (
                          <span />
                        )
                      ) : (
                        <Tag tone={p.kind}>
                          {t(`kind.${p.kind}`)}
                          {p.locked && (
                            <>
                              <Icon name="lock" />
                              <span className="vb-visually-hidden">{t('lesson.lockedShort')}</span>
                            </>
                          )}
                        </Tag>
                      )}
                      <span className="vb-arrow" aria-hidden="true">
                        {p.locked ? <Icon name="lock" /> : '→'}
                      </span>
                    </Row>
                  </li>
                );
              })}
          </ul>
        </div>
      ))}
    </>
  );
}

export interface Filters {
  category: string;
  tag: string;
}

/** Category + tag filters (AND). A second click on a tag clears it. Keep `value` in the URL (?category=&tag=). */
export function FilterBar({
  categories,
  tags,
  counts,
  value,
  onChange,
  resultCount,
}: {
  categories: Category[];
  tags: string[];
  /** published posts per category id, plus 'all' */
  counts?: Record<string, number>;
  value: Filters;
  onChange: (f: Filters) => void;
  resultCount: number;
}) {
  const { t } = useI18n();
  const active = value.category || value.tag;
  const cat = categories.find((c) => c.id === value.category);
  return (
    <div className="vb-filterbar">
      <div className="vb-filterrow" role="group" aria-label={t('writing.category')}>
        <span className="vb-meta">{t('writing.category')}</span>
        <Chip
          pressed={!value.category}
          count={counts?.all}
          onClick={() => onChange({ ...value, category: '' })}
        >
          {t('writing.all')}
        </Chip>
        {categories.map((c) => (
          <Chip
            key={c.id}
            pressed={value.category === c.id}
            count={counts?.[c.id]}
            onClick={() => onChange({ ...value, category: c.id })}
          >
            {c.label}
          </Chip>
        ))}
      </div>
      <div className="vb-filterrow" role="group" aria-label={t('writing.tags')}>
        <span className="vb-meta">{t('writing.tags')}</span>
        {tags.map((x) => (
          <TagChip
            key={x}
            tag={x}
            pressed={value.tag === x}
            onClick={() => onChange({ ...value, tag: value.tag === x ? '' : x })}
          />
        ))}
      </div>
      {active && (
        <div className="vb-active" role="status">
          <span>{t('writing.showing')}:</span>
          <span>
            {cat && <b>{cat.label}</b>}
            {cat && value.tag && ' + '}
            {value.tag && <b dir="ltr">#{value.tag}</b>}
          </span>
          <span className="vb-meta">{t('writing.count', { n: resultCount })}</span>
          <button
            type="button"
            className="vb-clear"
            onClick={() => onChange({ category: '', tag: '' })}
          >
            {t('writing.clear')}
          </button>
        </div>
      )}
    </div>
  );
}

export function EmptyState({ children, action }: { children: ReactNode; action?: ReactNode }) {
  return (
    <div className="vb-empty">
      {children}
      {action && (
        <>
          <br />
          {action}
        </>
      )}
    </div>
  );
}

export function ArticleTags({
  tags,
  hrefFor,
}: {
  tags: string[];
  hrefFor: (tag: string) => string;
}) {
  const { t } = useI18n();
  if (!tags.length) {
    return null;
  }
  return (
    <div className="vb-tags">
      <span className="vb-meta">{t('post.tagged')}</span>
      {tags.map((x) => (
        <a key={x} href={hrefFor(x)} dir="ltr">
          #{x}
        </a>
      ))}
    </div>
  );
}

/** Wrap rendered Markdown (react-markdown output) in this for the reading typography. */
export function Prose({ children }: { children: ReactNode }) {
  const { locale } = useI18n();
  return (
    <article className="vb-prose" lang={locale}>
      {children}
    </article>
  );
}

/** Dark code block with filename bar and Copy. Always LTR. Use as react-markdown's `pre` renderer. */
export function CodeBlock({ code, filename }: { code: string; filename?: string }) {
  const { t } = useI18n();
  const [copied, setCopied] = useState(false);
  const copy = () =>
    navigator.clipboard?.writeText(code).then(
      () => {
        setCopied(true);
        setTimeout(() => setCopied(false), 1400);
      },
      () => {},
    );
  return (
    <figure className="vb-code" dir="ltr">
      <header>
        <i />
        <i />
        <i />
        &nbsp;{filename ?? t('reader.code')}
        <button type="button" onClick={copy}>
          {copied ? t('button.copied') : t('button.copy')}
        </button>
      </header>
      <pre>
        <code>{code}</code>
      </pre>
    </figure>
  );
}

/** Image or video figure for media referenced in Markdown (/media/<id>). */
export function MediaFigure({
  kind,
  src,
  caption,
  poster,
}: {
  kind: 'image' | 'video';
  src: string;
  caption?: string;
  poster?: string;
}) {
  return (
    <figure className="vb-media">
      {kind === 'video' ? (
        <video controls playsInline preload="metadata" src={src} poster={poster} title={caption} />
      ) : (
        <img src={src} alt={caption ?? ''} loading="lazy" />
      )}
      {caption && <figcaption>{caption}</figcaption>}
    </figure>
  );
}

export function AuthorCard({
  name,
  monogram,
  avatarUrl,
  bio,
  moreHref,
}: {
  name: string;
  monogram: string;
  avatarUrl?: string;
  bio: ReactNode;
  moreHref: string;
}) {
  const { t, dir } = useI18n();
  return (
    <aside className="vb-author">
      <span className="vb-avatar" aria-hidden="true" dir="ltr">
        {avatarUrl ? <img src={avatarUrl} alt="" /> : monogram}
      </span>
      <div>
        <b>{t('post.writtenBy', { name })}</b>
        <p>{bio}</p>
        <a className="vb-link" href={moreHref}>
          {t('post.moreWriting')} {dir === 'rtl' ? '←' : '→'}
        </a>
      </div>
    </aside>
  );
}
