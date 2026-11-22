import { useCallback, useEffect, useRef } from 'react';
import { useBeforeUnload, useBlocker, type BlockerFunction } from 'react-router-dom';
import { useI18n } from '../../../../ui/src/i18n/I18nProvider';

export function useOwnerLeaveGuard(dirty: boolean) {
  const { t } = useI18n();
  const replacingSavedRoute = useRef(false);
  const blocker = useBlocker(
    useCallback<BlockerFunction>(
      ({ currentLocation, nextLocation }) =>
        dirty &&
        !replacingSavedRoute.current &&
        (currentLocation.pathname !== nextLocation.pathname ||
          currentLocation.search !== nextLocation.search),
      [dirty],
    ),
  );

  useEffect(() => {
    if (blocker.state === 'blocked') {
      if (window.confirm(t('owner.confirmLeave'))) {
        blocker.proceed();
      } else {
        blocker.reset();
      }
    }
  }, [blocker, t]);

  useBeforeUnload(
    useCallback(
      (event) => {
        if (dirty) {
          event.preventDefault();
          event.returnValue = '';
        }
      },
      [dirty],
    ),
  );

  // Saving a new draft replaces its creation URL before React renders the clean editor.
  return (replace: () => void) => {
    replacingSavedRoute.current = true;
    try {
      replace();
    } finally {
      replacingSavedRoute.current = false;
    }
  };
}
