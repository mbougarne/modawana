import { useId, useState, type ReactNode } from 'react';
import { Link } from 'react-router-dom';
import { Portrait, renderHeadline, renderBio, useI18n, Icon, type IconName } from '@ui';
import { usePublicConfiguration } from '../../features/configuration/PublicConfiguration';
import { MarkdownReader, safeLink } from '../../features/markdown/MarkdownReader';
import { iconNames } from '../admin/settings/siteBrand';
import type { DiscoveryPage, PublicContent } from '../../api/content';
import type { Localized } from '../../api/configuration';
import { useResource } from './api';
import { ResourceState } from './ResourceState';
import { EntryList } from './writing';
import { CollectionIndex } from './reading';

export function HomePage({ about = false }: { about?: boolean }) {
  const { data } = usePublicConfiguration();
  const { locale, t, date } = useI18n();
  const p = data?.profile;
  const [emailNotice, setEmailNotice] = useState('');
  const taxonomy = useResource<{ items: import('../../api/content').TaxonomyTerm[] }>(
    '/api/v1/public/taxonomy?locale=' + locale,
  );
  const bannerID = 'public-banner-' + useId().replace(/[^a-zA-Z0-9]/g, '');
  const writing = useResource<DiscoveryPage>(
    !about && p?.show_writing
      ? `/api/v1/public/content?locale=${locale}&limit=${p.writing_limit}`
      : undefined,
  );
  const narrative = useResource<PublicContent>(
    about && data?.about_url ? data.about_url : undefined,
  );
  const text = (v?: Localized) => v?.[locale] || v?.en || '';
  function localized(v: Localized | undefined, render: (s: string) => ReactNode = (s) => s) {
    const value = text(v);
    if (!value) {
      return null;
    }
    const actual = v?.[locale] ? locale : 'en';
    return (
      <span lang={actual} dir={actual === 'ar' ? 'rtl' : 'ltr'}>
        {render(value)}
      </span>
    );
  }
  if (!p) {
    return <p role="status">{t('public.loading')}</p>;
  }
  const portrait = (
    <Portrait
      src={p.portrait_media_id ? '/media/' + p.portrait_media_id : undefined}
      alt={text(p.portrait_alt) || text(p.name) || t('config.portrait')}
      monogram={data?.site.monogram || ''}
      sticker={text(p.sticker)}
    />
  );
  const actions = (
    <div className="vb-socials">
      {[...(p.main_action ? [p.main_action] : []), ...p.links]
        .filter((link) => link.visible && text(link.label) && safeLink(link.url))
        .map((link) => (
          <a
            key={link.id}
            className={
              'vb-btn ' +
              (link.id === p.main_action?.id
                ? 'vb-btn-primary'
                : link.icon === 'rss'
                  ? 'vb-btn-quiet public-rss'
                  : 'vb-btn-secondary')
            }
            href={safeLink(link.url)}
          >
            {link.icon && iconNames.includes(link.icon) && <Icon name={link.icon as IconName} />}{' '}
            {localized(link.label)}
          </a>
        ))}
    </div>
  );
  if (about) {
    return (
      <div className="vb-about public-about">
        <aside className="public-about-aside">
          {portrait}
          {p.experience.some((e) => text(e.role) || text(e.organization)) && (
            <section className="vb-card">
              <h2 className="vb-meta">{t('config.experience')}</h2>
              <ul className="vb-exp">
                {p.experience
                  .filter((e) => text(e.role) || text(e.organization))
                  .map((e) => (
                    <li key={e.id}>
                      <b>{localized(e.role)}</b>
                      <span>
                        {localized(e.organization)} · <bdi>{e.start}</bdi> —{' '}
                        {e.current ? t('config.current') : <bdi>{e.end}</bdi>}
                      </span>
                      {text(e.description) && <p>{localized(e.description)}</p>}
                      {e.url && (
                        <a className="vb-link" href={safeLink(e.url)}>
                          {t('config.website')}
                        </a>
                      )}
                    </li>
                  ))}
              </ul>
            </section>
          )}
        </aside>
        <div className="public-about-main">
          <h1 className="vb-title">{t('page.about')}</h1>
          {text(p.bio) && <p className="vb-lead">{localized(p.bio, renderBio)}</p>}
          <ResourceState resource={narrative} />
          {narrative.data && (
            <MarkdownReader
              markdown={narrative.data.markdown || ''}
              media={narrative.data.media}
              presentations={narrative.data.media_presentations}
            />
          )}
          {p.public_email && (
            <div className="vb-email">
              <a href={'mailto:' + p.public_email}>
                <bdi>{p.public_email}</bdi>
              </a>
              <button
                type="button"
                onClick={async () => {
                  try {
                    await navigator.clipboard.writeText(p.public_email);
                    setEmailNotice('public.emailCopied');
                  } catch {
                    setEmailNotice('public.emailCopyFailed');
                  }
                }}
              >
                {t('public.copyEmail')}
              </button>
            </div>
          )}
          {emailNotice && <p role="status">{t(emailNotice)}</p>}
          {text(p.location) && <p className="vb-meta">{localized(p.location)}</p>}
          {actions}
        </div>
      </div>
    );
  }
  return (
    <>
      {p.banner_enabled && p.banner_media_id && (
        <>
          <style
            nonce={document.querySelector<HTMLMetaElement>('meta[name="csp-nonce"]')?.content}
          >{`#${bannerID}{object-position:${Math.max(0, Math.min(100, p.banner_x))}% ${Math.max(0, Math.min(100, p.banner_y))}%;}`}</style>
          <img
            id={bannerID}
            className="public-banner"
            src={'/media/' + p.banner_media_id}
            alt={text(p.banner_alt)}
          />
        </>
      )}
      <section className="vb-hero public-hero">
        <div>
          <h1>
            {about
              ? t('page.about')
              : text(p.headline)
                ? localized(p.headline, renderHeadline)
                : t('page.home')}
          </h1>
          {text(p.bio) && <p className="vb-lead">{localized(p.bio, renderBio)}</p>}
          {actions}
        </div>
        {(p.portrait_media_id || text(p.name)) && (
          <Portrait
            src={p.portrait_media_id ? '/media/' + p.portrait_media_id : undefined}
            alt={text(p.portrait_alt) || text(p.name)}
            monogram={data?.site.monogram || ''}
            sticker={text(p.sticker)}
          />
        )}
      </section>
      {p.show_now && p.now.some((r) => text(r.text)) && (
        <section className="vb-now">
          <div>
            <h2 className="vb-h2">
              <span className="vb-live" aria-hidden="true" />
              {t('home.now')}
            </h2>
            {p.updated_month && (
              <time className="vb-meta" dateTime={p.updated_month}>
                {t('public.updated', {
                  date: date(p.updated_month + '-01', { year: 'numeric', month: 'long' }),
                })}
              </time>
            )}
          </div>
          <ul>
            {p.now
              .filter((r) => text(r.text))
              .map((r) => (
                <li key={r.id}>
                  <span>{localized(r.label)}</span>
                  <div className="public-now-text">{localized(r.text)}</div>
                </li>
              ))}
          </ul>
        </section>
      )}
      {!about && p.show_writing && (
        <section className="public-section">
          <div className="vb-sechead public-section-heading">
            <h2 className="vb-h2">{t('page.writing')}</h2>
            <Link className="vb-link" to={'/' + locale + '/writing'}>
              {t('home.everything')} {locale === 'ar' ? '←' : '→'}
            </Link>
          </div>
          <ResourceState resource={writing} />
          {writing.data && <EntryList items={writing.data.items} terms={taxonomy.data?.items} />}
        </section>
      )}
      {!about && p.show_learning && (
        <CollectionIndex kind="learning" limit={p.learning_limit} compact />
      )}
    </>
  );
}
