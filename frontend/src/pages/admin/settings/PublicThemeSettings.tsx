import { useState } from 'react';
import { Button, Label, useI18n } from '@ui';
import type { PublicThemeID } from '../../../api/configuration';
import { authenticatedTransfer } from '../../../features/auth/reauthenticate';
import { usePublicConfiguration } from '../../../features/configuration/PublicConfiguration';
import { Section } from '../../../features/configuration/controls';
import { useFieldIssue } from '../../../features/configuration/ValidationIssues';
import { useResource } from '../../public/api';

const builtinThemes = ['classic', 'folio', 'studio'] as const;
const packsPath = '/api/v1/settings/theme-packs';

type ThemePack = {
  id: `pack-${string}`;
  name: string;
  revision: string;
  state: 'available' | 'installed' | 'changed' | 'missing' | 'invalid';
  candidate_name?: string;
  candidate_checksum?: string;
};

export function PublicThemeSettings({
  value,
  saved,
  onChange,
  onCSRF,
}: {
  value?: PublicThemeID;
  saved?: PublicThemeID;
  onChange: (choice: PublicThemeID) => void;
  onCSRF: (csrf: string) => void;
}) {
  const { t, locale } = useI18n();
  const { refresh } = usePublicConfiguration();
  const resource = useResource<{ items: ThemePack[] }>(packsPath);
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState('');
  const selected = value ?? 'classic';
  const themeIssue = useFieldIssue('public_theme');
  const items = resource.data?.items ?? [];
  const available = items.filter((pack) => pack.state === 'installed');
  const unavailable =
    selected.startsWith('pack-') && !available.some((pack) => pack.id === selected);
  const usingFallback =
    resource.data && saved?.startsWith('pack-') && !available.some((pack) => pack.id === saved);

  async function changeApproval(pack: ThemePack, action: 'approve' | 'remove') {
    if (busy) {
      return;
    }
    if (action === 'remove' && !window.confirm(t('themePacks.removeConfirm'))) {
      return;
    }
    setBusy(true);
    setNotice('');
    try {
      const response = await authenticatedTransfer(
        `${packsPath}/${pack.id}/${action}`,
        {
          method: 'POST',
          credentials: 'same-origin',
          cache: 'no-store',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            expected_revision: pack.revision,
            expected_checksum: pack.candidate_checksum,
          }),
        },
        (session) => {
          if (session.csrf) {
            onCSRF(session.csrf);
          }
        },
      );
      if (response.ok) {
        setNotice('themePacks.saved');
        resource.retry();
        void refresh();
      } else if (response.status === 409) {
        setNotice('themePacks.conflict');
        resource.retry();
      } else {
        setNotice('themePacks.failed');
      }
    } catch {
      setNotice('themePacks.failed');
    } finally {
      setBusy(false);
    }
  }

  return (
    <Section name="config.publicAppearance">
      <label className="config-field">
        <Label name="config.publicTheme" />
        <select
          {...themeIssue}
          value={selected}
          onChange={(event) => onChange(event.target.value as PublicThemeID)}
        >
          {builtinThemes.map((theme) => (
            <option key={theme} value={theme}>
              {t('publicTheme.' + theme)}
            </option>
          ))}
          {available.map((pack) => (
            <option key={pack.id} value={pack.id}>
              {pack.name}
            </option>
          ))}
          {unavailable && (
            <option value={selected} disabled>
              {selected}
            </option>
          )}
        </select>
      </label>
      {usingFallback && <p role="status">{t('themePacks.fallback')}</p>}
      {!unavailable && (
        <a
          className="vb-btn vb-btn-secondary vb-btn-sm"
          href={`/${locale}/admin/settings/theme-preview?theme=${encodeURIComponent(selected)}`}
          target="_blank"
          rel="noopener"
        >
          {t('config.previewPublicTheme')}
        </a>
      )}
      <h3>
        <Label name="themePacks.title" />
      </h3>
      <p>{t('themePacks.help')}</p>
      {resource.loading && <p role="status">{t('public.loading')}</p>}
      {resource.error && (
        <p role="alert">
          {t('public.error')}{' '}
          <Button type="button" variant="quiet" onClick={resource.retry}>
            {t('public.retry')}
          </Button>
        </p>
      )}
      {resource.data && items.length === 0 && <p>{t('themePacks.empty')}</p>}
      {items.map((pack) => (
        <div key={pack.id} className="config-field">
          <strong dir="auto">{pack.candidate_name || pack.name || pack.id}</strong>
          <span className="vb-meta">
            <bdi>{pack.id}</bdi> · {t('plugins.state.' + pack.state)}
          </span>
          {pack.candidate_checksum && (
            <span>
              <code dir="ltr">{pack.candidate_checksum.slice(0, 12)}</code>
            </span>
          )}
          <div className="owner-fields">
            {pack.candidate_checksum && (
              <Button type="button" disabled={busy} onClick={() => changeApproval(pack, 'approve')}>
                {t(pack.revision ? 'plugins.reapprove' : 'themePacks.approve')}
              </Button>
            )}
            {pack.revision && (
              <Button
                type="button"
                variant="quiet"
                disabled={busy}
                onClick={() => changeApproval(pack, 'remove')}
              >
                {t('themePacks.remove')}
              </Button>
            )}
          </div>
        </div>
      ))}
      {notice && <p role="status">{t(notice)}</p>}
    </Section>
  );
}
