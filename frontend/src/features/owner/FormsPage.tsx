import { useState } from 'react';
import { Button, Field, useI18n } from '@ui';
import {
  formsURL,
  leadsURL,
  type FieldKind,
  type LeadStatus,
  type Lead,
  type LeadForm,
} from '../../api/forms';
import { useResource } from '../../pages/public/api';
import { call } from '../auth/api';
import { OwnerPageHeading } from './OwnerWorkspace';

const fieldKinds: FieldKind[] = ['text', 'email', 'textarea'];
const statuses: LeadStatus[] = ['new', 'contacted', 'closed'];

function emptyForm(): LeadForm {
  return {
    key: '',
    title: '',
    version: 0,
    enabled: false,
    consent: '',
    retention_days: 90,
    fields: [{ key: '', label: '', kind: 'text', required: true }],
  };
}

export function FormsPage({ csrf }: { csrf: string }) {
  const { t } = useI18n();
  const forms = useResource<LeadForm[]>(formsURL);
  const [cursor, setCursor] = useState('');
  const inbox = useResource<{ items: Lead[]; next?: string }>(
    leadsURL + (cursor ? '?cursor=' + encodeURIComponent(cursor) : ''),
  );
  const [form, setForm] = useState(emptyForm);
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState('');
  const change = <K extends keyof LeadForm>(key: K, value: LeadForm[K]) =>
    setForm((current) => ({ ...current, [key]: value }));
  return (
    <>
      <OwnerPageHeading title={t('owner.forms')} description={t('forms.manageHelp')} />
      <div className="vb-card">
        <Field label={t('forms.definition')}>
          <select
            value={form.version ? form.key : ''}
            onChange={(event) => {
              setForm(forms.data?.find(({ key }) => key === event.target.value) ?? emptyForm());
              setNotice('');
            }}
          >
            <option value="">{t('forms.newForm')}</option>
            {forms.data?.map((item) => (
              <option key={item.key} value={item.key}>
                {item.title}
              </option>
            ))}
          </select>
        </Field>
        {forms.error && <Button onClick={forms.retry}>{t('forms.retry')}</Button>}
        <form
          onSubmit={async (event) => {
            event.preventDefault();
            setBusy(true);
            setNotice('');
            try {
              const saved = await call<LeadForm>('/owner/forms', 'PUT', form, csrf);
              setForm(saved);
              forms.retry();
              setNotice('forms.saved');
            } catch {
              setNotice('forms.failed');
            } finally {
              setBusy(false);
            }
          }}
        >
          <fieldset disabled={busy}>
            <Field label={t('forms.key')}>
              <input
                required
                maxLength={64}
                pattern="[a-z][a-z0-9_\-]{0,63}"
                value={form.key}
                disabled={form.version > 0}
                onChange={(event) => change('key', event.target.value)}
              />
            </Field>
            <Field label={t('forms.title')}>
              <input
                required
                dir="auto"
                maxLength={100}
                value={form.title}
                onChange={(event) => change('title', event.target.value)}
              />
            </Field>
            <Field label={t('forms.retention')}>
              <input
                type="number"
                required
                min={1}
                max={365}
                value={form.retention_days}
                onChange={(event) => change('retention_days', event.target.valueAsNumber)}
              />
            </Field>
            <Field label={t('forms.consent')}>
              <input
                dir="auto"
                maxLength={500}
                value={form.consent || ''}
                onChange={(event) => change('consent', event.target.value)}
              />
            </Field>
            <label className="config-check">
              <input
                type="checkbox"
                checked={form.enabled}
                onChange={(event) => change('enabled', event.target.checked)}
              />
              {t('forms.enabled')}
            </label>
            {form.fields.map((field, index) => {
              const update = (patch: Partial<typeof field>) =>
                change(
                  'fields',
                  form.fields.map((value, position) =>
                    position === index ? { ...value, ...patch } : value,
                  ),
                );
              return (
                <fieldset key={index} className="vb-card">
                  <legend>{t('forms.field')}</legend>
                  <Field label={t('forms.key')}>
                    <input
                      required
                      maxLength={64}
                      pattern="[a-z][a-z0-9_\-]{0,63}"
                      value={field.key}
                      onChange={(event) => update({ key: event.target.value })}
                    />
                  </Field>
                  <Field label={t('forms.label')}>
                    <input
                      required
                      dir="auto"
                      maxLength={100}
                      value={field.label}
                      onChange={(event) => update({ label: event.target.value })}
                    />
                  </Field>
                  <Field label={t('forms.kind')}>
                    <select
                      value={field.kind}
                      onChange={(event) => update({ kind: event.target.value as FieldKind })}
                    >
                      {fieldKinds.map((kind) => (
                        <option key={kind} value={kind}>
                          {t('forms.' + kind)}
                        </option>
                      ))}
                    </select>
                  </Field>
                  <label className="config-check">
                    <input
                      type="checkbox"
                      checked={field.required}
                      onChange={(event) => update({ required: event.target.checked })}
                    />
                    {t('forms.required')}
                  </label>
                  <Button
                    type="button"
                    disabled={form.fields.length === 1}
                    onClick={() =>
                      change(
                        'fields',
                        form.fields.filter((_, position) => position !== index),
                      )
                    }
                  >
                    {t('config.remove')}
                  </Button>
                </fieldset>
              );
            })}
            <Button
              type="button"
              disabled={form.fields.length >= 12}
              onClick={() =>
                change('fields', [
                  ...form.fields,
                  { key: '', label: '', kind: 'text', required: false },
                ])
              }
            >
              {t('forms.addField')}
            </Button>
            <Button type="submit">{t('forms.save')}</Button>
          </fieldset>
          {notice && <p role="status">{t(notice)}</p>}
        </form>
      </div>

      <section>
        <h2>{t('forms.inbox')}</h2>
        <a className="vb-btn" href={leadsURL + '/export'}>
          {t('forms.export')}
        </a>
        {inbox.error && <Button onClick={inbox.retry}>{t('forms.retry')}</Button>}
        {inbox.data?.items.length === 0 && <p>{t('forms.empty')}</p>}
        {inbox.data?.items.map((lead) => (
          <LeadCard key={lead.id} lead={lead} csrf={csrf} reload={inbox.retry} />
        ))}
        {cursor && <Button onClick={() => setCursor('')}>{t('forms.first')}</Button>}
        {inbox.data?.next && (
          <Button onClick={() => setCursor(inbox.data?.next ?? '')}>{t('forms.next')}</Button>
        )}
      </section>
    </>
  );
}

