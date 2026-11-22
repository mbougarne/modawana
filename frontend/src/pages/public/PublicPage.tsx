import { useEffect, useState } from 'react';
import { useLocation } from 'react-router-dom';
import { useI18n } from '@ui';
import { PublicLayout } from './PublicLayout';
import { useResource, type PageRoute } from './api';
import { HomePage } from './home';
import { WritingPage } from './writing';
import { CollectionIndex, CollectionView, ReaderPage } from './reading';
import './public.css';
import { ResourceState } from './ResourceState';
import { updateSocialMetadata } from './social';

function decodedPath(path: string) {
  try {
    return decodeURI(path);
  } catch {
    return path;
  }
}

export function PublicPage({ invalid = false }: { invalid?: boolean }) {
  const location = useLocation();
  const [canonicalOrigin] = useState(() => {
    const initial = document.querySelector<HTMLLinkElement>('link[rel="canonical"]')?.href;
    return initial ? new URL(initial).origin : window.location.origin;
  });
  const { t } = useI18n();
  const resource = useResource<PageRoute>(
    invalid
      ? undefined
      : '/api/v1/public/page?path=' + encodeURIComponent(decodedPath(location.pathname)),
    true,
    true,
  );
  const page = resource.data;
  useEffect(() => {
    const title =
      page?.metadata?.title ||
      page?.title ||
      t(
        resource.error === 404 || invalid
          ? 'foundation.notFound'
          : 'page.' + (page?.kind || 'home'),
      );
    const summary = page?.metadata?.description ?? page?.summary ?? '';
    document.title = title;
    const description = document.querySelector<HTMLMetaElement>('meta[name="description"]');
    if (description) {
      description.content = summary;
    }
    document.head
      .querySelectorAll(
        'link[rel="canonical"],link[rel="alternate"][hreflang],meta[property="og:url"],meta[name="robots"]',
      )
      .forEach((n) => n.remove());
    const shareable = page && !page.content?.locked && !resource.error && !invalid;
    updateSocialMetadata(shareable ? page.metadata?.social : undefined, title, summary);
    if (shareable) {
      const canonical = document.createElement('link');
      canonical.rel = 'canonical';
      canonical.href = new URL(page.url, canonicalOrigin).href;
      const og = document.createElement('meta');
      og.setAttribute('property', 'og:url');
      og.content = canonical.href;
      document.head.append(og);
      document.head.append(canonical);
      for (const a of page.alternatives) {
        const l = document.createElement('link');
        l.rel = 'alternate';
        l.hreflang = a.locale;
        l.href = new URL(a.url, canonicalOrigin).href;
        document.head.append(l);
      }
    } else {
      const meta = document.createElement('meta');
      meta.name = 'robots';
      meta.content = 'noindex';
      document.head.append(meta);
    }
    for (const [property, value] of [
      ['og:title', title],
      ['og:description', summary],
    ]) {
      const meta = document.querySelector<HTMLMetaElement>('meta[property="' + property + '"]');
      if (meta) {
        meta.content = value;
      }
    }
    return () => updateSocialMetadata(undefined, '', '');
  }, [page, resource.error, invalid, t, canonicalOrigin]);
  return (
    <PublicLayout page={page}>
      {invalid ? <h1>{t('foundation.notFound')}</h1> : <ResourceState resource={resource} />}
      {page &&
        (page.content ? (
          <ReaderPage page={page} />
        ) : page.collection ? (
          <CollectionView collection={page.collection} />
        ) : page.kind === 'home' || page.kind === 'about' ? (
          <HomePage about={page.kind === 'about'} />
        ) : page.kind === 'writing' || page.kind === 'search' ? (
          <WritingPage />
        ) : (
          <CollectionIndex kind={page.kind === 'learn' ? 'learning' : 'docs'} />
        ))}
    </PublicLayout>
  );
}
