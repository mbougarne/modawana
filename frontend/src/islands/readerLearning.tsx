import { useEffect } from 'react';
import { createRoot } from 'react-dom/client';
import { I18nProvider, type LabelOverrides, type Locale } from '@ui';
import type { ContentKind } from '../api/content';
import type { ReaderLearningState } from '../api/learning';
import { BrowserProgress } from '../features/learning/BrowserProgress';
import { ReaderLearning } from '../features/learning/ReaderLearning';
import { useResource } from '../pages/public/api';
import { ResourceState } from '../pages/public/ResourceState';
import { observeReadingProgress } from './readingNavigation';

const locale = document.documentElement.lang;
if (locale === 'en' || locale === 'ar' || locale === 'fr') {
  mountLearning(locale);
}

function labelOverrides(): LabelOverrides | undefined {
  const data = document.getElementById('reader-labels')?.textContent;
  if (data) {
    try {
      return JSON.parse(data);
    } catch {
      return undefined;
    }
  }
  return undefined;
}

function mountLearning(locale: Locale) {
  const overrides = labelOverrides();
  for (const element of document.querySelectorAll<HTMLElement>('[data-reader-learning]')) {
    const { key, kind } = element.dataset;
    if (key && element.dataset.locale === locale && (kind === 'lesson' || kind === 'article')) {
      createRoot(element).render(
        <I18nProvider locale={locale} overrides={overrides} syncDocument={false}>
          <LearningControls contentKey={key} locale={locale} kind={kind} />
        </I18nProvider>,
      );
    }
  }
  for (const element of document.querySelectorAll<HTMLElement>('[data-browser-progress]')) {
    createRoot(element).render(
      <I18nProvider locale={locale} overrides={overrides} syncDocument={false}>
        <BrowserProgress />
      </I18nProvider>,
    );
  }
  observeReadingProgress();
}

function LearningControls({
  contentKey,
  kind,
  locale,
}: {
  contentKey: string;
  kind: ContentKind;
  locale: Locale;
}) {
  const state = useResource<ReaderLearningState>(
    `/api/v1/learning/content/${encodeURIComponent(contentKey)}/${locale}/quiz`,
    false,
    true,
  );
  useEffect(() => {
    if (state.error === 403 || state.error === 404) {
      // Reapply the server's current publication or access decision.
      window.location.reload();
    }
  }, [state.error]);
  if (state.error || !state.data) {
    return <ResourceState resource={state} />;
  }
  return (
    <ReaderLearning
      content={{
        key: contentKey,
        kind,
        locale,
        locked: false,
        learning: state.data,
      }}
    />
  );
}
