import { Portability } from './Portability';
import { Plugins } from './Plugins';
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
  page: 'posts' | 'editor' | 'media' | 'collections' | 'taxonomy' | 'transfer' | 'plugins';
}) {
  const { t } = useI18n();
  return (
    <OwnerFrame current={page === 'editor' ? 'posts' : page}>
      {(csrf) =>
        page === 'posts' ? (
          <OwnerPosts csrf={csrf} />
        ) : page === 'editor' ? (
          <EditorPage csrf={csrf} />
        ) : page === 'transfer' ? (
          <Portability csrf={csrf} />
        ) : page === 'plugins' ? (
          <Plugins />
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
