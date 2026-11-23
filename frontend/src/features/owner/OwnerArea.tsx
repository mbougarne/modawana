import { FormsPage } from './FormsPage';
import { Portability } from './Portability';
import { Plugins } from './Plugins';
import { DashboardLayoutSettings, OwnerDashboard } from './DashboardScreens';
import { Organization } from './Organization';
import { OwnerPageHeading } from './OwnerWorkspace';
import { useI18n } from '@ui';
import { OwnerFrame } from './OwnerFrame';
import { OwnerPosts } from './OwnerPosts';
import { EditorPage } from '../../pages/admin/editor/EditorPage';
import { MediaLibrary } from './MediaLibrary';

export function OwnerArea({
  page,
}: {
  page:
    | 'posts'
    | 'pages'
    | 'editor'
    | 'media'
    | 'collections'
    | 'taxonomy'
    | 'transfer'
    | 'plugins'
    | 'forms'
    | 'dashboard'
    | 'layout';
}) {
  const { t } = useI18n();
  return (
    <OwnerFrame current={page === 'editor' ? 'posts' : page}>
      {(csrf) =>
        page === 'posts' || page === 'pages' ? (
          <OwnerPosts csrf={csrf} pagesOnly={page === 'pages'} />
        ) : page === 'editor' ? (
          <EditorPage csrf={csrf} />
        ) : page === 'transfer' ? (
          <Portability csrf={csrf} />
        ) : page === 'dashboard' ? (
          <OwnerDashboard />
        ) : page === 'layout' ? (
          <DashboardLayoutSettings csrf={csrf} />
        ) : page === 'forms' ? (
          <FormsPage csrf={csrf} />
        ) : page === 'plugins' ? (
          <Plugins csrf={csrf} />
        ) : page === 'media' ? (
          <>
            <OwnerPageHeading title={t('owner.media')} description={t('workspace.mediaHelp')} />
            <MediaLibrary csrf={csrf} />
          </>
        ) : (
          <Organization page={page} csrf={csrf} />
        )
      }
    </OwnerFrame>
  );
}
