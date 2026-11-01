import { OwnerFrame } from '../owner/OwnerFrame';
import { OwnerPageHeading } from '../owner/OwnerWorkspace';
import { useEffect, useState } from 'react';
import { useParams } from 'react-router-dom';
import { I18nProvider, useI18n } from '../../../../ui/src/i18n/I18nProvider';
import { call } from '../auth/api';
import type { Draft } from '../../api/content';
import { MarkdownReader } from './MarkdownReader';

export function PreviewPage() {
  const { key, contentLocale } = useParams();
  const { t } = useI18n();
  const [draft, setDraft] = useState<Draft>();
  const [error, setError] = useState(false);
  useEffect(() => {
    let active = true;
    setDraft(undefined);
    setError(false);
    call<Draft>(
      `/content/${encodeURIComponent(key ?? '')}/${encodeURIComponent(contentLocale ?? '')}`,
    ).then(
      (x) => {
        if (active) {
          setDraft(x);
        }
      },
      () => {
        if (active) {
          setError(true);
        }
      },
    );
    return () => {
      active = false;
    };
  }, [key, contentLocale]);
  return (
    <OwnerFrame current="posts">
      {() => (
        <div className="vb-reader vb-owner-preview">
          {error ? (
            <p role="alert">{t('reader.mediaUnavailable')}</p>
          ) : draft ? (
            <I18nProvider locale={draft.locale} syncDocument={false}>
              <div lang={draft.locale} dir={draft.locale === 'ar' ? 'rtl' : 'ltr'}>
                <OwnerPageHeading title={draft.revision.title} />
                <MarkdownReader
                  markdown={draft.revision.markdown}
                  media={draft.revision.media}
                  presentations={draft.revision.media_presentations}
                />
              </div>
            </I18nProvider>
          ) : (
            <p role="status">{t('auth.busy')}</p>
          )}
        </div>
      )}
    </OwnerFrame>
  );
}
