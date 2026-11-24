import { useState } from 'react';
import { Button, Field, useI18n } from '@ui';
import { useResource } from '../../pages/public/api';
import { AuthError, call } from '../auth/api';
import { ResourceState } from '../../pages/public/ResourceState';
import { OwnerPageHeading } from './OwnerWorkspace';

const path = '/owner/analytics';

interface Settings {
  version: number;
  enabled: boolean;
  consent: 'opt_in' | 'unless_declined';
  retention_days: number;
  exclusions: string[];
}

interface Totals {
  page_views: number;
  leads: number;
}

interface Report {
  settings: Settings;
  totals: Totals;
  days: (Totals & { day: string })[];
  pages: (Totals & { path: string })[];
}

export function AnalyticsPage({ csrf }: { csrf: string }) {
  const { t, date, locale } = useI18n();
  const report = useResource<Report>('/api/v1' + path);
  const [saved, setSaved] = useState(false);
  const fmt = new Intl.NumberFormat(locale);

  return (
    <>
      <OwnerPageHeading title={t('owner.analytics')} description={t('analytics.help')}>
        <Button onClick={report.retry}>{t('analytics.refresh')}</Button>
      </OwnerPageHeading>
      <ResourceState resource={report} />
      {saved && <p role="status">{t('analytics.saved')}</p>}
      {report.data && (
        <>
          <AnalyticsSettings
            key={report.data.settings.version}
            initial={report.data.settings}
            csrf={csrf}
            saved={() => {
              setSaved(true);
              report.retry();
            }}
          />
          <section className="owner-panel owner-form">
            <h2>{t('analytics.summary')}</h2>
            <dl className="analytics-totals">
              <div>
                <dt>{t('analytics.pageViews')}</dt>
                <dd>{fmt.format(report.data.totals.page_views)}</dd>
              </div>
              <div>
                <dt>{t('analytics.leads')}</dt>
                <dd>{fmt.format(report.data.totals.leads)}</dd>
              </div>
            </dl>
            {report.data.days.length === 0 && <p>{t('analytics.empty')}</p>}
            {report.data.days.length > 0 && (
              <>
                <AnalyticsTable
                  caption={t('analytics.days')}
                  column={t('analytics.day')}
                  rows={report.data.days.map(({ day, ...totals }) => ({
                    key: day,
                    label: date(day),
                    ...totals,
                  }))}
                />
                <AnalyticsTable
                  caption={t('analytics.topPages')}
                  column={t('analytics.path')}
                  rows={report.data.pages.map(({ path, ...totals }) => ({
                    key: path,
                    label: path,
                    ...totals,
                  }))}
                />
              </>
            )}
          </section>
        </>
      )}
    </>
  );
}

function AnalyticsSettings({
  initial,
  csrf,
  saved,
}: {
  initial: Settings;
  csrf: string;
  saved: () => void;
}) {
  const { t } = useI18n();
  const [enabled, setEnabled] = useState(initial.enabled);
  const [consent, setConsent] = useState(initial.consent);
  const [retention, setRetention] = useState(String(initial.retention_days));
  const [exclusions, setExclusions] = useState(initial.exclusions.join('\n'));
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState('');

  async function save() {
    if (busy) {
      return;
    }
    setBusy(true);
    setFailure('');
    try {
      await call<Settings>(
        path,
        'PUT',
        {
          version: initial.version,
          enabled,
          consent,
          retention_days: Number(retention),
          exclusions: exclusions
            .split('\n')
            .map((row) => row.trim())
            .filter(Boolean),
        },
        csrf,
      );
      saved();
    } catch (error) {
      setFailure(
        error instanceof AuthError && error.code === 'revision_conflict'
          ? 'analytics.conflict'
          : 'analytics.saveFailed',
      );
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
      <h2>{t('analytics.settings')}</h2>
      <fieldset className="owner-fields" disabled={busy}>
        <label>
          <input
            type="checkbox"
            checked={enabled}
            onChange={(event) => setEnabled(event.target.checked)}
          />{' '}
          {t('analytics.enabled')}
        </label>
        <Field label={t('analytics.consent')}>
          <select
            value={consent}
            onChange={(event) =>
              setConsent(event.target.value === 'unless_declined' ? 'unless_declined' : 'opt_in')
            }
          >
            <option value="opt_in">{t('analytics.mode.optIn')}</option>
            <option value="unless_declined">{t('analytics.mode.unlessDeclined')}</option>
          </select>
        </Field>
        <Field label={t('analytics.retention')}>
          <input
            type="number"
            required
            min={1}
            max={90}
            value={retention}
            onChange={(event) => setRetention(event.target.value)}
          />
        </Field>
        <Field label={t('analytics.exclusions')} help={t('analytics.exclusionsHelp')}>
          <textarea
            dir="ltr"
            rows={4}
            value={exclusions}
            onChange={(event) => setExclusions(event.target.value)}
          />
        </Field>
        <Button type="submit" variant="primary">
          {t('analytics.save')}
        </Button>
      </fieldset>
      {failure && <p role="alert">{t(failure)}</p>}
    </form>
  );
}

function AnalyticsTable({
  caption,
  column,
  rows,
}: {
  caption: string;
  column: string;
  rows: (Totals & { label: string; key: string })[];
}) {
  const { t, locale } = useI18n();
  const fmt = new Intl.NumberFormat(locale);
  return (
    <div className="analytics-table-wrap">
      <table className="analytics-table">
        <caption>{caption}</caption>
        <thead>
          <tr>
            <th scope="col">{column}</th>
            <th scope="col">{t('analytics.pageViews')}</th>
            <th scope="col">{t('analytics.leads')}</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => (
            <tr key={row.key}>
              <th scope="row">
                <bdi>{row.label}</bdi>
              </th>
              <td>{fmt.format(row.page_views)}</td>
              <td>{fmt.format(row.leads)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
