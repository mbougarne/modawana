import { useEffect, useRef, useState } from 'react';
import { FileDropInput } from '../../../../ui/src/components/FileDropInput';
import { useI18n } from '@ui';
import type { MediaRecord } from '../../api/contracts';
import { call, AuthError } from '../auth/api';
import { redirectOwnerToLogin } from '../auth/navigation';
import { useResource } from '../../pages/public/api';
import { ResourceState } from '../../pages/public/ResourceState';

export interface UsedMedia extends MediaRecord {
  retained_references?: number;
  public_reuse?: boolean;
}

export function MediaLibrary({
  csrf,
  onSelect,
  pendingFile,
  onPendingFileHandled,
}: {
  csrf: string;
  onSelect?: (m: UsedMedia) => void;
  pendingFile?: File;
  onPendingFileHandled?: () => void;
}) {
  const { t } = useI18n();
  const [kind, setKind] = useState('');
  const [cursor, setCursor] = useState('');
  const [notice, setNotice] = useState('');
  const [busy, setBusy] = useState(false);
  const [percent, setPercent] = useState(0);
  const [file, setFile] = useState<File>();
  const upload = useRef<XMLHttpRequest | null>(null);
  const alive = useRef(true);
  const list = useResource<{ items: UsedMedia[]; next_cursor: string }>(
    `/api/v1/media?kind=${kind}&cursor=${cursor}`,
  );
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
      upload.current?.abort();
    };
  }, []);
  useEffect(() => {
    if (pendingFile) {
      void send(pendingFile);
      onPendingFileHandled?.();
    }
  }, [pendingFile]);
  async function send(selected: File) {
    if (upload.current) {
      setNotice('owner.uploadBusy');
      return;
    }
    setFile(selected);
    setBusy(true);
    setPercent(0);
    setNotice('');
    const xhr = new XMLHttpRequest();
    upload.current = xhr;
    const body = new FormData();
    body.append('file', selected);
    xhr.open('POST', '/api/v1/media');
    xhr.setRequestHeader('X-CSRF-Token', csrf);
    xhr.upload.onprogress = (e) => {
      if (alive.current && e.lengthComputable) {
        setPercent(Math.floor((e.loaded / e.total) * 100));
      }
    };
    xhr.onload = () => {
      if (!alive.current) {
        return;
      }
      if (xhr.status >= 200 && xhr.status < 300) {
        try {
          const m = JSON.parse(xhr.responseText) as UsedMedia;
          list.retry();
          setNotice('owner.uploaded');
          setFile(undefined);
          onSelect?.(m);
        } catch {
          setNotice('owner.uploadFailed');
        }
      } else {
        if (xhr.status === 401) {
          redirectOwnerToLogin();
        }
        setNotice('owner.uploadFailed');
      }
    };
    xhr.onerror = () => {
      if (alive.current) {
        setNotice('owner.uploadFailed');
      }
    };
    xhr.onabort = () => {
      if (alive.current) {
        setNotice('owner.cancelled');
      }
    };
    xhr.onloadend = () => {
      upload.current = null;
      if (alive.current) {
        setBusy(false);
      }
    };
    xhr.send(body);
  }
  async function remove(m: UsedMedia) {
    if (!window.confirm(t('owner.confirmDelete'))) {
      return;
    }
    try {
      await call('/media/' + m.id, 'DELETE', undefined, csrf);
      list.retry();
      setNotice('owner.deleted');
    } catch (e) {
      setNotice(
        e instanceof AuthError && e.code === 'revision_conflict'
          ? 'owner.referenced'
          : 'owner.failed',
      );
    }
  }
  return (
    <section
      className="owner-panel media-library"
      onDragOver={(e) => e.preventDefault()}
      onDrop={(e) => {
        e.preventDefault();
        e.stopPropagation();
        const selected = e.dataTransfer.files[0];
        if (selected) {
          void send(selected);
        }
      }}
      onPaste={(e) => {
        const selected = e.clipboardData.files[0];
        if (selected) {
          e.preventDefault();
          e.stopPropagation();
          void send(selected);
        }
      }}
    >
      <FileDropInput
        label={t('owner.upload')}
        title={t('upload.mediaTitle')}
        help={t('upload.mediaHelp')}
        choices={[
          {
            label: t('upload.chooseImage'),
            accept: 'image/jpeg,image/png,image/webp',
            icon: 'image',
          },
          { label: t('upload.chooseVideo'), accept: 'video/mp4,video/webm', icon: 'video' },
          {
            label: t('upload.chooseAttachment'),
            accept: 'text/vtt,application/pdf',
            icon: 'upload',
            quiet: true,
          },
        ]}
        accept="image/jpeg,image/png,image/webp,video/mp4,video/webm,text/vtt,application/pdf"
        onFile={(selected) => void send(selected)}
      />
      {busy && (
        <div className="owner-row">
          <progress value={percent} max={100} aria-label={t('owner.upload')} />
          <button className="vb-btn vb-btn-secondary" onClick={() => upload.current?.abort()}>
            {t('config.cancel')}
          </button>
        </div>
      )}
      {notice && (
        <p
          role="status"
          className="workspace-alert"
          data-tone={
            notice === 'owner.uploadFailed' ||
            notice === 'owner.failed' ||
            notice === 'owner.referenced'
              ? 'error'
              : notice === 'owner.cancelled' || notice === 'owner.uploadBusy'
                ? 'info'
                : 'success'
          }
        >
          {t(notice)}
        </p>
      )}
      {file && !busy && (
        <button className="vb-btn vb-btn-secondary" onClick={() => void send(file)}>
          {t('public.retry')}
        </button>
      )}
      <label className="media-filter">
        {t('owner.mediaKind')}
        <select
          value={kind}
          onChange={(e) => {
            setKind(e.target.value);
            setCursor('');
          }}
        >
          <option value="">{t('owner.all')}</option>
          {['image', 'video', 'pdf', 'captions'].map((k) => (
            <option key={k} value={k}>
              {t('owner.' + k)}
            </option>
          ))}
        </select>
      </label>
      <ResourceState resource={list} />
      {list.data?.items.length === 0 && <p className="vb-empty">{t('owner.empty')}</p>}
      <div className="owner-media-grid">
        {list.data?.items.map((m) => (
          <article key={m.id}>
            {m.kind === 'image' && <img src={m.url} alt="" loading="lazy" />}
            {m.kind === 'video' && <video src={m.url} controls preload="metadata" />}
            <h3>{m.filename}</h3>
            <p>{t('owner.bytes', { n: m.byte_size })}</p>
            <p>{t('owner.references', { n: m.retained_references ?? 0 })}</p>
            {m.public_reuse && <p className="owner-warning">{t('owner.publicReuse')}</p>}
            <div className="owner-row">
              {onSelect && (
                <button className="vb-btn vb-btn-secondary" onClick={() => onSelect(m)}>
                  {t('owner.select')}
                </button>
              )}
              <a href={m.url} target="_blank" rel="noreferrer">
                {t('owner.openFile')}
              </a>
              <button className="vb-btn vb-btn-quiet" onClick={() => void remove(m)}>
                {t('config.remove')}
              </button>
            </div>
          </article>
        ))}
      </div>
      {list.data?.next_cursor && (
        <button
          className="vb-btn vb-btn-secondary"
          onClick={() => setCursor(list.data!.next_cursor)}
        >
          {t('public.more')}
        </button>
      )}
    </section>
  );
}
