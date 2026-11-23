import { useState, type FormEvent } from 'react';
import { Button, useI18n } from '@ui';
import { useResource } from '../../pages/public/api';
import { ResourceState } from '../../pages/public/ResourceState';
import { authenticatedTransfer } from './reauthenticate';
import type { State } from './api';

type Role = 'owner' | 'editor' | 'store_manager';
type Member = { id: string; name: string; role: Role; revoked_at?: string };
type Invitation = Member & { expires_at: string; consumed_at?: string };
const base = '/api/v1/staff/management';

export function StaffManagement({ onSession }: { onSession: (state: State) => void }) {
  const { t, locale } = useI18n();
  const directory = useResource<{ members: Member[]; invitations: Invitation[] }>(base);
  const [name, setName] = useState('');
  const [role, setRole] = useState<Role>('editor');
  const [link, setLink] = useState('');
  const [notice, setNotice] = useState('');
  const [busy, setBusy] = useState(false);

  async function change(path: string, body?: { name: string; role: Role }) {
    setBusy(true);
    setNotice('');
    try {
      const response = await authenticatedTransfer(
        base + path,
        {
          method: 'POST',
          credentials: 'same-origin',
          cache: 'no-store',
          headers: body ? { 'Content-Type': 'application/json' } : {},
          body: body ? JSON.stringify(body) : undefined,
        },
        onSession,
      );
      if (!response.ok) {
        throw new Error();
      }
      if (body) {
        const result: { token: string } = await response.json();
        const url = new URL(`/${locale}/staff`, window.location.origin);
        url.hash = 'invite=' + encodeURIComponent(result.token);
        setLink(url.href);
        setName('');
      } else {
        setLink('');
        setNotice('staff.revoked');
      }
      directory.retry();
    } catch {
      setNotice('staff.failed');
    } finally {
      setBusy(false);
    }
  }

  function create(e: FormEvent) {
    e.preventDefault();
    setLink('');
    if (name.trim()) {
      void change('/invitations', { name, role });
    }
  }

  return (
    <section className="owner-panel owner-form">
      <h2>{t('staff.management')}</h2>
      <form onSubmit={create} className="owner-fields">
        <label>
          {t('staff.name')}{' '}
          <input maxLength={80} value={name} onChange={(e) => setName(e.target.value)} required />
        </label>
        <label>
          {t('staff.role')}
          <select value={role} onChange={(e) => setRole(e.target.value as Role)}>
            {(['owner', 'editor', 'store_manager'] as const).map((value) => (
              <option value={value} key={value}>
                {t('staff.role.' + value)}
              </option>
            ))}
          </select>
        </label>
        <Button type="submit" disabled={busy || !name.trim()}>
          {t('staff.invite')}
        </Button>
      </form>
      {link && (
        <label>
          {t('staff.inviteLink')}
          <input dir="ltr" value={link} readOnly onFocus={(e) => e.target.select()} />
        </label>
      )}
      {notice && <p role="status">{t(notice)}</p>}
      <ResourceState resource={directory} />
      {directory.data?.invitations
        .filter(
          (invite) =>
            !invite.consumed_at && !invite.revoked_at && Date.parse(invite.expires_at) > Date.now(),
        )
        .map((invite) => (
          <div className="owner-fields" key={invite.id}>
            <bdi>{invite.name}</bdi>
            <span>{t('staff.role.' + invite.role)}</span>
            <span>{t('staff.pending')}</span>
            <Button
              variant="quiet"
              disabled={busy}
              onClick={() => void change('/invitations/' + invite.id + '/revoke')}
            >
              {t('staff.revoke')}
            </Button>
          </div>
        ))}
      {directory.data?.members.map((member) => (
        <div className="owner-fields" key={member.id}>
          <bdi>{member.name}</bdi>
          <span>{t('staff.role.' + member.role)}</span>
          {member.revoked_at ? (
            <span>{t('staff.revoked')}</span>
          ) : (
            <Button
              variant="quiet"
              disabled={busy}
              onClick={() => void change('/members/' + member.id + '/revoke')}
            >
              {t('staff.revoke')}
            </Button>
          )}
        </div>
      ))}
    </section>
  );
}
