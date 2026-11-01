import { useEffect, useState } from 'react';
import { isOwnerRequest, redirectOwnerToLogin } from '../../features/auth/navigation';

export type { PageRoute, CollectionPage } from '../../api/content';

export interface Resource<T> {
  data?: T;
  loading: boolean;
  error?: number;
  retry: () => void;
}

// Both cancellation and identity checks matter: a completed older request must
// never paint its results under a newer URL, including browser Back navigation.
export function useResource<T>(
  url?: string,
  allowLocked = false,
  followProgress = false,
): Resource<T> {
  const [attempt, setAttempt] = useState(0);
  const [result, setResult] = useState<{ url?: string; data?: T; error?: number }>({});
  const [loading, setLoading] = useState(!!url);
  useEffect(() => {
    if (!followProgress) {
      return;
    }
    const refresh = () => setAttempt((n) => n + 1);
    window.addEventListener('learning-progress', refresh);
    return () => window.removeEventListener('learning-progress', refresh);
  }, [followProgress]);
  useEffect(() => {
    const controller = new AbortController();
    let active = true;
    setLoading(!!url);
    setResult((old) => (followProgress && old.url === url ? old : { url }));
    if (url) {
      fetch(url, { signal: controller.signal, cache: 'no-store', credentials: 'same-origin' })
        .then(async (response) => {
          if (active && response.status === 401 && isOwnerRequest(url.replace(/^\/api\/v1/, ''))) {
            redirectOwnerToLogin();
          }
          if (!response.ok && !(allowLocked && response.status === 403)) {
            throw response.status;
          }
          return response.json() as Promise<T>;
        })
        .then((data) => {
          if (active) {
            setResult({ url, data });
          }
        })
        .catch((error) => {
          if (active) {
            setResult({ url, error: typeof error === 'number' ? error : 503 });
          }
        })
        .finally(() => {
          if (active) {
            setLoading(false);
          }
        });
    }
    return () => {
      active = false;
      controller.abort();
    };
  }, [url, attempt, allowLocked, followProgress]);
  const current = result.url === url;
  return {
    data: current ? result.data : undefined,
    error: current ? result.error : undefined,
    loading: !!url && (!current || loading),
    retry: () => setAttempt((n) => n + 1),
  };
}
