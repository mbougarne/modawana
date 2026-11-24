import { useState } from 'react';
import { useLocation } from 'react-router-dom';
import { Button, Field, useI18n, type Locale } from '@ui';
import { isLocale, localeCodes } from '../../../../ui/src/i18n/locales';
import { useResource } from '../../pages/public/api';
import { call } from '../auth/api';
import { authenticatedTransfer } from '../auth/reauthenticate';
import { OwnerPageHeading } from './OwnerWorkspace';

const api = '/api/v1/owner/social';

interface Connection {
  id: string;
  instance: string;
  account: string;
  url: string;
}

interface Candidate {
  key: string;
  revision_id: string;
  title: string;
}

interface ScheduledPost {
  id: string;
  destination: string;
  body: string;
  locale: Locale;
  status: string;
  reason?: string;
  scheduled_at: string;
  updated_at: string;
  attempts: number;
  remote_id?: string;
  remote_url?: string;
  can_cancel: boolean;
}

interface Preview {
  body: string;
  path: string;
  destination: string;
}

interface Draft {
  connection_id: string;
  key: string;
  locale: Locale;
  revision_id: string;
  text: string;
}

type ReviewedPost = Preview & Draft & { id: string; scheduled_at?: string };

const failureReasons = new Set([
  'access_revoked',
  'connection_changed',
  'credentials_unavailable',
  'destination_changed',
  'publication_changed',
  'provider_rejected',
  'provider_retry',
  'provider_unavailable',
  'replay_window_expired',
  'unexpected_receipt',
]);

export function SocialPublishing({ csrf }: { csrf: string }) {
  const { t, locale, date } = useI18n();
  const location = useLocation();
  const [cursor, setCursor] = useState('');
  const account = useResource<{ instance: string; connection?: Connection }>(api + '/connection');
  const history = useResource<{ items: ScheduledPost[]; next?: string }>(
    api + (cursor ? '?cursor=' + encodeURIComponent(cursor) : ''),
  );
  const [activeCSRF, setCSRF] = useState('');
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState('');
  const oauth = new URLSearchParams(location.search).get('oauth');
  const connection = account.data?.connection;

  function refresh() {
    setCursor('');
    history.retry();
  }

  async function mutate(action: () => Promise<void>, fallback = 'social.failedRequest') {
    setBusy(true);
    setNotice('');
    try {
      await action();
    } catch {
      setNotice(fallback);
    } finally {
      setBusy(false);
    }
  }

  async function accountAction(action: 'connect' | 'disconnect') {
    await mutate(async () => {
      const response = await authenticatedTransfer(
        api + '/' + action,
        {
          method: 'POST',
          credentials: 'same-origin',
          cache: 'no-store',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ locale }),
        },
        (session) => setCSRF(session.csrf || ''),
      );
      if (!response.ok) {
        throw new Error('Connection request failed');
      }
      if (action === 'connect') {
        const result = (await response.json()) as { url: string };
        window.location.assign(result.url);
      } else {
        account.retry();
        refresh();
        setNotice('social.disconnected');
      }
    }, 'social.connectionFailed');
  }

  return (
    <>
      <OwnerPageHeading title={t('owner.social')} description={t('social.help')} />
      {account.loading && !cursor && <p role="status">{t('social.loading')}</p>}
      {(account.error || history.error) && <p role="alert">{t('social.failedRequest')}</p>}
      {account.data && (
        <section className="vb-card">
          <h2>{t('social.destination')}</h2>
          {!account.data.instance ? (
            <p>{t('social.unconfigured')}</p>
          ) : (
            <>
              <p>
                <bdi>
                  {connection
                    ? connection.account + '@' + connection.instance.slice(8)
                    : account.data.instance}
                </bdi>
              </p>
              {connection ? (
                <Button
                  disabled={busy}
                  variant="quiet"
                  onClick={() => void accountAction('disconnect')}
                >
                  {t('social.disconnect')}
                </Button>
              ) : (
                <Button disabled={busy} onClick={() => void accountAction('connect')}>
                  {t('social.connect')}
                </Button>
              )}
            </>
          )}
        </section>
      )}
      {(notice || oauth === 'connected' || oauth === 'failed') && (
        <p role="status">
          {t(notice || (oauth === 'connected' ? 'social.connected' : 'social.connectionFailed'))}
        </p>
      )}
      {connection && (
        <Composer
          key={connection.id}
          connection={connection}
          csrf={activeCSRF || csrf}
          refresh={refresh}
        />
      )}
      <section className="vb-card">
        <h2>{t('social.history')}</h2>
        <Button onClick={refresh}>{t('social.refresh')}</Button>
        {history.data?.items.length === 0 && <p>{t('social.empty')}</p>}
        {history.data?.items.map((post) => (
          <article className="vb-card" key={post.id}>
            <h3>{t('social.status.' + post.status)}</h3>
            <p>
              <bdi>{post.destination}</bdi> ·{' '}
              <time dateTime={post.scheduled_at}>
                {date(post.scheduled_at, { dateStyle: 'medium', timeStyle: 'short' })}
              </time>
            </p>
            <p className="social-post-body" lang={post.locale} dir="auto">
              {post.body}
            </p>
            {post.reason && (
              <p>
                {t(
                  failureReasons.has(post.reason)
                    ? 'social.reason.' + post.reason
                    : 'social.failedRequest',
                )}
              </p>
            )}
            {post.status === 'uncertain' && <p>{t('social.uncertainHelp')}</p>}
            {post.remote_url ? (
              <a href={post.remote_url} target="_blank" rel="noreferrer">
                {t('social.viewPost')}
              </a>
            ) : post.remote_id ? (
              <p>
                <bdi>{post.remote_id}</bdi>
              </p>
            ) : null}
            {post.can_cancel && (
              <Button
                disabled={busy}
                variant="quiet"
                onClick={() =>
                  void mutate(async () => {
                    await call('/owner/social/cancel/' + post.id, 'POST', {}, activeCSRF || csrf);
                    refresh();
                  })
                }
              >
                {t('social.cancel')}
              </Button>
            )}
          </article>
        ))}
        {cursor && <Button onClick={() => setCursor('')}>{t('social.newer')}</Button>}
        {history.data?.next && (
          <Button onClick={() => setCursor(history.data?.next || '')}>{t('social.older')}</Button>
        )}
      </section>
    </>
  );
}

