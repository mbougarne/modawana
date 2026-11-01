import { titleSlugChange } from '../content/slug';
import { CollectionContents, type SelectedMember } from './CollectionContents';
import { useRef, useState } from 'react';
import { StableKeyField } from '@ui/components/StableKeyField';
import { OwnerPageHeading } from './OwnerWorkspace';
import { Button, Field, useI18n } from '@ui';
import { call, AuthError, requestErrorKey } from '../auth/api';
import { useResource } from '../../pages/public/api';
import { ResourceState } from '../../pages/public/ResourceState';
import type { CollectionDocument, CollectionInput, Locale, TaxonomyTerm } from '../../api/content';

export function Organization({ csrf, page }: { csrf: string; page: 'collections' | 'taxonomy' }) {
  const { t, locale } = useI18n();
  const [language, setLanguage] = useState<Locale>(locale);
  const [cursor, setCursor] = useState('');
  const [notice, setNotice] = useState('');
  const [busy, setBusy] = useState(false);
  const [key, setKey] = useState('');
  const [doc, setDoc] = useState<CollectionDocument>();
  const [members, setMembers] = useState<SelectedMember[]>([]);
  const collectionSlugManual = useRef(false);
  const termSlugManual = useRef(false);
  const retry = useRef<{ signature: string; id: string } | undefined>(undefined);
  const [planDirty, setPlanDirty] = useState(false);
  const [form, setForm] = useState<CollectionInput>({
    expected_version: 0,
    kind: 'learning',
    title: '',
    slug: '',
    summary: '',
    visible: true,
  });
  const [term, setTerm] = useState({ kind: 'category', label: '', slug: '', expected_version: 0 });
  const list = useResource<{
    items: ({ key: string; kind: string; title?: string } & Partial<TaxonomyTerm>)[];
    next_cursor?: string;
  }>(
    page === 'collections'
      ? '/api/v1/collections?locale=' + language + '&cursor=' + cursor
      : '/api/v1/taxonomy?locale=' + language,
  );
  async function run(fn: () => Promise<void>) {
    if (busy) {
      return;
    }
    setBusy(true);
    setNotice('');
    try {
      await fn();
    } catch (e) {
      setNotice(
        e instanceof AuthError && e.code === 'revision_conflict'
          ? 'config.conflict'
          : e instanceof AuthError &&
              e.fields.some((f) => /^(conversion_|collection_)/.test(f.code))
            ? 'conversion.' + e.fields.find((f) => /^(conversion_|collection_)/.test(f.code))!.code
            : e instanceof AuthError && e.code === 'invalid_fields' && page === 'collections'
              ? 'request.invalid'
              : requestErrorKey(e),
      );
    } finally {
      setBusy(false);
    }
  }
  async function open(k: string, lang = language) {
    const d = await call<CollectionDocument>('/collections/' + encodeURIComponent(k));
    setKey(k);
    setDoc(d);
    const missing = d.assigned_members.filter(
      (member) => !d.draft_members.some((saved) => saved.key === member.key),
    );
    setMembers([...d.draft_members, ...missing]);
    setPlanDirty(missing.length > 0);
    const l = d.locales.find((v) => v.locale === lang);
    collectionSlugManual.current = !!l?.slug;
    setForm({
      expected_version: d.version,
      kind: d.kind,
      title: l?.title || '',
      slug: l?.slug || '',
      summary: l?.summary || '',
      visible: l?.visible || false,
    });
  }
  function reset() {
    setNotice('');
    collectionSlugManual.current = false;
    termSlugManual.current = false;
    setKey('');
    setDoc(undefined);
    setMembers([]);
    setForm({
      expected_version: 0,
      kind: 'learning',
      title: '',
      slug: '',
      summary: '',
      visible: false,
    });
    setTerm({ kind: 'category', label: '', slug: '', expected_version: 0 });
    setPlanDirty(false);
  }
  return (
    <>
      <OwnerPageHeading title={t('owner.' + page)} description={t('workspace.' + page + 'Help')} />
      <div className="owner-organization-grid">
        <section className="owner-panel organization-list" aria-label={t('workspace.savedItems')}>
          <h2>{t('workspace.savedItems')}</h2>
          <Field label={t('owner.locale')}>
            <select
              value={language}
              disabled={busy}
              onChange={(e) => {
                const lang = e.target.value as Locale;
                setLanguage(lang);
                if (doc) {
                  void run(() => open(key, lang));
                } else if (page === 'taxonomy' && term.expected_version > 0) {
                  void run(async () => {
                    const result = await call<{ items: TaxonomyTerm[] }>(
                      '/taxonomy?locale=' + lang,
                    );
                    const translated = result.items.find(
                      (v) => v.key === key && v.kind === term.kind,
                    );
                    termSlugManual.current = !!translated?.slug;
                    setTerm({
                      ...term,
                      label: translated?.label || '',
                      slug: translated?.slug || '',
                      expected_version: translated?.version || term.expected_version,
                    });
                  });
                } else {
                  reset();
                }
              }}
            >
              {['en', 'ar', 'fr'].map((l) => (
                <option key={l} value={l}>
                  {t('language.' + l)}
                </option>
              ))}
            </select>
          </Field>
          <ResourceState resource={list} />
          <ul className="owner-list">
            {list.data?.items.map((v) => (
              <li key={v.kind + v.key}>
                <span>
                  {v.title || v.label || v.key} · {t('owner.' + v.kind)}
                </span>
                <Button
                  disabled={busy}
                  onClick={() => {
                    if (page === 'collections') {
                      void run(() => open(v.key));
                    } else {
                      setNotice('');
                      termSlugManual.current = true;
                      setKey(v.key);
                      setTerm({
                        kind: v.kind,
                        label: v.label || '',
                        slug: v.slug || '',
                        expected_version: v.version || 0,
                      });
                    }
                  }}
                >
                  {t('owner.edit')}
                </Button>
              </li>
            ))}
          </ul>
          {list.data?.next_cursor && (
            <Button onClick={() => setCursor(list.data!.next_cursor!)}>{t('public.more')}</Button>
          )}
          <Button disabled={busy} onClick={reset}>
            {t('owner.new')}
          </Button>
        </section>
        <fieldset className="owner-fields owner-panel organization-form" disabled={busy}>
          <legend className="vb-visually-hidden">{t('workspace.editDetails')}</legend>
          <h2>{t('workspace.editDetails')}</h2>
          {notice && (
            <p
              role="status"
              className="workspace-alert"
              data-tone={
                notice === 'owner.failed' ||
                notice === 'collection.failed' ||
                notice === 'config.conflict' ||
                notice.startsWith('conversion.') ||
                notice.startsWith('request.')
                  ? 'error'
                  : 'success'
              }
            >
              {t(notice)}
            </p>
          )}
          <StableKeyField
            value={key}
            disabled={!!doc || term.expected_version > 0}
            onChange={setKey}
          />
          {page === 'taxonomy' ? (
            <>
              <Field label={t('owner.kind')}>
                <select
                  value={term.kind}
                  disabled={term.expected_version > 0}
                  onChange={(e) => setTerm({ ...term, kind: e.target.value })}
                >
                  {['category', 'tag'].map((k) => (
                    <option key={k} value={k}>
                      {t('owner.' + k)}
                    </option>
                  ))}
                </select>
              </Field>
              {(['label', 'slug'] as const).map((k) => (
                <Field key={k} label={t('owner.' + k)}>
                  <input
                    value={term[k]}
                    maxLength={120}
                    onChange={(e) => {
                      const value = e.target.value;
                      if (k === 'slug') {
                        termSlugManual.current = true;
                      }
                      setTerm({
                        ...term,
                        [k]: value,
                        ...(k === 'label'
                          ? {
                              slug: titleSlugChange(
                                term.label,
                                term.slug,
                                value,
                                termSlugManual.current,
                                false,
                                120,
                              ),
                            }
                          : {}),
                      });
                    }}
                  />
                </Field>
              ))}
              <Button
                disabled={!key || !term.label || !term.slug}
                onClick={() =>
                  void run(async () => {
                    const r = await call<{ version: number }>(
                      `/taxonomy/${term.kind}/${key}/${language}`,
                      'PUT',
                      {
                        expected_version: term.expected_version,
                        label: term.label,
                        slug: term.slug,
                      },
                      csrf,
                    );
                    setTerm({ ...term, expected_version: r.version });
                    list.retry();
                    setNotice('owner.termSaved');
                  })
                }
              >
                {t('owner.saveTerm')}
              </Button>
            </>
          ) : (
            <>
              <Field label={t('owner.kind')}>
                <select
                  value={form.kind}
                  disabled={!!doc}
                  onChange={(e) => {
                    setForm({ ...form, kind: e.target.value as CollectionInput['kind'] });
                    setMembers([]);
                    setPlanDirty(false);
                  }}
                >
                  {['learning', 'docs'].map((k) => (
                    <option key={k} value={k}>
                      {t('owner.' + k)}
                    </option>
                  ))}
                </select>
              </Field>
              {(['title', 'slug', 'summary'] as const).map((k) => (
                <Field key={k} label={t('owner.' + k)}>
                  <input
                    value={form[k] || ''}
                    maxLength={k === 'summary' ? 500 : k === 'slug' ? 160 : 200}
                    onChange={(e) => {
                      const value = e.target.value;
                      if (k === 'slug') {
                        collectionSlugManual.current = true;
                      }
                      setForm({
                        ...form,
                        [k]: value,
                        ...(k === 'title'
                          ? {
                              slug: titleSlugChange(
                                form.title,
                                form.slug,
                                value,
                                collectionSlugManual.current,
                              ),
                            }
                          : {}),
                      });
                    }}
                  />
                </Field>
              ))}
              <label>
                <input
                  type="checkbox"
                  checked={form.visible}
                  onChange={(e) => setForm({ ...form, visible: e.target.checked })}
                />
                {t('owner.visible')}
              </label>
              <CollectionContents
                key={key + language + form.kind}
                members={members}
                onChange={(items) => {
                  setMembers(items);
                  setPlanDirty(true);
                }}
                kind={form.kind}
                locale={language}
                collectionKey={key}
                titles={doc?.member_titles}
              />
              <p className="vb-help">{t('collection.saveHelp')}</p>
              {doc && (
                <Button
                  disabled={busy}
                  onClick={() =>
                    void run(async () => {
                      if (!window.confirm(t('collection.removeConfirm'))) {
                        return;
                      }
                      await call(
                        '/collections/' + encodeURIComponent(key),
                        'DELETE',
                        { expected_version: doc.version },
                        csrf,
                      );
                      setDoc(undefined);
                      setKey('');
                      setMembers([]);
                      list.retry();
                      setNotice('collection.removed');
                    })
                  }
                >
                  {t('collection.remove')}
                </Button>
              )}
              <Button
                variant="primary"
                disabled={!key || !form.title || !form.slug}
                onClick={() =>
                  void run(async () => {
                    if (
                      (members.some((m) => m.kind === 'article') ||
                        (doc?.active_plan_id &&
                          (planDirty || doc.draft_plan_id !== doc.active_plan_id))) &&
                      !window.confirm(t('collection.confirm'))
                    ) {
                      return;
                    }
                    const payload = {
                      ...form,
                      expected_version: doc?.version || 0,
                      members: members.map((m) => ({
                        key: m.key,
                        announcements: m.announcements,
                        ...(m.expected_version ? { expected_version: m.expected_version } : {}),
                      })),
                    };
                    const signature = JSON.stringify({ key, language, payload });
                    if (retry.current?.signature !== signature) {
                      retry.current = { signature, id: crypto.randomUUID() };
                    }
                    await call(
                      '/collections/' + encodeURIComponent(key) + '/workspace?locale=' + language,
                      'PUT',
                      payload,
                      csrf,
                      { 'Idempotency-Key': retry.current.id },
                    );
                    await open(key);
                    list.retry();
                    setNotice('collection.saved');
                  })
                }
              >
                {t('collection.save')}
              </Button>
              {doc && (
                <details>
                  <summary>{t('collection.advanced')}</summary>
                  {doc.kind === 'learning' && (
                    <Button
                      onClick={() => {
                        if (!window.confirm(t('progress.resetPathConfirm'))) {
                          return;
                        }
                        void run(async () => {
                          await call(
                            '/collections/' + key + '/progress-reset',
                            'POST',
                            { expected_version: doc.version },
                            csrf,
                          );
                          await open(key);
                          setNotice('progress.pathReset');
                        });
                      }}
                    >
                      {t('progress.resetPath')}
                    </Button>
                  )}
                  {doc.draft_gate_policy > 0 && <p>{t('builder.gateHelp')}</p>}
                  <p>{t('owner.version', { n: doc.version })}</p>
                  <p className="vb-help">{t('owner.versionHelp')}</p>
                  <p>{t('owner.announcementHelp')}</p>
                  <Button
                    onClick={() =>
                      void run(async () => {
                        await call(
                          `/collections/${key}/plan`,
                          'PUT',
                          {
                            expected_version: doc.version,
                            members: members.map((m) => ({
                              key: m.key,
                              announcements: m.announcements,
                            })),
                          },
                          csrf,
                        );
                        await open(key);
                        setNotice('owner.planSaved');
                      })
                    }
                  >
                    {t('owner.savePlan')}
                  </Button>
                  <Button
                    disabled={planDirty || !doc.draft_plan_id}
                    onClick={() =>
                      void run(async () => {
                        if (
                          !window.confirm(
                            t('progress.planImpact', {
                              path: key,
                              lessons: doc.draft_members.map((m) => m.key).join(', '),
                            }),
                          )
                        ) {
                          return;
                        }
                        await call(
                          `/collections/${key}/apply`,
                          'POST',
                          { expected_version: doc.version, plan_id: doc.draft_plan_id },
                          csrf,
                        );
                        await open(key);
                        setNotice('owner.planApplied');
                      })
                    }
                  >
                    {t('owner.applyPlan')}
                  </Button>
                </details>
              )}
            </>
          )}
        </fieldset>
      </div>
    </>
  );
}
