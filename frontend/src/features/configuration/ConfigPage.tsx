import { FieldIssues, ValidationIssues } from './ValidationIssues';
import { redirectOwnerToLogin } from '../auth/navigation';
import { type FieldIssue, call, AuthError, requestErrorKey, type State } from '../auth/api';
import { OwnerWorkspace, OwnerPageHeading } from '../owner/OwnerWorkspace';
import { useEffect, useState } from 'react';
import { Button, Label, useI18n, type LabelOverrides } from '@ui';
import type {
  ConfigKind,
  ConfigDocument,
  ConfigPreview,
  Snapshot,
  SiteSettings,
  ProfileSettings,
} from '../../api/configuration';
import { SettingsForm } from '../../pages/admin/settings/SettingsForm';
import { ProfileForm } from '../../pages/admin/profile/ProfileForm';
import { LabelsForm } from '../../pages/admin/labels/LabelsForm';
import { usePublicConfiguration } from './PublicConfiguration';
import './configuration.css';
import { UploadActivity } from './controls';

export function ConfigPage({ kind }: { kind: ConfigKind }) {
  const { t, locale, date } = useI18n();
  const { refresh } = usePublicConfiguration();
  const [base, setBase] = useState<ConfigDocument>();
  const [working, setWorking] = useState<Snapshot>();
  const [csrf, setCSRF] = useState('');
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState('');
  const [issues, setIssues] = useState<FieldIssue[]>([]);
  const [preview, setPreview] = useState<ConfigPreview>();
  const [confirmed, setConfirmed] = useState(false);
  const [history, setHistory] = useState<{ revision_id: string; created_at: string }[]>([]);
  const [nextHistory, setNextHistory] = useState('');
  const [generation, setGeneration] = useState(0);
  const [uploads, setUploads] = useState(0);
  function error(e: unknown) {
    setIssues(e instanceof AuthError ? e.fields : []);
    setNotice(
      e instanceof AuthError && e.code === 'revision_conflict'
        ? 'config.conflict'
        : e instanceof AuthError && e.code === 'unauthenticated'
          ? 'config.signIn'
          : requestErrorKey(e),
    );
  }
  async function load() {
    setGeneration((n) => n + 1);
    setBusy(true);
    try {
      const session = await call<State>('/session');
      if (session.state !== 'owner') {
        redirectOwnerToLogin();
        throw new AuthError('unauthenticated');
      }
      const d = await call<ConfigDocument>('/' + kind);
      setCSRF(session.csrf!);
      setBase(d);
      setWorking(structuredClone(d.snapshot));
      setPreview(undefined);
      setConfirmed(false);
      setNotice('');
      setIssues([]);
      setHistory([]);
    } catch (e) {
      error(e);
    } finally {
      setBusy(false);
    }
  }
  useEffect(() => {
    void load();
  }, [kind]);
  const dirty = !!base && JSON.stringify(working) !== JSON.stringify(base.snapshot);
  useEffect(() => {
    const leave = (e: BeforeUnloadEvent) => {
      if (dirty) {
        e.preventDefault();
        e.returnValue = '';
      }
    };
    window.addEventListener('beforeunload', leave);
    return () => window.removeEventListener('beforeunload', leave);
  }, [dirty]);
  function edit(v: Snapshot) {
    setIssues([]);
    setWorking(v);
    setPreview(undefined);
    setConfirmed(false);
    setNotice('config.unsaved');
  }
  async function validate(snapshot: unknown = working) {
    if (!base) {
      return;
    }
    const out = await call<ConfigPreview>(
      '/' + kind + '/preview',
      'POST',
      { expected_version: base.version, snapshot },
      csrf,
    );
    setIssues([]);
    setPreview(out);
    setConfirmed(false);
    setNotice('config.previewReady');
    return out;
  }
  async function run(fn: () => Promise<void>) {
    if (busy) {
      return;
    }
    setBusy(true);
    try {
      await fn();
    } catch (e) {
      error(e);
    } finally {
      setBusy(false);
    }
  }
  async function save() {
    if (!base) {
      return;
    }
    const out = preview ?? (await validate());
    if (!out) {
      return;
    }
    if (out.required_reviews.length && !confirmed) {
      return;
    }
    const d = await call<ConfigDocument>(
      '/' + kind,
      'PUT',
      {
        expected_version: base.version,
        snapshot: out.snapshot,
        reviews: (confirmed ? [...out.required_reviews, ...out.stale_reviews] : []).map(
          ({ value, ...r }) => r,
        ),
      },
      csrf,
    );
    setBase(d);
    setWorking(structuredClone(d.snapshot));
    setPreview(undefined);
    setConfirmed(false);
    setHistory([]);
    setNotice('config.saved');
    try {
      await refresh();
    } catch {
      setNotice('config.savedRefreshFailed');
    }
  }
  async function showHistory(cursor = '') {
    const h = await call<{ items: typeof history; next_cursor: string }>(
      '/' + kind + '/history?cursor=' + cursor,
    );
    setHistory((old) => (cursor ? [...old, ...h.items] : h.items));
    setNextHistory(h.next_cursor);
  }
  return (
    <OwnerWorkspace
      current={kind}
      csrf={csrf}
      busy={busy}
      mainId="configuration"
      className={`config-page${kind === 'labels' ? ' labels-page' : ''}`}
      languagePath={'/admin/' + kind}
    >
      <OwnerPageHeading
        title={t('config.' + kind)}
        description={t(kind === 'labels' ? 'labels.description' : 'workspace.' + kind + 'Help')}
      />
      {notice && !issues.length && (
        <p
          role="status"
          className="config-notice workspace-alert"
          data-tone={
            notice === 'config.saved'
              ? 'success'
              : ['config.unsaved', 'config.savedRefreshFailed'].includes(notice)
                ? 'warning'
                : notice.startsWith('request.') ||
                    ['config.invalid', 'config.conflict', 'config.signIn'].includes(notice)
                  ? 'error'
                  : 'info'
          }
        >
          <Label name={notice} />
          {notice === 'config.conflict' && (
            <Button onClick={() => run(load)}>
              <Label name="config.reload" />
            </Button>
          )}
          {notice === 'config.signIn' && (
            <a href={'/' + locale + '/login'}>
              <Label name="auth.title" />
            </a>
          )}
        </p>
      )}
      <ValidationIssues issues={issues} />
      {base && working && (
        <>
          <FieldIssues.Provider value={issues}>
            <UploadActivity.Provider value={(delta) => setUploads((n) => Math.max(0, n + delta))}>
              <fieldset key={generation} className="config-fields" disabled={busy}>
                {kind === 'settings' ? (
                  <SettingsForm value={working as SiteSettings} onChange={edit} csrf={csrf} />
                ) : kind === 'profile' ? (
                  <ProfileForm value={working as ProfileSettings} onChange={edit} csrf={csrf} />
                ) : (
                  <LabelsForm
                    value={working as LabelOverrides}
                    saved={base.snapshot as LabelOverrides}
                    onChange={edit}
                    onImport={(v) =>
                      run(async () => {
                        const out = await validate(v);
                        if (out) {
                          setWorking(out.snapshot);
                          setPreview(out);
                        }
                      })
                    }
                  />
                )}
              </fieldset>
            </UploadActivity.Provider>
          </FieldIssues.Provider>
          <section className="config-panel config-history-panel">
            <h2>
              <Label name="config.history" />
            </h2>
            <Button
              disabled={busy || uploads > 0}
              onClick={() =>
                run(async () => {
                  const d = await call<ConfigDocument>('/' + kind + '/defaults');
                  edit(d.snapshot);
                })
              }
            >
              <Label name="config.resetAll" />
            </Button>
            <Button onClick={() => run(() => showHistory())}>
              <Label name="config.loadHistory" />
            </Button>
            {history.map((h) => (
              <div className="config-history" key={h.revision_id}>
                <time dateTime={h.created_at}>
                  {date(h.created_at, {
                    year: 'numeric',
                    month: 'short',
                    day: 'numeric',
                    hour: 'numeric',
                    minute: 'numeric',
                    second: 'numeric',
                  })}
                </time>
                <Button
                  onClick={() =>
                    run(async () => {
                      const d = await call<ConfigDocument>(
                        '/' + kind + '/restore',
                        'POST',
                        { expected_version: base.version, revision_id: h.revision_id },
                        csrf,
                      );
                      edit(d.snapshot);
                    })
                  }
                >
                  <Label name="config.restore" />
                </Button>
              </div>
            ))}
            {nextHistory && (
              <Button onClick={() => run(() => showHistory(nextHistory))}>
                <Label name="config.more" />
              </Button>
            )}
          </section>
          {preview && (
            <section className="config-panel" aria-label={t('config.review')}>
              <h2>
                <Label name="config.review" />
              </h2>
              {preview.required_reviews.length || preview.stale_reviews.length ? (
                <>
                  <p>
                    <Label name="config.reviewHelp" />
                  </p>
                  {preview.stale_reviews.length > 0 && (
                    <p>
                      <Label name="config.staleHelp" />
                    </p>
                  )}
                  <ul>
                    {[...preview.required_reviews, ...preview.stale_reviews].map((r) => (
                      <li key={r.path + r.locale}>
                        <bdi>{r.path}</bdi> · {t('language.' + r.locale)}
                        <p lang={r.locale} dir={r.locale === 'ar' ? 'rtl' : 'ltr'}>
                          {typeof r.value === 'string' ? r.value : JSON.stringify(r.value)}
                        </p>
                      </li>
                    ))}
                  </ul>
                  <label className="config-check">
                    <input
                      type="checkbox"
                      checked={confirmed}
                      onChange={(e) => setConfirmed(e.target.checked)}
                    />
                    <Label name="config.confirmReview" />
                  </label>
                </>
              ) : (
                <p>
                  <Label name="config.noReview" />
                </p>
              )}
            </section>
          )}
          <footer className="config-save">
            <span>
              <Label name={dirty ? 'config.unsaved' : 'config.current'} />
            </span>
            <Button
              disabled={busy || uploads > 0}
              onClick={() =>
                run(async () => {
                  await validate();
                })
              }
            >
              <Label name="config.preview" />
            </Button>
            <Button
              variant="primary"
              disabled={busy || uploads > 0 || (!!preview?.required_reviews.length && !confirmed)}
              onClick={() => run(save)}
            >
              <Label name="config.save" />
            </Button>
            <Button
              disabled={busy}
              onClick={() => {
                setGeneration((n) => n + 1);
                setWorking(structuredClone(base.snapshot));
                setPreview(undefined);
                setConfirmed(false);
                setNotice('config.cancelled');
                setIssues([]);
              }}
            >
              <Label name="config.cancel" />
            </Button>
          </footer>
        </>
      )}
      {!base && !notice && (
        <p role="status">
          <Label name="auth.busy" />
        </p>
      )}
    </OwnerWorkspace>
  );
}
