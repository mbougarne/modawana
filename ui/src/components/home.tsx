import { Fragment, type ReactNode } from 'react';
import type { Profile } from '../types';
import { useI18n } from '../i18n/I18nProvider';
import { Icon } from './Icon';
import { Button } from './primitives';

/** Headline syntax: new line = line break, *word* = primary colour. */
export function renderHeadline(text: string): ReactNode {
  return text.split('\n').map((line, i, all) => (
    <Fragment key={i}>
      {line
        .split(/(\*[^*]+\*)/g)
        .map((part, j) =>
          part.startsWith('*') && part.endsWith('*') && part.length > 2 ? (
            <em key={j}>{part.slice(1, -1)}</em>
          ) : (
            part
          ),
        )}
      {i < all.length - 1 && <br />}
    </Fragment>
  ));
}

/** Bio syntax: **words** = bold. */
export function renderBio(text: string): ReactNode {
  return text
    .split(/(\*\*[^*]+\*\*)/g)
    .map((part, i) =>
      part.startsWith('**') && part.endsWith('**') ? <b key={i}>{part.slice(2, -2)}</b> : part,
    );
}

/** Photo, or the monogram on the zellige star when there is no photo yet. */
export function Portrait({
  src,
  alt,
  monogram,
  sticker,
}: {
  src?: string;
  alt: string;
  monogram: string;
  sticker?: string;
}) {
  const { dir } = useI18n();
  return (
    <div className="vb-portrait" role={src ? undefined : 'img'} aria-label={src ? undefined : alt}>
      {src ? (
        <img className="vb-photo" src={src} alt={alt} />
      ) : (
        <svg viewBox="0 0 320 320" preserveAspectRatio="xMidYMid slice" aria-hidden="true">
          <circle className="sun" cx={dir === 'rtl' ? 58 : 262} cy="62" r="34" />
          <rect className="star" x="70" y="70" width="180" height="180" rx="14" />
          <rect
            className="star"
            x="70"
            y="70"
            width="180"
            height="180"
            rx="14"
            transform="rotate(45 160 160)"
          />
          <text className="ini" x="160" y="194" textAnchor="middle">
            {monogram}
          </text>
        </svg>
      )}
      {sticker && <span className="vb-sticker">{sticker}</span>}
    </div>
  );
}

/** The home header: headline, bio, main button + profile links, and the portrait. */
export function Hero({
  profile,
  monogram,
  writingHref,
}: {
  profile: Profile;
  monogram: string;
  writingHref: string;
}) {
  const { t, pick } = useI18n();
  return (
    <section className="vb-hero">
      <div>
        <h1>{renderHeadline(pick(profile.headline, 'profile') ?? '')}</h1>
        <p className="vb-lead">{renderBio(pick(profile.bio, 'profile') ?? '')}</p>
        <div className="vb-socials">
          <Button
            variant="primary"
            href={profile.mainAction?.visible ? profile.mainAction.url : writingHref}
          >
            {profile.mainAction?.visible
              ? pick(profile.mainAction.label, 'profile')
              : t('home.cta')}
          </Button>
          {profile.links
            .filter((l) => l.visible)
            .map((l) => {
              const ic = l.icon;
              return (
                <Button
                  key={l.id}
                  href={l.url}
                  variant={ic === 'rss' ? 'quiet' : 'secondary'}
                  icon={ic && <Icon name={ic} size={17} />}
                >
                  {pick(l.label, 'profile')}
                </Button>
              );
            })}
        </div>
      </div>
      <Portrait
        src={profile.avatarUrl}
        alt={pick(profile.avatarAlt, 'profile') ?? ''}
        monogram={monogram}
        sticker={pick(profile.sticker, 'profile')}
      />
    </section>
  );
}

export function NowCard({ profile }: { profile: Profile }) {
  const { t, pick, date } = useI18n();
  const items = profile.now.filter((n) => pick(n.text, 'profile'));
  if (!profile.home.showNow || items.length === 0) {
    return null;
  }
  return (
    <section className="vb-now" aria-labelledby="now-h">
      <div>
        <h2 className="vb-h2" id="now-h">
          <span className="vb-live" aria-hidden="true" />
          {t('home.now')}
        </h2>
        {profile.nowUpdated && (
          <span className="vb-meta">
            {t('home.updated', {
              date: date(profile.nowUpdated + '-01', { month: 'long', year: 'numeric' }),
            })}
          </span>
        )}
      </div>
      <ul>
        {items.map((n) => (
          <li key={n.id}>
            <span>{pick(n.label, 'profile')}</span>
            {pick(n.text, 'profile')}
          </li>
        ))}
      </ul>
    </section>
  );
}

export function SectionHead({
  title,
  moreLabel,
  moreHref,
}: {
  title: string;
  moreLabel?: string;
  moreHref?: string;
}) {
  const { dir } = useI18n();
  return (
    <div className="vb-sechead">
      <h2 className="vb-h2">{title}</h2>
      {moreHref && (
        <a className="vb-link" href={moreHref}>
          {moreLabel} {dir === 'rtl' ? '←' : '→'}
        </a>
      )}
    </div>
  );
}
