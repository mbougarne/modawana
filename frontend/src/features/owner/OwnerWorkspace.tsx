import { useState, type ReactNode, type Ref } from 'react';
import { useI18n } from '../../../../ui/src/i18n/I18nProvider';
import { useTheme } from '../../../../ui/src/theme/ThemeProvider';
import { Button } from '../../../../ui/src/components/primitives';
import { Icon } from '../../../../ui/src/components/Icon';
import { OwnerLanguageMenu } from './OwnerLanguageMenu';
import { call } from '../auth/api';

const pages = [
  'posts',
  'collections',
  'taxonomy',
  'media',
  'settings',
  'profile',
  'labels',
  'security',
  'transfer',
  'plugins',
] as const;

export function OwnerNavigation({ current }: { current?: string } = {}) {
  const { locale, t } = useI18n();
  return (
    <nav className="owner-navigation vb-nav" aria-label={t('config.owner')}>
      {pages.map((page) => (
        <a
          key={page}
          href={`/${locale}/admin/${page}`}
          aria-current={page === current ? 'page' : undefined}
        >
          {t('owner.' + page)}
        </a>
      ))}
    </nav>
  );
}

export function OwnerPageHeading({
  title,
  description,
  children,
  headingRef,
}: {
  title: string;
  description?: string;
  children?: ReactNode;
  headingRef?: Ref<HTMLHeadingElement>;
}) {
  return (
    <div className="owner-page-heading">
      <div className="vb-pagehead">
        <h1 ref={headingRef} tabIndex={headingRef ? -1 : undefined}>
          {title}
        </h1>
        {description && <p>{description}</p>}
      </div>
      {children && <div className="owner-heading-actions">{children}</div>}
    </div>
  );
}

export function OwnerWorkspace({
  current,
  csrf,
  busy = false,
  children,
  className = '',
  mainId = 'owner-content',
  languagePath,
  onSignOut,
}: {
  current: string;
  csrf?: string;
  busy?: boolean;
  children: ReactNode;
  className?: string;
  mainId?: string;
  languagePath?: string;
  onSignOut?: () => Promise<void>;
}) {
  const { locale, dir, t } = useI18n();
  const { toggle } = useTheme();
  const [signingOut, setSigningOut] = useState(false);
  const [failed, setFailed] = useState(false);
  const path =
    (languagePath ??
      (window.location.pathname.replace(/^\/(en|ar|fr)(?=\/|$)/, '') || '/admin/' + current)) +
    window.location.search;
  async function signOut() {
    setSigningOut(true);
    setFailed(false);
    try {
      if (onSignOut) {
        await onSignOut();
      } else {
        await call('/session', 'DELETE', {}, csrf);
        window.location.assign('/' + locale + '/login');
      }
    } catch {
      setFailed(true);
    } finally {
      setSigningOut(false);
    }
  }
  return (
    <div className={`vb owner-workspace ds-ui ${className}`} lang={locale} dir={dir}>
      <a className="vb-skip" href={'#' + mainId}>
        {t('nav.skip')}
      </a>
      <div className="workspace-shell">
        <header className="workspace-header">
          <a className="vb-brand" href={'/' + locale + '/admin/posts'}>
            {t('config.owner')}
          </a>
          <div className="config-actions workspace-actions">
            <OwnerLanguageMenu path={path} />
            <Button href={'/' + locale} size="sm">
              {t('auth.viewSite')}
            </Button>
            <button type="button" className="vb-icon" aria-label={t('nav.theme')} onClick={toggle}>
              <Icon name="moon" />
            </button>
            <Button
              variant="quiet"
              size="sm"
              disabled={busy || signingOut || !csrf}
              onClick={() => void signOut()}
            >
              {t('auth.signout')}
            </Button>
          </div>
          <OwnerNavigation current={current} />
        </header>
        {failed && (
          <p role="alert" className="owner-notice">
            {t('auth.network')}
          </p>
        )}
        <main id={mainId} className="workspace-main config-main" aria-busy={busy}>
          {children}
        </main>
      </div>
    </div>
  );
}
