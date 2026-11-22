import { useState, type FormEvent } from 'react';
import { Button, useI18n } from '@ui';
import { useResource } from '../../pages/public/api';
import { ResourceState } from '../../pages/public/ResourceState';
import { authenticatedTransfer } from '../auth/reauthenticate';

type SettingValue = string | number | boolean;
type Labels = Partial<Record<'en' | 'ar' | 'fr', string>>;

export type PluginManifest = {
  settings?: { name: string; label: Labels; type: 'string' | 'integer' | 'boolean' }[];
  secrets?: { name: string; label: Labels }[];
  jobs?: { name: string; every_seconds: number }[];
  https_hosts?: string[];
  mcp_tools?: { name: string }[];
};

type Configuration = {
  version: number;
  values: Record<string, SettingValue>;
  history: number[];
  secrets: Record<string, boolean>;
};

export function PluginConfiguration({
  slug,
  manifest,
  onUpdate,
}: {
  slug: string;
  manifest: PluginManifest;
  onUpdate: () => void;
}) {
  const path = `/api/v1/plugin-configuration/${slug}`;
  const resource = useResource<Configuration>(path);
  return (
    <>
      <ResourceState resource={resource} />
      {resource.data && (
        <PluginConfigurationForm
          key={resource.data.version}
          data={resource.data}
          manifest={manifest}
          path={path}
          onUpdate={() => {
            resource.retry();
            onUpdate();
          }}
        />
      )}
    </>
  );
}

function PluginConfigurationForm({
  data,
  manifest,
  path,
  onUpdate,
}: {
  data: Configuration;
  manifest: PluginManifest;
  path: string;
  onUpdate: () => void;
}) {
  const { t, locale } = useI18n();
  const [values, setValues] = useState(data.values);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [revision, setRevision] = useState('');
  const displayLabel = (item: { name: string; label: Labels }) =>
    item.label[locale] || item.label.en || item.name;

  async function save(url: string, body: object) {
    if (busy) {
      return;
    }
    setBusy(true);
    setError('');
    try {
      const response = await authenticatedTransfer(url, {
        method: 'PUT',
        credentials: 'same-origin',
        cache: 'no-store',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ expected_version: data.version, ...body }),
      });
      if (response.ok) {
        onUpdate();
      } else {
        setError(response.status === 409 ? 'plugins.conflict' : 'plugins.failed');
      }
    } catch {
      setError('plugins.failed');
    } finally {
      setBusy(false);
    }
  }

  function submitSecret(name: string, event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = event.currentTarget;
    const value = new FormData(form).get('secret');
    form.reset();
    if (typeof value === 'string' && value) {
      void save(`${path}/secrets/${name}`, { value });
    }
  }

  return (
    <>
      {!!manifest.settings?.length && (
        <form
          onSubmit={(event) => {
            event.preventDefault();
            void save(path, { values });
          }}
        >
          <fieldset className="config-fields" disabled={busy}>
            <legend>{t('plugins.settings')}</legend>
            {manifest.settings.map((field) => (
              <label className="config-field" key={field.name}>
                {displayLabel(field)}
                <input
                  type={
                    field.type === 'boolean'
                      ? 'checkbox'
                      : field.type === 'integer'
                        ? 'number'
                        : 'text'
                  }
                  checked={field.type === 'boolean' ? values[field.name] === true : undefined}
                  value={field.type === 'boolean' ? undefined : String(values[field.name] ?? '')}
                  required={field.type === 'integer'}
                  min={field.type === 'integer' ? -2147483648 : undefined}
                  max={field.type === 'integer' ? 2147483647 : undefined}
                  maxLength={field.type === 'string' ? 1024 : undefined}
                  onChange={(event) => {
                    const input = event.currentTarget;
                    const value =
                      field.type === 'boolean'
                        ? input.checked
                        : field.type === 'integer' && input.value !== ''
                          ? input.valueAsNumber
                          : input.value;
                    setValues({ ...values, [field.name]: value });
                  }}
                />
              </label>
            ))}
            <Button type="submit">{t('plugins.saveSettings')}</Button>
            {data.history.length > 0 && (
              <div className="config-field">
                <label>
                  {t('plugins.settingsHistory')}
                  <select value={revision} onChange={(event) => setRevision(event.target.value)}>
                    <option value="">{t('plugins.selectRevision')}</option>
                    {data.history.map((version) => (
                      <option key={version}>{version}</option>
                    ))}
                  </select>
                </label>
                <Button
                  disabled={!revision}
                  onClick={() => save(path, { restore_version: Number(revision) })}
                >
                  {t('plugins.restoreSettings')}
                </Button>
              </div>
            )}
          </fieldset>
        </form>
      )}
      {manifest.secrets?.map((secret) => (
        <form key={secret.name} onSubmit={(event) => submitSecret(secret.name, event)}>
          <label className="config-field">
            {displayLabel(secret)}
            <input
              name="secret"
              type="password"
              maxLength={4096}
              autoComplete="new-password"
              required
            />
          </label>
          <Button type="submit" disabled={busy}>
            {t('plugins.saveSecret')}
          </Button>
          {data.secrets[secret.name] && (
            <Button
              variant="quiet"
              disabled={busy}
              onClick={() => save(`${path}/secrets/${secret.name}`, { value: null })}
            >
              {t('plugins.removeSecret')}
            </Button>
          )}
        </form>
      ))}
      {error && <p role="alert">{t(error)}</p>}
    </>
  );
}
