import { lazy, Suspense } from 'react';
import { createRoot } from 'react-dom/client';
import { BrowserRouter, Navigate, Route, Routes, useParams } from 'react-router-dom';
import { I18nProvider, ThemeProvider, type Locale } from '@ui';
import '@ui/styles/index.css';
import './features/owner/owner.css';
import {
  PublicConfigurationProvider,
  usePublicConfiguration,
} from './features/configuration/PublicConfiguration';
import type { ConfigKind } from './api/configuration';
import { OAuthPage } from './features/auth/OAuthPage';
import { AuthPage } from './features/auth/AuthPage';
import { PublicPage } from './pages/public/PublicPage';

const OwnerArea = lazy(() =>
  import('./features/owner/OwnerArea').then((m) => ({ default: m.OwnerArea })),
);

const PreviewPage = lazy(() =>
  import('./features/markdown/PreviewPage').then((m) => ({ default: m.PreviewPage })),
);
const ConfigPage = lazy(() =>
  import('./features/configuration/ConfigPage').then((m) => ({ default: m.ConfigPage })),
);

function LoginAlias() {
  const { locale } = useParams();
  const chosen = locale === 'ar' || locale === 'fr' ? locale : 'en';
  return <Navigate to={`/${chosen}/login`} replace />;
}

function LocaleRoot({
  notFound = false,
  auth = false,
  oauth = false,
  preview = false,
  config,
  owner,
}: {
  notFound?: boolean;
  auth?: boolean;
  oauth?: boolean;
  preview?: boolean;
  config?: ConfigKind;
  owner?: 'posts' | 'editor' | 'media' | 'collections' | 'taxonomy' | 'transfer' | 'plugins';
}) {
  const { locale } = useParams();
  const { data } = usePublicConfiguration();
  const valid = locale === 'en' || locale === 'ar' || locale === 'fr';
  const chosen: Locale = valid ? locale : 'en';
  return (
    <I18nProvider locale={chosen} overrides={data?.labels}>
      <Suspense fallback={<p role="status">…</p>}>
        {oauth && valid ? (
          <OAuthPage />
        ) : owner && valid ? (
          <OwnerArea page={owner} />
        ) : config && valid ? (
          <ConfigPage key={config} kind={config} />
        ) : preview && valid ? (
          <PreviewPage />
        ) : auth && valid ? (
          <AuthPage />
        ) : (
          <PublicPage invalid={notFound || !valid} />
        )}
      </Suspense>
    </I18nProvider>
  );
}
createRoot(document.getElementById('root')!).render(
  <ThemeProvider>
    <PublicConfigurationProvider>
      <BrowserRouter>
        <Routes>
          <Route path="/" element={<Navigate to="/en" replace />} />
          <Route path="/admin/login" element={<LoginAlias />} />
          <Route path="/:locale" element={<LocaleRoot />} />
          <Route path="/:locale/login" element={<LocaleRoot auth />} />
          <Route path="/:locale/admin/login" element={<LoginAlias />} />
          <Route path="/:locale/admin" element={<LocaleRoot auth />} />
          <Route path="/:locale/admin/collections" element={<LocaleRoot owner="collections" />} />
          <Route path="/:locale/admin/taxonomy" element={<LocaleRoot owner="taxonomy" />} />
          <Route path="/:locale/admin/posts" element={<LocaleRoot owner="posts" />} />
          <Route path="/:locale/admin/new" element={<LocaleRoot owner="editor" />} />
          <Route
            path="/:locale/admin/editor/:key/:contentLocale"
            element={<LocaleRoot owner="editor" />}
          />
          <Route path="/:locale/admin/transfer" element={<LocaleRoot owner="transfer" />} />
          <Route path="/:locale/admin/plugins" element={<LocaleRoot owner="plugins" />} />
          <Route path="/:locale/admin/media" element={<LocaleRoot owner="media" />} />
          <Route path="/:locale/admin/oauth" element={<LocaleRoot oauth />} />
          <Route path="/:locale/admin/security" element={<LocaleRoot auth />} />
          <Route
            path="/:locale/admin/preview/:key/:contentLocale"
            element={<LocaleRoot preview />}
          />
          {(['settings', 'profile', 'labels'] as const).map((k) => (
            <Route key={k} path={`/:locale/admin/${k}`} element={<LocaleRoot config={k} />} />
          ))}
          <Route path="/:locale/*" element={<LocaleRoot />} />
        </Routes>
      </BrowserRouter>
    </PublicConfigurationProvider>
  </ThemeProvider>,
);
