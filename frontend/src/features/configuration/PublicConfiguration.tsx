import { createContext, useContext, useEffect, useState, type ReactNode } from 'react';
import { BrandStyle, DEFAULT_PRIMARY, DEFAULT_SECONDARY } from '@ui';
import { call } from '../auth/api';
import type { PublicConfiguration } from '../../api/configuration';

const Context = createContext<{ data?: PublicConfiguration; refresh: () => Promise<void> }>({
  refresh: async () => {},
});

export const usePublicConfiguration = () => useContext(Context);
function initial(): PublicConfiguration | undefined {
  try {
    return (
      JSON.parse(document.getElementById('public-configuration')?.textContent ?? 'null') ??
      undefined
    );
  } catch {
    return undefined;
  }
}

export function PublicConfigurationProvider({ children }: { children: ReactNode }) {
  const [data, setData] = useState(initial);
  async function refresh() {
    const l = location.pathname.split('/')[1];
    setData(
      await call<PublicConfiguration>(
        '/public/site?locale=' + (['en', 'ar', 'fr'].includes(l) ? l : 'en'),
      ),
    );
  }
  useEffect(() => {
    if (!data) {
      refresh().catch(() => {});
    }
  }, []);
  useEffect(() => {
    if (!data) {
      return;
    }
    document.getElementById('initial-brand')?.remove();
    let icon = document.querySelector<HTMLLinkElement>('link[rel="icon"]');
    if (data.site.favicon_media_id) {
      if (!icon) {
        icon = document.createElement('link');
        icon.rel = 'icon';
        document.head.append(icon);
      }
      icon.href = '/media/' + data.site.favicon_media_id;
    } else {
      icon?.remove();
    }
  }, [data]);
  return (
    <Context.Provider value={{ data, refresh }}>
      <BrandStyle
        primary={data?.site.primary ?? DEFAULT_PRIMARY}
        secondary={data?.site.secondary ?? DEFAULT_SECONDARY}
        nonce={document.querySelector<HTMLMetaElement>('meta[name="csp-nonce"]')?.content}
      />
      {children}
    </Context.Provider>
  );
}