function Composer({
  connection,
  csrf,
  refresh,
}: {
  connection: Connection;
  csrf: string;
  refresh: () => void;
}) {
  const { t, locale, date } = useI18n();
  const [language, setLanguage] = useState(locale);
  const [cursor, setCursor] = useState('');
  const candidates = useResource<{ items: Candidate[]; next?: string }>(
    api + '/content?' + new URLSearchParams({ locale: language, cursor }),
  );
  const [selected, select] = useState('');
  const [text, setText] = useState('');
  const [when, setWhen] = useState('');
  const [review, setReview] = useState<ReviewedPost>();
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState('');
  const content = candidates.data?.items.find((item) => item.key === selected);

  async function preview() {
    if (!content || busy) {
      return;
    }
    setBusy(true);
    setNotice('');
    try {
      const draft: Draft = {
        connection_id: connection.id,
        key: content.key,
        locale: language,
        revision_id: content.revision_id,
        text,
      };
      const scheduled_at = when ? new Date(when).toISOString() : undefined;
      const result = await call<Preview>('/owner/social/preview', 'POST', draft, csrf);
      setReview({ ...result, ...draft, scheduled_at, id: crypto.randomUUID().replaceAll('-', '') });
    } catch {
      setNotice('social.previewFailed');
    } finally {
      setBusy(false);
    }
  }

  async function schedule() {
    if (!review || busy) {
      return;
    }
    const { id, key, locale, revision_id, text, path, scheduled_at, connection_id } = review;
    setBusy(true);
    setNotice('');
    try {
      await call(
        '/owner/social/schedule',
        'POST',
        {
          id,
          key,
          locale,
          revision_id,
          text,
          path,
          scheduled_at,
          connection_id,
        },
        csrf,
      );
      setReview(undefined);
      setText('');
      setNotice('social.queued');
      refresh();
    } catch {
      setNotice('social.failedRequest');
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="vb-card">
      <h2>{t('social.compose')}</h2>
      <form
        onSubmit={(event) => {
          event.preventDefault();
          void preview();
        }}
      >
        <fieldset className="owner-fields" disabled={busy}>
          <Field label={t('social.language')}>
            <select
              value={language}
              onChange={(event) => {
                if (isLocale(event.target.value)) {
                  setLanguage(event.target.value);
                  setCursor('');
                  select('');
                  setReview(undefined);
                }
              }}
            >
              {localeCodes.map((code) => (
                <option key={code} value={code}>
                  {t('language.' + code)}
                </option>
              ))}
            </select>
          </Field>
          <Field label={t('social.content')}>
            <select
              required
              value={selected}
              onChange={(event) => {
                select(event.target.value);
                setReview(undefined);
              }}
            >
              <option value="">{t('social.chooseContent')}</option>
              {candidates.data?.items.map((item) => (
                <option key={item.key} value={item.key}>
                  {item.title}
                </option>
              ))}
            </select>
          </Field>
          {candidates.error && <p role="alert">{t('social.failedRequest')}</p>}
          {candidates.data?.items.length === 0 && <p>{t('social.noContent')}</p>}
          {cursor && (
            <Button
              type="button"
              onClick={() => {
                setCursor('');
                select('');
                setReview(undefined);
              }}
            >
              {t('social.newer')}
            </Button>
          )}
          {candidates.data?.next && (
            <Button
              type="button"
              onClick={() => {
                setCursor(candidates.data?.next || '');
                select('');
                setReview(undefined);
              }}
            >
              {t('social.moreContent')}
            </Button>
          )}
          <Field label={t('social.text')}>
            <textarea
              required
              dir="auto"
              maxLength={500}
              value={text}
              onChange={(event) => {
                setText(event.target.value);
                setReview(undefined);
              }}
            />
          </Field>
          <Field label={t('social.date')}>
            <input
              type="datetime-local"
              value={when}
              onChange={(event) => {
                setWhen(event.target.value);
                setReview(undefined);
              }}
            />
          </Field>
          <Button type="submit" disabled={!content || !text.trim()}>
            {t('social.preview')}
          </Button>
        </fieldset>
      </form>
      {review && (
        <div className="vb-card">
          <h3>{t('social.preview')}</h3>
          <p>
            <bdi>{review.destination}</bdi>
          </p>
          {review.scheduled_at && (
            <p>
              <time dateTime={review.scheduled_at}>
                {date(review.scheduled_at, { dateStyle: 'medium', timeStyle: 'short' })}
              </time>
            </p>
          )}
          <p className="social-post-body" dir="auto" lang={review.locale}>
            {review.body}
          </p>
          <Button disabled={busy} onClick={() => void schedule()}>
            {t(review.scheduled_at ? 'social.schedule' : 'social.send')}
          </Button>
        </div>
      )}
      {notice && <p role="status">{t(notice)}</p>}
    </section>
  );
}
