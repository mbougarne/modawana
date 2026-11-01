import { useEffect, useRef, useState, type ReactNode } from 'react';
import { Link, useLocation } from 'react-router-dom';
import { Icon, Logo, Shell, useI18n, useTheme } from '@ui';
import { siteBrand } from '../admin/settings/siteBrand';
import { usePublicConfiguration } from '../../features/configuration/PublicConfiguration';
import { useResource, type PageRoute } from './api';
import type { State } from '../../features/auth/api';
import { safeLink } from '../../features/markdown/MarkdownReader';

const sections = [
  ['', 'home'],
  ['writing', 'writing'],
  ['learn', 'learning'],
  ['docs', 'docs'],
  ['about', 'about'],
] as const;

export function PublicLayout({ children, page }: { children: ReactNode; page?: PageRoute }) {
  const { locale, t } = useI18n();
  const { toggle } = useTheme();
  const { data } = usePublicConfiguration();
  const location = useLocation();
  const [open, setOpen] = useState<false | 'language' | 'navigation'>(false);
  const [missing, setMissing] = useState(false);
  const languageTrigger = useRef<HTMLButtonElement>(null);
  const navigationTrigger = useRef<HTMLButtonElement>(null);
  const trigger = open === 'navigation' ? navigationTrigger : languageTrigger;
  const firstNavigation = useRef(true);
  const menu = useRef<HTMLDivElement>(null);
  const session = useResource<State>('/api/v1/session');
  useEffect(() => {
    setOpen(false);
    setMissing(false);
    if (firstNavigation.current) {
      firstNavigation.current = false;
    } else {
      document.getElementById('content')?.focus();
    }
  }, [location.pathname]);
  useEffect(() => {
    if (!open) {
      return;
    }
    menu.current?.querySelector<HTMLElement>('a, button')?.focus();
    function key(e: KeyboardEvent) {
      if (e.key === 'Escape') {
        setOpen(false);
        trigger.current?.focus();
      }
    }
    function outside(e: PointerEvent) {
      if (
        !menu.current?.contains(e.target as Node) &&
        !trigger.current?.contains(e.target as Node)
      ) {
        setOpen(false);
      }
    }
    document.addEventListener('keydown', key);
    document.addEventListener('pointerdown', outside);
    return () => {
      document.removeEventListener('keydown', key);
      document.removeEventListener('pointerdown', outside);
    };
  }, [open]);
  const profile = data?.profile;
  const section = location.pathname.split('/')[2] || '';
  const currentSection = section === 'articles' ? 'writing' : section === 'doc' ? 'docs' : section;
  const text = (value?: Partial<Record<string, string>>) => value?.[locale] || value?.en || '';
  return (
    <div className="vb public-site">
      <a className="vb-skip" href="#content">
        {t('foundation.skip')}
      </a>
      <Shell>
        <header className="vb-header public-header">
          {data && <Logo brand={siteBrand(data.site)} href={'/' + locale} />}
          <nav className="vb-nav public-nav" aria-label={t('nav.main')}>
            {sections
              .filter(([path]) => path !== '')
              .map(([path, label]) => (
                <Link
                  key={path}
                  to={'/' + locale + (path ? '/' + path : '')}
                  aria-current={
                    (path ? currentSection === path : location.pathname === '/' + locale)
                      ? 'page'
                      : undefined
                  }
                >
                  {t('slot.' + label)}
                </Link>
              ))}
          </nav>
          <div className="vb-tools public-tools">
            <Link className="vb-icon" to={'/' + locale + '/search'} aria-label={t('nav.search')}>
              <Icon name="search" />
            </Link>
            <button
              className="vb-icon public-language"
              ref={languageTrigger}
              aria-label={t('foundation.language')}
              aria-expanded={open === 'language'}
              aria-controls="language-menu"
              onClick={() => setOpen((v) => (v === 'language' ? false : 'language'))}
            >
              {locale === 'ar' ? 'ع' : locale.toUpperCase()}
            </button>
            <button className="vb-icon" aria-label={t('nav.theme')} onClick={toggle}>
              <Icon name="moon" />
            </button>
            <button
              className="vb-icon vb-menu"
              ref={navigationTrigger}
              aria-label={t('nav.menu')}
              aria-expanded={open === 'navigation'}
              aria-controls="navigation-menu"
              onClick={() => setOpen((v) => (v === 'navigation' ? false : 'navigation'))}
            >
              <Icon name="menu" />
            </button>
          </div>
          {open && (
            <div
              id={open === 'language' ? 'language-menu' : 'navigation-menu'}
              className="public-menu"
              ref={menu}
            >
              {open === 'navigation' && (
                <nav aria-label={t('nav.main')}>
                  {sections.map(([path, label]) => (
                    <Link key={path} to={'/' + locale + (path ? '/' + path : '')}>
                      {t('slot.' + label)}
                    </Link>
                  ))}
                </nav>
              )}
              {open === 'language' && (
                <nav aria-label={t('foundation.language')}>
                  {(['en', 'ar', 'fr'] as const).map((l) => {
                    const alt = page?.alternatives.find((a) => a.locale === l);
                    return alt ? (
                      <a
                        key={l}
                        href={alt.url}
                        lang={l}
                        dir={l === 'ar' ? 'rtl' : 'ltr'}
                        aria-current={l === locale ? 'page' : undefined}
                      >
                        {t('language.' + l)}
                      </a>
                    ) : (
                      <button key={l} onClick={() => setMissing(true)}>
                        {t('language.' + l)}
                      </button>
                    );
                  })}
                </nav>
              )}
              {missing && (
                <div role="status">
                  <p>{t('public.unavailable')}</p>
                  {(['en', 'ar', 'fr'] as const).map((l) => (
                    <a key={l} href={'/' + l}>
                      {t('public.homeLanguage', { language: t('language.' + l) })}
                    </a>
                  ))}
                </div>
              )}
            </div>
          )}
        </header>
        <main id="content" tabIndex={-1} className="public-main">
          {children}
        </main>
        <footer className="vb-footer">
          {text(profile?.footer) ? (
            <p className="big" dir="auto">
              {text(profile?.footer)}
            </p>
          ) : (
            profile?.public_email && (
              <p className="big">
                {t('footer.question')}{' '}
                <a href={'mailto:' + profile.public_email}>{t('footer.sayhi')}</a>
              </p>
            )
          )}
          <div className="row">
            <span className="vb-meta">
              {text(profile?.copyright) && <>© {text(profile?.copyright)}</>}
            </span>
            <nav aria-label={t('footer.links')}>
              <Link to={'/' + locale + '/writing'}>{t('slot.writing')}</Link>
              <Link to={'/' + locale + '/learn'}>{t('slot.learning')}</Link>
              <Link to={'/' + locale + '/docs'}>{t('slot.docs')}</Link>
              {['github', 'linkedin', 'rss'].map((icon) => {
                const link = [
                  ...(profile?.main_action ? [profile.main_action] : []),
                  ...(profile?.links || []),
                ].find((item) => item.icon === icon && item.visible && safeLink(item.url));
                return (
                  link && (
                    <a key={icon} href={safeLink(link.url)}>
                      {t('icon.' + icon)}
                    </a>
                  )
                );
              })}
              {session.data?.state === 'owner' && (
                <Link to={'/' + locale + '/admin'}>{t('public.owner')}</Link>
              )}
            </nav>
          </div>
        </footer>
      </Shell>
    </div>
  );
}
