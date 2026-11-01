import { authenticatedTransfer } from '../auth/reauthenticate';
import { useState } from 'react';
import { Button, useI18n } from '@ui';
import { OwnerPageHeading } from './OwnerWorkspace';
import { FileDropInput } from '../../../../ui/src/components/FileDropInput';
import { NotionImport } from './NotionImport';

export function Portability({ csrf }: { csrf: string }) {
  const { t, locale } = useI18n();
  const [file, setFile] = useState<File>();
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState('');
  async function transfer(importing: boolean) {
    if (busy || (importing && !file)) {
      return;
    }
    if (importing && !window.confirm(t('transfer.confirm'))) {
      return;
    }
    setBusy(true);
    setNotice('');
    try {
      const response = await authenticatedTransfer(
        '/api/v1/portability/' + (importing ? 'import' : 'export'),
        {
          method: 'POST',
          credentials: 'same-origin',
          cache: 'no-store',
          headers: { 'X-CSRF-Token': csrf, 'Content-Type': 'application/zip' },
          body: importing ? file : undefined,
        },
      );
      if (!response.ok) {
        setNotice(
          response.status === 401 || response.status === 403
            ? 'transfer.reauth'
            : response.status === 409
              ? 'transfer.empty'
              : 'transfer.failed',
        );
        return;
      }
      if (importing) {
        setNotice('transfer.imported');
      } else {
        const url = URL.createObjectURL(await response.blob());
        const a = document.createElement('a');
        a.href = url;
        a.download = 'blog-content.zip';
        a.click();
        window.setTimeout(() => URL.revokeObjectURL(url), 1000);
        setNotice('transfer.exported');
      }
    } catch (error) {
      setNotice(error instanceof DOMException ? 'transfer.reauth' : 'transfer.failed');
    } finally {
      setBusy(false);
    }
  }
  return (
    <>
      <OwnerPageHeading title={t('owner.transfer')} description={t('workspace.transferHelp')} />
      <NotionImport csrf={csrf} />
      <section className="owner-panel owner-form">
        <h2>{t('notion.blogArchive')}</h2>
        <p>{t('transfer.help')}</p>
        <p>
          <a href={`/${locale}/admin/security`}>{t('transfer.reauth')}</a>
        </p>
        <Button disabled={busy} onClick={() => transfer(false)}>
          {t('transfer.export')}
        </Button>
        <h2>{t('transfer.import')}</h2>
        <p>{t('transfer.empty')}</p>
        <FileDropInput
          label={t('transfer.file')}
          help={t('workspace.zipHelp')}
          accept=".zip,application/zip"
          disabled={busy}
          onFile={(selected) => {
            if (selected.size > 256 * 1024 * 1024) {
              setNotice('transfer.size');
              setFile(undefined);
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
        <Button disabled={busy || !file} onClick={() => transfer(true)}>
          {t('transfer.import')}
        </Button>
        {notice && (
          <p
            role="status"
            className="workspace-alert"
            data-tone={
              notice === 'transfer.imported' || notice === 'transfer.exported'
                ? 'success'
                : 'warning'
            }
          >
            {t(notice)}
          </p>
        )}
      </section>
    </>
  );
}
