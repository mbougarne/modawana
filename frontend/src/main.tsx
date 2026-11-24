import { isLocale } from '../../ui/src/i18n/locales';
import { lazy, Suspense } from 'react';
import { createRoot } from 'react-dom/client';
import {
  createBrowserRouter,
  createRoutesFromElements,
  RouterProvider,
  Navigate,
  Route,
  useParams,
} from 'react-router-dom';
import { I18nProvider, ThemeProvider, Label, type Locale } from '@ui';
import '@ui/styles/index.css';
import './features/owner/owner.css';
import './features/owner/slate-theme.css';
import './pages/public/public.css';
import {
  PublicConfigurationProvider,
  usePublicConfiguration,
} from './features/configuration/PublicConfiguration';
import type { ConfigKind } from './api/configuration';
import { OAuthPage } from './features/auth/OAuthPage';
import { AuthPage } from './features/auth/AuthPage';

const StaffPage = lazy(() =>
  import('./features/auth/StaffPage').then((m) => ({ default: m.StaffPage })),
);
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
  const chosen = isLocale(locale) ? locale : 'en';
  return <Navigate to={`/${chosen}/login`} replace />;
}

function LocaleRoot({
  auth = false,
  staff = false,
  oauth = false,
  preview = false,
  config,
  owner,
}: {
  auth?: boolean;
  staff?: boolean;
  oauth?: boolean;
  preview?: boolean;
  config?: ConfigKind | 'themes' | 'menus';
  owner?:
    | 'posts'
    | 'pages'
    | 'editor'
    | 'media'
    | 'collections'
    | 'taxonomy'
    | 'transfer'
    | 'plugins'
    | 'forms'
    | 'social'
    | 'analytics'
    | 'editorial'
    | 'store'
    | 'dashboard'
    | 'layout';
}) {
  const { locale } = useParams();
  const { data } = usePublicConfiguration();
  const valid = isLocale(locale);
  const chosen: Locale = valid ? locale : 'en';
  return (
    <I18nProvider locale={chosen} overrides={data?.labels}>
      <Suspense fallback={<p role="status">…</p>}>
        {staff && valid ? (
          <StaffPage />
        ) : oauth && valid ? (
          <OAuthPage />
        ) : owner && valid ? (
          <OwnerArea key={owner} page={owner} />
        ) : config && valid ? (
          <ConfigPage key={config} page={config} />
        ) : preview && valid ? (
          <PreviewPage />
        ) : auth && valid ? (
          <AuthPage />
        ) : (
          <h1>
            <Label name="foundation.notFound" />
          </h1>
        )}
      </Suspense>
    </I18nProvider>
  );
}
const router = createBrowserRouter(
  createRoutesFromElements(
    <>
      <Route path="/admin/login" element={<LoginAlias />} />
      <Route path="/:locale/login" element={<LocaleRoot auth />} />
      <Route path="/:locale/admin/login" element={<LoginAlias />} />
      <Route path="/:locale/staff" element={<LocaleRoot staff />} />
      <Route path="/:locale/admin" element={<LocaleRoot auth />} />
      <Route path="/:locale/admin/collections" element={<LocaleRoot owner="collections" />} />
      <Route path="/:locale/admin/taxonomy" element={<LocaleRoot owner="taxonomy" />} />
      <Route path="/:locale/admin/dashboard" element={<LocaleRoot owner="dashboard" />} />
      <Route path="/:locale/admin/layout" element={<LocaleRoot owner="layout" />} />
      <Route path="/:locale/admin/posts" element={<LocaleRoot owner="posts" />} />
      <Route path="/:locale/admin/pages" element={<LocaleRoot owner="pages" />} />
      <Route path="/:locale/admin/new" element={<LocaleRoot owner="editor" />} />
      <Route
        path="/:locale/admin/editor/:key/:contentLocale"
        element={<LocaleRoot owner="editor" />}
      />
      <Route path="/:locale/admin/transfer" element={<LocaleRoot owner="transfer" />} />
      <Route path="/:locale/admin/social" element={<LocaleRoot owner="social" />} />
      <Route path="/:locale/admin/analytics" element={<LocaleRoot owner="analytics" />} />
      <Route path="/:locale/admin/editorial" element={<LocaleRoot owner="editorial" />} />
      <Route path="/:locale/admin/store" element={<LocaleRoot owner="store" />} />
      <Route path="/:locale/admin/forms" element={<LocaleRoot owner="forms" />} />
      <Route path="/:locale/admin/plugins" element={<LocaleRoot owner="plugins" />} />
      <Route path="/:locale/admin/media" element={<LocaleRoot owner="media" />} />
      <Route path="/:locale/admin/oauth" element={<LocaleRoot oauth />} />
      <Route path="/:locale/admin/security" element={<LocaleRoot auth />} />
      <Route path="/:locale/admin/preview/:key/:contentLocale" element={<LocaleRoot preview />} />
      {(['settings', 'themes', 'menus', 'profile', 'labels'] as const).map((k) => (
        <Route key={k} path={`/:locale/admin/${k}`} element={<LocaleRoot config={k} />} />
      ))}
      <Route path="/:locale/admin/*" element={<LocaleRoot />} />
    </>,
  ),
);

createRoot(document.getElementById('root')!).render(
  <ThemeProvider>
    <PublicConfigurationProvider>
      <RouterProvider router={router} />
    </PublicConfigurationProvider>
  </ThemeProvider>,
);
