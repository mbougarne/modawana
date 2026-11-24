import { useState } from 'react';
import { editorialAPI, type EditorialConfig } from '../../api/editorial';
import { Button, Field, useI18n } from '@ui';
import { authenticatedTransfer } from '../auth/reauthenticate';
import { ResourceState } from '../../pages/public/ResourceState';
import { useResource } from '../../pages/public/api';
import { OwnerPageHeading } from './OwnerWorkspace';

export function EditorialSettings() {
  const { t } = useI18n();
  const settings = useResource<EditorialConfig>(editorialAPI);
  const [notice, setNotice] = useState('');
  return (
    <>
      <OwnerPageHeading title={t('owner.editorial')} description={t('editorial.settingsHelp')} />
      <ResourceState resource={settings} />
      {notice && <p role="status">{t(notice)}</p>}
      {settings.data && (
        <SettingsForm
          config={settings.data}
          saved={() => {
            setNotice('editorial.saved');
            settings.retry();
          }}
        />
      )}
    </>
  );
}

function SettingsForm({ config, saved }: { config: EditorialConfig; saved: () => void }) {
  const { t } = useI18n();
  const [enabled, setEnabled] = useState(config.enabled);
  const [limit, setLimit] = useState(String(config.daily_limit));
  const [credential, setCredential] = useState('');
  const [remove, setRemove] = useState(false);
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState('');

  async function save() {
    if (busy) {
      return;
    }
    setBusy(true);
    setNotice('');
    try {
      const response = await authenticatedTransfer(editorialAPI, {
        method: 'PUT',
        credentials: 'same-origin',
        cache: 'no-store',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          version: config.version,
          enabled,
          daily_limit: Number(limit),
          credential: credential.trim() || undefined,
          remove_credential: remove || undefined,
        }),
      });
      if (!response.ok) {
        if (response.status === 409) {
          setNotice('config.conflict');
        } else {
          const data = await response.json().catch(() => undefined);
          setNotice(
            data?.error?.code === 'editorial_key'
              ? 'editorial.keyUnavailable'
              : 'editorial.settingsFailed',
          );
        }
        return;
      }
      setCredential('');
      saved();
    } catch {
      setNotice('editorial.settingsFailed');
    } finally {
      setBusy(false);
    }
  }

  return (
    <form
      className="owner-panel owner-form"
      onSubmit={(event) => {
        event.preventDefault();
        void save();
      }}
    >
      <h2>OpenAI</h2>
      <p>{t('editorial.usage', { used: config.used, limit: config.daily_limit })}</p>
      <p>{t(config.configured ? 'editorial.keySaved' : 'editorial.noKey')}</p>
      {!config.key_available && <p role="status">{t('editorial.keyUnavailable')}</p>}
      <fieldset className="owner-fields" disabled={busy}>
        <Field label={t('editorial.credential')}>
          <input
            type="password"
            autoComplete="new-password"
            maxLength={1024}
            dir="ltr"
            spellCheck={false}
            disabled={remove || !config.key_available}
            value={credential}
            onChange={(event) => setCredential(event.target.value)}
          />
        </Field>
        {config.configured && (
          <label>
            <input
              type="checkbox"
              checked={remove}
              onChange={(event) => {
                setRemove(event.target.checked);
                if (event.target.checked) {
                  setCredential('');
                  setEnabled(false);
                }
              }}
            />{' '}
            {t('editorial.removeKey')}
          </label>
        )}
        <label>
          <input
            type="checkbox"
            checked={enabled}
            disabled={remove}
            onChange={(event) => setEnabled(event.target.checked)}
          />{' '}
          {t('editorial.enabled')}
        </label>
        <Field label={t('editorial.limit')}>
          <input
            type="number"
            min={1}
            max={100}
            required
            value={limit}
            onChange={(event) => setLimit(event.target.value)}
          />
        </Field>
        <Button type="submit" variant="primary">
          {t('editorial.saveSettings')}
        </Button>
      </fieldset>
      {notice && <p role="alert">{t(notice)}</p>}
    </form>
  );
}
