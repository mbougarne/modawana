import { useEffect, type ReactNode } from 'react';
import { redirectOwnerToLogin } from '../auth/navigation';
import { useI18n } from '@ui';
import { useResource } from '../../pages/public/api';
import { ResourceState } from '../../pages/public/ResourceState';
import type { State } from '../auth/api';
import { OwnerWorkspace } from './OwnerWorkspace';

export function OwnerFrame({
  current,
  children,
}: {
  current: string;
  children: (csrf: string) => ReactNode;
}) {
  const session = useResource<State>('/api/v1/session');
  const { t, locale } = useI18n();
  useEffect(() => {
    if (session.data && session.data.state !== 'owner') {
      redirectOwnerToLogin();
    }
  }, [session.data?.state, locale]);
  return (
    <OwnerWorkspace
      current={current}
      csrf={session.data?.state === 'owner' ? session.data.csrf : undefined}
    >
      <ResourceState resource={session} />
      {session.data?.state === 'owner'
        ? children(session.data.csrf!)
        : session.data && (
            <p>
              <a href={`/${locale}/login`}>{t('config.signIn')}</a>
            </p>
          )}
    </OwnerWorkspace>
  );
}