function LeadCard({ lead, csrf, reload }: { lead: Lead; csrf: string; reload: () => void }) {
  const { t, locale } = useI18n();
  const [status, setStatus] = useState(lead.status);
  const [tags, setTags] = useState(lead.tags.join(', '));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState(false);
  async function update(method: 'PATCH' | 'DELETE') {
    setBusy(true);
    setError(false);
    try {
      const changes =
        method === 'PATCH'
          ? {
              status,
              tags: tags
                .split(',')
                .map((tag) => tag.trim())
                .filter(Boolean),
            }
          : undefined;
      await call<void>('/owner/leads/' + lead.id, method, changes, csrf);
      reload();
    } catch {
      setError(true);
    } finally {
      setBusy(false);
    }
  }
  return (
    <article className="vb-card">
      <h3 dir="auto">{lead.form_key}</h3>
      <p>
        <bdi>{lead.page_key}</bdi> · {new Date(lead.created_at * 1000).toLocaleString(locale)}
      </p>
      {lead.notified_at > 0 && lead.status === 'new' && <p>{t('forms.newLead')}</p>}
      <dl>
        {Object.entries(lead.values).map(([key, value]) => (
          <div key={key}>
            <dt dir="auto">{key}</dt>
            <dd dir="auto">{value}</dd>
          </div>
        ))}
      </dl>
      {lead.consent && <p dir="auto">{lead.consent}</p>}
      <Field label={t('forms.status')}>
        <select value={status} onChange={(event) => setStatus(event.target.value as LeadStatus)}>
          {statuses.map((value) => (
            <option value={value} key={value}>
              {t('forms.' + value)}
            </option>
          ))}
        </select>
      </Field>
      <Field label={t('forms.tags')}>
        <input
          dir="auto"
          value={tags}
          maxLength={340}
          onChange={(event) => setTags(event.target.value)}
        />
      </Field>
      <Button disabled={busy} onClick={() => void update('PATCH')}>
        {t('forms.save')}
      </Button>
      <Button
        disabled={busy}
        variant="quiet"
        onClick={() => {
          if (window.confirm(t('forms.deleteConfirm'))) {
            void update('DELETE');
          }
        }}
      >
        {t('forms.delete')}
      </Button>
      {error && <p role="alert">{t('forms.failed')}</p>}
    </article>
  );
}
