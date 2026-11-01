import { useState, type ReactNode } from 'react';
import type { Brand } from '../types';
import type { LabelKey } from '../i18n/labels';
import { useI18n } from '../i18n/I18nProvider';
import { Icon } from './Icon';
import { IconButton } from './primitives';

/** The site logo from the owner's Brand settings: wordmark (last "." takes the primary colour), monogram or image. */
export function Logo({ brand, href = '/' }: { brand: Brand; href?: string }) {
  const l = brand.logo;
  let inner: ReactNode;
  if (l.kind === 'image') {
    inner = <img src={l.url} alt={l.alt} />;
  } else if (l.kind === 'monogram') {
    inner = <span className="vb-mono">{l.letters}</span>;
  } else {
    const i = l.text.lastIndexOf('.');
    inner =
      i > 0 ? (
        <>
          {l.text.slice(0, i)}
          <span>.</span>
          {l.text.slice(i + 1)}
        </>
      ) : (
        l.text
      );
  }
  return (
    <a className="vb-brand" href={href} dir="auto">
      {inner}
    </a>
  );
}

/** Page container: max width var(--shell) with responsive gutters. Wrap each page in <div className="vb"> once. */
export function Shell({ children }: { children: ReactNode }) {
  return <div className="vb-shell">{children}</div>;
}

export interface NavItem {
  key: LabelKey;
  href: string;
}

export function SiteHeader({
  brand,
  items,
  current,
  onSearch,
  onToggleTheme,
  otherLocale,
}: {
  brand: Brand;
  items: NavItem[];
  /** key of the current section, e.g. 'nav.articles' */
  current?: LabelKey;
  onSearch?: () => void;
  onToggleTheme?: () => void;
  /** e.g. { label: 'ع', title: 'العربية', href: '/ar' } */
  otherLocale?: { label: string; title: string; href: string };
}) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  return (
    <header className="vb-header" data-open={open}>
      <Logo brand={brand} />
      <nav className="vb-nav" aria-label={t('nav.main')} id="site-nav">
        {items.map((it) => (
          <a key={it.key} href={it.href} aria-current={it.key === current ? 'page' : undefined}>
            {t(it.key)}
          </a>
        ))}
      </nav>
      <div className="vb-tools">
        {onSearch && (
          <IconButton label={t('nav.search')} onClick={onSearch}>
            <Icon name="search" size={19} />
          </IconButton>
        )}
        {otherLocale && (
          <a
            className="vb-icon vb-hide-sm"
            href={otherLocale.href}
            hrefLang={otherLocale.href.split('/')[1]}
            title={otherLocale.title}
            aria-label={otherLocale.title}
          >
            {otherLocale.label}
          </a>
        )}
        {onToggleTheme && (
          <IconButton label={t('nav.theme')} onClick={onToggleTheme}>
            <Icon name="moon" size={19} />
          </IconButton>
        )}
        <IconButton
          label={t('nav.menu')}
          className="vb-menu"
          aria-expanded={open}
          aria-controls="site-nav"
          onClick={() => setOpen((o) => !o)}
        >
          <Icon name="menu" size={19} />
        </IconButton>
      </div>
    </header>
  );
}

export function SiteFooter({
  name,
  place,
  contactHref,
  links,
}: {
  name: string;
  place?: string;
  contactHref: string;
  links: { label: string; href: string }[];
}) {
  const { t } = useI18n();
  return (
    <footer className="vb-footer">
      <p className="big">
        {t('footer.question')} <a href={contactHref}>{t('footer.sayhi')}</a>
      </p>
      <div className="row">
        <span className="vb-meta">
          © {new Date().getFullYear()} {name}
          {place ? ` · ${place}` : ''}
        </span>
        <nav aria-label={t('footer.links')}>
          {links.map((l) => (
            <a key={l.href} href={l.href}>
              {l.label}
            </a>
          ))}
        </nav>
      </div>
    </footer>
  );
}
