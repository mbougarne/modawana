import { useState } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';
import { Button, useI18n } from '@ui';
import { call } from './api';
import { useResource } from '../../pages/public/api';
import { ResourceState } from '../../pages/public/ResourceState';
import {
  creationOptions,
  credentialJSON,
  requestOptions,
  type CreationJSON,
  type RequestJSON,
} from './webauthn';

type StaffSession = { member: { name: string; role: string }; csrf: string };
type Kind = 'article' | 'page';
type Entry = { key: string; title: string; status: string };
type Draft = {
  version: number;
  published_revision_id: string;
  revision: { revision_id: string; title: string; markdown: string; body_format?: string };
};
const authPath = '/auth/staff';
const contentPath = '/staff/content';

export function StaffPage() {
  const { locale, dir, t } = useI18n();
  const location = useLocation();
  const navigate = useNavigate();
  const invite = new URLSearchParams(location.hash.slice(1)).get('invite') ?? '';
  const session = useResource<StaffSession>('/api/v1' + authPath + '/session');
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState('');

  async function signIn(enroll: boolean) {
    setBusy(true);
    setNotice('');
    try {
      const mode = authPath + (enroll ? '/enroll' : '/login');
      const next = await call<{ csrf: string; options: { publicKey: CreationJSON & RequestJSON } }>(
        mode + '/start',
        'POST',
        enroll ? { invite } : {},
        undefined,
        { 'X-Blog-Request': '1' },
      );
      const options = next.options.publicKey;
      const credential = (
        enroll
          ? await navigator.credentials.create({ publicKey: creationOptions(options) })
          : await navigator.credentials.get({ publicKey: requestOptions(options) })
      ) as PublicKeyCredential | null;
      if (!credential) {
        throw new Error();
      }
      await call(mode + '/finish', 'POST', credentialJSON(credential), next.csrf);
      navigate(`/${locale}/staff`, { replace: true });
      session.retry();
    } catch {
      setNotice('staff.failed');
    } finally {
      setBusy(false);
    }
  }

  async function signOut(staff: StaffSession) {
    setBusy(true);
    try {
      await call(authPath + '/session', 'DELETE', undefined, staff.csrf);
      session.retry();
    } catch {
      setNotice('staff.failed');
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="vb public-site ds-ui" lang={locale} dir={dir}>
      <main className="vb-shell">
        <h1>{t('staff.workspace')}</h1>
        {session.loading && <p role="status">{t('staff.loading')}</p>}
        {notice && <p role="alert">{t(notice)}</p>}
        {session.data && !invite ? (
          <>
            <div className="owner-fields">
              <bdi>{session.data.member.name}</bdi>
              <span>{t('staff.role.' + session.data.member.role)}</span>
              <Button disabled={busy} onClick={() => void signOut(session.data!)}>
                {t('auth.signout')}
              </Button>
            </div>
            {session.data.member.role !== 'store_manager' && (
              <StaffContent session={session.data} />
            )}
          </>
        ) : (
          !session.loading && (
            <Button disabled={busy} onClick={() => void signIn(!!invite)}>
              {t(invite ? 'staff.accept' : 'staff.signIn')}
            </Button>
          )
        )}
      </main>
    </div>
  );
}

function StaffContent({ session }: { session: StaffSession }) {
  const { locale, t } = useI18n();
  const [kind, setKind] = useState<Kind>('article');
  const [key, setKey] = useState('');
  const [slug, setSlug] = useState('');
  const [cursor, setCursor] = useState('');
  const listing = useResource<{ items: Entry[]; next_cursor: string }>(
    '/api/v1' + contentPath + '?' + new URLSearchParams({ locale, kind, cursor }),
  );
  const selected = useResource<Draft>(
    key ? '/api/v1' + contentPath + '/' + key + '/' + locale + '/draft' : undefined,
  );
  return (
    <section className="owner-panel owner-form">
      <label>
        {t('staff.contentType')}
        <select
          value={kind}
          onChange={(e) => {
            setKind(e.target.value as Kind);
            setKey('');
            setSlug('');
            setCursor('');
          }}
        >
          <option value="article">{t('owner.posts')}</option>
          <option value="page">{t('owner.pages')}</option>
        </select>
      </label>
      <ResourceState resource={listing} />
      <ul>
        {listing.data?.items.map((item) => (
          <li key={item.key}>
            <Button
              variant="quiet"
              onClick={() => {
                setKey(item.key);
                setSlug('');
              }}
            >
              <bdi>{item.title}</bdi>
            </Button>
          </li>
        ))}
      </ul>
      {listing.data?.next_cursor && (
        <Button onClick={() => setCursor(listing.data!.next_cursor)}>{t('staff.more')}</Button>
      )}
      {cursor && (
        <Button variant="quiet" onClick={() => setCursor('')}>
          {t('staff.first')}
        </Button>
      )}
      <ResourceState resource={selected} />
      {key && selected.data && (
        <StaffDraftEditor
          key={key + ':' + selected.data.version}
          session={session}
          itemKey={key}
          kind={kind}
          initial={selected.data}
          onSaved={() => {
            selected.retry();
            listing.retry();
          }}
        />
      )}
      {!key && (
        <>
          <label>
            {t('staff.slug')}{' '}
            <input maxLength={120} value={slug} onChange={(e) => setSlug(e.target.value)} />
          </label>
          {slug && /^[a-z0-9][a-z0-9_-]{0,119}$/.test(slug) && (
            <StaffDraftEditor
              key={kind + ':' + slug}
              session={session}
              itemKey={slug}
              kind={kind}
              onSaved={() => {
                setSlug('');
                setKey(slug);
                listing.retry();
              }}
            />
          )}
        </>
      )}
      {key && (
        <Button
          variant="quiet"
          onClick={() => {
            setKey('');
            setSlug('');
          }}
        >
          {t('staff.newDraft')}
        </Button>
      )}
    </section>
  );
}

function StaffDraftEditor({
  session,
  itemKey,
  kind,
  initial,
  onSaved,
}: {
  session: StaffSession;
  itemKey: string;
  kind: Kind;
  initial?: Draft;
  onSaved: () => void;
}) {
  const { locale, t } = useI18n();
  const [title, setTitle] = useState(initial?.revision.title ?? '');
  const [markdown, setMarkdown] = useState(initial?.revision.markdown ?? '');
  const [pending, setPending] = useState(false);
  const [notice, setNotice] = useState('');
  const base = contentPath + '/' + itemKey + '/' + locale;
  const blocks = initial?.revision.body_format === 'blocks';

  async function change(publish = false) {
    setPending(true);
    setNotice('');
    try {
      if (publish && initial) {
        await call(
          base + '/publish',
          'POST',
          { expected_version: initial.version, revision_id: initial.revision.revision_id },
          session.csrf,
        );
      } else {
        await call(
          base + '/draft',
          'PUT',
          {
            expected_version: initial?.version ?? 0,
            title,
            markdown,
            ...(!initial ? { kind, slug: itemKey } : {}),
          },
          session.csrf,
        );
      }
      onSaved();
    } catch {
      setNotice('staff.failed');
    } finally {
      setPending(false);
    }
  }

  return (
    <div className="owner-form">
      <label>
        {t('staff.title')}{' '}
        <input
          dir="auto"
          value={title}
          disabled={blocks}
          onChange={(e) => setTitle(e.target.value)}
        />
      </label>
      {blocks ? (
        <p>{t('staff.blocks')}</p>
      ) : (
        <>
          <label>
            {t('staff.markdown')}{' '}
            <textarea dir="auto" value={markdown} onChange={(e) => setMarkdown(e.target.value)} />
          </label>
          <Button
            disabled={pending || !title.trim() || !markdown.trim()}
            onClick={() => void change()}
          >
            {t('staff.saveDraft')}
          </Button>
        </>
      )}
      {initial &&
        session.member.role === 'owner' &&
        initial.published_revision_id !== initial.revision.revision_id &&
        initial.revision.title === title &&
        initial.revision.markdown === markdown && (
          <Button disabled={pending} onClick={() => void change(true)}>
            {t('staff.publish')}
          </Button>
        )}
      {notice && <p role="alert">{t(notice)}</p>}
    </div>
  );
}
