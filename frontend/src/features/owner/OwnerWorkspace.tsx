import { useState, type ReactNode, type Ref } from 'react';
import { Link, useLocation } from 'react-router-dom';
import { useI18n } from '../../../../ui/src/i18n/I18nProvider';
import { useTheme } from '../../../../ui/src/theme/ThemeProvider';
import { Button } from '../../../../ui/src/components/primitives';
import { Icon, type IconName } from '../../../../ui/src/components/Icon';
import { OwnerLanguageMenu } from './OwnerLanguageMenu';
import { OwnerAppearanceContext, useOwnerAppearance } from './OwnerAppearance';
import { call } from '../auth/api';
import { usePublicConfiguration } from '../configuration/PublicConfiguration';
import { useResource } from '../../pages/public/api';
import {
  ExtensionCatalog,
  localizedContribution,
  contributionKey,
  type RegisteredContribution,
} from './ExtensionCatalog';

const pages = [
  ['posts', 'ul'],
  ['pages', 'page'],
  ['collections', 'folder'],
  ['taxonomy', 'tag'],
  ['media', 'image'],
  ['themes', 'split'],
  ['menus', 'menu'],
  ['settings', 'settings'],
  ['profile', 'user'],
  ['labels', 'quote'],
  ['security', 'lock'],
  ['transfer', 'upload'],
  ['plugins', 'plug'],
] as const satisfies readonly (readonly [string, IconName])[];

export function OwnerNavigation({
  current,
  showIcons = false,
  contributions = [],
}: { current?: string; showIcons?: boolean; contributions?: RegisteredContribution[] } = {}) {
  const { locale, t } = useI18n();
  return (
    <nav className="owner-navigation vb-nav" aria-label={t('config.owner')}>
      {pages
        .filter(
          ([page]) => showIcons || page === current || (page !== 'themes' && page !== 'menus'),
        )
        .map(([page, icon]) => (
          <Link
            key={page}
            to={`/${locale}/admin/${page}`}
            aria-current={page === current ? 'page' : undefined}
          >
            {showIcons && <Icon name={icon} size={16} />}
            <span>{t('owner.' + page)}</span>
          </Link>
        ))}
      {contributions
        .filter((item) => item.kind === 'admin_navigation')
        .map((item) => (
          <Link
            key={contributionKey(item)}
            to={`/${locale}/admin/plugins#extension-${item.extension}${item.target === 'settings' ? ':settings' : ''}`}
          >
            {showIcons && <Icon name="plug" size={16} />}
            <span>{localizedContribution(item.label, locale)}</span>
          </Link>
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
  const { resolved, toggle } = useTheme();
  const location = useLocation();
  const appearance = useOwnerAppearance();
  const slate = appearance.theme === 'slate';
  const postsHref = `/${locale}/admin/posts`;
  const [signingOut, setSigningOut] = useState(false);
  const [failed, setFailed] = useState(false);
  const path =
    (languagePath ??
      (location.pathname.replace(/^\/(en|ar|fr)(?=\/|$)/, '') || '/admin/' + current)) +
    location.search;
  const { data: configuration } = usePublicConfiguration();
  const catalog = useResource<{ items: RegisteredContribution[] }>(
    csrf ? '/api/v1/extension-contributions' : undefined,
  );
  const siteHref =
    configuration?.site.public_locales?.includes(locale) === false ? '/' : `/${locale}`;

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
    <OwnerAppearanceContext.Provider value={appearance}>
      <div
        className={`vb owner-workspace ds-ui ${className}`}
        data-admin-theme={appearance.theme}
        data-admin-color-mode={resolved}
        lang={locale}
        dir={dir}
      >
        <a className="vb-skip" href={'#' + mainId}>
          {t('nav.skip')}
        </a>
        <div className="workspace-shell">
          <header className="workspace-header">
            <Link className="vb-brand" to={postsHref}>
              {t('config.owner')}
            </Link>
            {slate && (
              <nav className="workspace-quick-links" aria-label={t('ownerAppearance.quickActions')}>
                <Link to={`/${locale}/admin/new`}>
                  <Icon name="plus" size={16} />
                  {t('owner.new')}
                </Link>
                <a href={siteHref}>{t('auth.viewSite')}</a>
              </nav>
            )}
            <div className="config-actions workspace-actions">
              <OwnerLanguageMenu path={path} />
              {!slate && (
                <Button href={siteHref} size="sm">
                  {t('auth.viewSite')}
                </Button>
              )}
              <button
                type="button"
                className="vb-icon"
                aria-label={t('nav.theme')}
                onClick={toggle}
              >
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
          </header>
          <div className="workspace-navigation">
            {slate && (
              <Link className="vb-brand workspace-rail-brand" to={postsHref}>
                {t('config.owner')}
              </Link>
            )}
            <OwnerNavigation
              current={current}
              showIcons={slate}
              contributions={catalog.data?.items}
            />
          </div>
          <div className="workspace-content">
            {failed && (
              <p role="alert" className="owner-notice">
                {t('auth.network')}
              </p>
            )}
            <main id={mainId} className="workspace-main config-main" aria-busy={busy}>
              <ExtensionCatalog.Provider
                value={{ items: catalog.data?.items || [], refresh: catalog.retry }}
              >
                {children}
              </ExtensionCatalog.Provider>
            </main>
          </div>
        </div>
      </div>
    </OwnerAppearanceContext.Provider>
  );
}
