import { authenticatedTransfer } from '../auth/reauthenticate';
import { useState } from 'react';
import { FileDropInput } from '../../../../ui/src/components/FileDropInput';
import { Button, Field } from '../../../../ui/src/components/primitives';
import { useI18n } from '../../../../ui/src/i18n/I18nProvider';
import type { Locale } from '../../../../ui/src/types';

type Preview = {
  pages: { key: string; title: string; kind: string; existing: boolean }[];
  issues: { path: string; code: string }[];
  media: number;
  ignored: number;
  skipped: number;
  collection_title: string;
};

export function NotionImport({ csrf }: { csrf: string }) {
  const { t, locale } = useI18n();
  const [file, setFile] = useState<File>();
  const [contentLocale, setContentLocale] = useState<Locale>(locale);
  const [kind, setKind] = useState('article');
  const [preview, setPreview] = useState<Preview>();
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState('');
  const [result, setResult] = useState<{ revisions: number; media: number; skipped: number }>();
  async function upload(importing: boolean) {
    if (!file || busy || (importing && (!preview || preview.issues.length))) {
      return;
    }
    setBusy(true);
    setNotice('');
    setResult(undefined);
    try {
      const query = new URLSearchParams({ locale: contentLocale, kind });
      const response = await authenticatedTransfer(
        `/api/v1/portability/notion/${importing ? 'import' : 'preview'}?${query}`,
        {
          method: 'POST',
          credentials: 'same-origin',
          cache: 'no-store',
          headers: { 'Content-Type': 'application/zip', 'X-CSRF-Token': csrf },
          body: file,
        },
      );
      if (!response.ok) {
        setNotice(
          response.status === 401 || response.status === 403
            ? 'transfer.reauth'
            : response.status === 409
              ? 'notion.conflict'
              : 'transfer.failed',
        );
        return;
      }
      const data = await response.json();
      if (importing) {
        setResult(data);
        setPreview(undefined);
      } else {
        setPreview(data);
      }
    } catch (error) {
      setNotice(error instanceof DOMException ? 'transfer.reauth' : 'transfer.failed');
    } finally {
      setBusy(false);
    }
  }
  function reset() {
    setPreview(undefined);
    setResult(undefined);
    setNotice('');
  }
  return (
    <section className="owner-form owner-panel" aria-busy={busy}>
      <h2>{t('notion.title')}</h2>
      <p>{t('notion.help')}</p>
      <FileDropInput
        label={t('notion.file')}
        help={t('workspace.zipHelp')}
        accept=".zip,application/zip"
        disabled={busy}
        onFile={(selected) => {
          reset();
          if (selected.size > 256 * 1024 * 1024) {
            setFile(undefined);
            setNotice('transfer.size');
          } else {
            setFile(selected);
          }
        }}
      />
      {file && (
        <p className="vb-meta" dir="auto">
          {file.name}
        </p>
      )}
      <div className="notion-options">
        <Field label={t('notion.locale')}>
          <select
            value={contentLocale}
            disabled={busy}
            onChange={(event) => {
              reset();
              setContentLocale(event.target.value as Locale);
            }}
          >
            {(['en', 'ar', 'fr'] as const).map((language) => (
              <option key={language} value={language}>
                {t(`language.${language}`)}
              </option>
            ))}
          </select>
        </Field>
        <Field label={t('notion.kind')}>
          <select
            value={kind}
            disabled={busy}
            onChange={(event) => {
              reset();
              setKind(event.target.value);
            }}
          >
            <option value="article">{t('notion.articles')}</option>
            <option value="learning">{t('notion.learning')}</option>
          </select>
        </Field>
      </div>
      <Button disabled={!file || busy} onClick={() => upload(false)}>
        {t('notion.preview')}
      </Button>
      {preview && (
        <div>
          <p>
            {t('notion.summary', {
              pages: preview.pages.length,
              media: preview.media,
              skipped: preview.skipped,
            })}
          </p>
          {preview.collection_title && (
            <p>{t('notion.collection', { title: preview.collection_title })}</p>
          )}
          <ol>
            {preview.pages.map((page) => (
              <li key={page.key}>
                <span dir="auto">{page.title}</span>
                {' · '}
                {t(
                  page.existing
                    ? 'notion.skipped'
                    : page.kind === 'lesson'
                      ? 'notion.lesson'
                      : 'notion.article',
                )}
              </li>
            ))}
          </ol>
          {preview.ignored > 0 && <p>{t('notion.ignored', { count: preview.ignored })}</p>}
          {preview.issues.length > 0 && (
            <div role="alert">
              <p>{t('notion.unsupported')}</p>
              <ul>
                {preview.issues.map((issue, index) => (
                  <li key={index} dir="auto">
                    {issue.path}
                  </li>
                ))}
              </ul>
            </div>
          )}
          <Button
            variant="primary"
            disabled={busy || preview.issues.length > 0}
            onClick={() => upload(true)}
          >
            {t('notion.import')}
          </Button>
        </div>
      )}
      {busy && <p role="status">{t('notion.busy')}</p>}
      {notice === 'transfer.reauth' && (
        <a href={`/${locale}/admin/security`} target="_blank" rel="noopener">
          {t('auth.passwordOption')}
        </a>
      )}
      {notice && (
        <p
          role="status"
          className="workspace-alert"
          data-tone={notice === 'transfer.failed' ? 'error' : 'warning'}
        >
          {t(notice)}
        </p>
      )}
      {result && (
        <div role="status" className="workspace-alert" data-tone="success">
          <p>
            {t('notion.imported', {
              pages: result.revisions,
              media: result.media,
              skipped: result.skipped,
            })}
          </p>
          <a href={`/${locale}/admin/posts`}>{t('notion.review')}</a>
        </div>
      )}
    </section>
  );
}
