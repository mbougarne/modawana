import { TaxonomyCreate } from '../../../features/owner/TaxonomyCreate';
import { titleSlugChange } from '../../../features/content/slug';
import { publishEditorDraft } from './publishDraft';
import { ReferencePicker } from '../../../features/owner/ReferencePicker';
import { StableKeyField } from '@ui/components/StableKeyField';
import { EditorTabs } from '../../../../../ui/src/editor/EditorTabs';
import { EditorTags } from '../../../../../ui/src/editor/EditorTags';
import { QuizBuilder } from './QuizBuilder';
import { validQuiz } from '../../../api/learning';
import type { QuizPath } from '../../../api/learning';
import { useEffect, useRef, useState } from 'react';
import { useNavigate, useParams, useSearchParams } from 'react-router-dom';
import { Button, Field, I18nProvider, MarkdownToolbar, useI18n, type MarkdownAction } from '@ui';
import { markdownEdit, editorStatistics } from '@ui/editor/markdown';
import { call, AuthError, requestErrorKey } from '../../../features/auth/api';
import { MarkdownReader } from '../../../features/markdown/MarkdownReader';
import { MediaLibrary, type UsedMedia } from '../../../features/owner/MediaLibrary';
import type {
  Draft,
  DraftInput,
  Locale,
  RevisionResult,
  MediaPresentation,
  TaxonomyTerm,
  Revision,
  CollectionDocument,
} from '../../../api/content';
import { useResource } from '../../public/api';

const empty = (locale: Locale): DraftInput => ({
  expected_version: 0,
  kind: 'article',
  source_locale: locale,
  slug: '',
  title: '',
  summary: '',
  markdown: '',
  tag_keys: [],
  media_presentations: [],
});
function fromDraft(d: Draft): DraftInput {
  return {
    collection_key: d.collection_key || undefined,
    position: d.collection_key ? d.position : undefined,
    quiz: d.revision.quiz,
    expected_version: d.version,
    kind: d.kind,
    source_locale: d.source_locale,
    slug: d.revision.slug,
    title: d.revision.title,
    summary: d.revision.summary,
    markdown: d.revision.markdown,
    source_revision_id: d.revision.source_revision_id,
    category_key: d.revision.category_key,
    tag_keys: d.revision.tag_keys,
    cover_media_id: d.revision.cover_media_id,
    media_presentations: d.revision.media_presentations,
  };
}

export function EditorPage({ csrf }: { csrf: string }) {
  const { t, locale, date } = useI18n();
  const params = useParams();
  const [query] = useSearchParams();
  const navigate = useNavigate();
  const lang = (params.contentLocale || locale) as Locale;
  const [key, setKey] = useState(params.key || '');
  const [base, setBase] = useState<Draft>();
  const [working, setWorking] = useState<DraftInput>(() => empty(lang));
  const [initial, setInitial] = useState(() => JSON.stringify(empty(lang)));
  const [pendingFile, setPendingFile] = useState<File>();
  const [tab, setTab] = useState('write');
  const [split, setSplit] = useState(false);
  const [narrow, setNarrow] = useState(() => window.matchMedia('(max-width: 960px)').matches);
  useEffect(() => {
    const query = window.matchMedia('(max-width: 960px)');
    const resize = () => {
      setNarrow(query.matches);
      if (!query.matches) {
        setTab((current) => (current === 'details' ? 'write' : current));
      }
    };
    query.addEventListener('change', resize);
    return () => query.removeEventListener('change', resize);
  }, []);
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState('');
  const [ready, setReady] = useState(!params.key);
  const [media, setMedia] = useState<UsedMedia[]>([]);
  const [selected, setSelected] = useState<UsedMedia>();
  const [alt, setAlt] = useState('');
  const [history, setHistory] = useState<
    { revision_id: string; title: string; created_at: string }[]
  >([]);
  const [nextHistory, setNextHistory] = useState('');
  const slugManual = useRef(false);
  const target = useRef<HTMLTextAreaElement>(null);
  const selection = useRef({ start: 0, end: 0 });
  const retry = useRef<{ signature: string; id: string } | undefined>(undefined);
  const loadGeneration = useRef(0);
  const savedNavigation = useRef<{ key: string; lang: string; revision: string } | undefined>(
    undefined,
  );
  const terms = useResource<{ items: TaxonomyTerm[] }>('/api/v1/taxonomy?locale=' + lang);
  const [createdTerms, setCreatedTerms] = useState<TaxonomyTerm[]>([]);
  useEffect(() => {
    setCreatedTerms([]);
  }, [lang]);
  const allTerms = [...(terms.data?.items || []), ...createdTerms];
  const dirty = JSON.stringify(working) !== initial;
  function report(e: unknown) {
    const code =
      e instanceof AuthError
        ? e.fields.find((field) => field.code.startsWith('conversion_'))?.code
        : undefined;
    if (
      code &&
      [
        'conversion_kind',
        'conversion_about',
        'conversion_plan',
        'conversion_quiz',
        'conversion_collection',
        'conversion_position',
        'conversion_article',
        'conversion_owner',
      ].includes(code)
    ) {
      setNotice('conversion.' + code);
      return;
    }
    setNotice(
      e instanceof AuthError && e.code === 'revision_conflict'
        ? 'config.conflict'
        : e instanceof AuthError && e.code === 'translation_review_required'
          ? 'owner.reviewRequired'
          : requestErrorKey(e),
    );
  }
  function install(d: Draft) {
    slugManual.current = !!d.revision.slug;
    setBase(d);
    const form = fromDraft(d);
    setWorking(form);
    setInitial(JSON.stringify(form));
    setMedia(d.revision.media);
    setReady(true);
    retry.current = undefined;
  }
  async function load() {
    const generation = ++loadGeneration.current;
    const saved = savedNavigation.current;
    savedNavigation.current = undefined;
    const current = () => generation === loadGeneration.current;
    setReady(false);
    setBusy(true);
    setNotice('');
    setHistory([]);
    setNextHistory('');
    setSelected(undefined);
    setPendingFile(undefined);
    try {
      if (params.key) {
        try {
          const draft = await call<Draft>(`/content/${params.key}/${lang}`);
          if (current()) {
            setKey(params.key);
            install(draft);
            if (
              saved?.key === params.key &&
              saved.lang === lang &&
              saved.revision === draft.revision.revision_id
            ) {
              setNotice('owner.saved');
            }
          }
        } catch (e) {
          if (!current()) {
            return;
          }
          const source = query.get('source');
          if (
            e instanceof AuthError &&
            e.code === 'not_found' &&
            source &&
            ['en', 'ar', 'fr'].includes(source) &&
            source !== lang
          ) {
            const d = await call<Draft>(`/content/${params.key}/${source}`);
            if (!current()) {
              return;
            }
            if (!d.published_revision_id) {
              throw e;
            }
            const published = await call<Revision>(
              `/content/${params.key}/${source}/revisions/${d.published_revision_id}`,
            );
            if (!current()) {
              return;
            }
            const quiz = published.quiz
              ? {
                  ...published.quiz,
                  questions: published.quiz.questions.map((q) => ({
                    ...q,
                    text: '',
                    explanation: '',
                    options: q.options.map((o) => ({ ...o, text: '' })),
                  })),
                }
              : undefined;
            const form = {
              quiz,
              ...empty(lang),
              kind: d.kind,
              source_locale: d.source_locale,
              source_revision_id: d.published_revision_id,
            };
            slugManual.current = false;
            setBase(undefined);
            setKey(params.key);
            setMedia([]);
            setWorking(form);
            setInitial(JSON.stringify(form));
            setReady(true);
          } else {
            throw e;
          }
        }
      } else {
        slugManual.current = false;
        const form = empty(lang);
        setWorking(form);
        setInitial(JSON.stringify(form));
        setKey('');
        setBase(undefined);
        setMedia([]);
        setReady(true);
      }
    } catch (e) {
      if (current()) {
        report(e);
      }
    } finally {
      if (current()) {
        setBusy(false);
      }
    }
  }
  useEffect(() => {
    void load();
    return () => {
      loadGeneration.current++;
    };
  }, [params.key, lang, query.get('source')]);
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
  function change<K extends keyof DraftInput>(field: K, value: DraftInput[K]) {
    setWorking((w) => ({ ...w, [field]: value }));
    setNotice('config.unsaved');
  }
  async function run(fn: () => Promise<void>) {
    if (busy) {
      return;
    }
    setBusy(true);
    setNotice('');
    try {
      await fn();
    } catch (e) {
      report(e);
    } finally {
      setBusy(false);
    }
  }
  async function refreshSource() {
    if (!params.key || !window.confirm(t('builder.refreshConfirm'))) {
      return;
    }
    const source = await call<Draft>(`/content/${params.key}/${working.source_locale}`);
    if (!source.published_revision_id) {
      throw new AuthError('not_found');
    }
    const revision = await call<Revision>(
      `/content/${params.key}/${working.source_locale}/revisions/${source.published_revision_id}`,
    );
    setWorking((w) => ({
      ...w,
      source_revision_id: source.published_revision_id,
      quiz: revision.quiz
        ? {
            ...revision.quiz,
            questions: revision.quiz.questions.map((q) => {
              const old = w.quiz?.questions.find((previous) => previous.id === q.id);
              return {
                ...q,
                text: old?.text || '',
                explanation: old?.explanation || '',
                options: q.options.map((option) => ({
                  ...option,
                  text: old?.options.find((previous) => previous.id === option.id)?.text || '',
                })),
              };
            }),
          }
        : undefined,
    }));
    setNotice('builder.sourceLoaded');
  }
  async function save(navigateAfterSave = true): Promise<Draft | undefined> {
    const payload = structuredClone(working);
    if (payload.kind !== 'article' && !payload.collection_key) {
      setNotice('editor.pathRequired');
      return;
    }
    if (
      base &&
      payload.kind !== base.kind &&
      !window.confirm(t('conversion.confirm', { kind: t('owner.' + payload.kind) }))
    ) {
      return;
    }
    if (
      base &&
      payload.kind !== base.kind &&
      payload.kind !== 'article' &&
      payload.expected_collection_version === undefined
    ) {
      throw new AuthError('invalid_fields', [
        { path: 'collection_key', code: 'conversion_collection' },
      ]);
    }
    const signature = JSON.stringify({ key, lang, payload });
    if (retry.current?.signature !== signature) {
      retry.current = { signature, id: crypto.randomUUID() };
    }
    await call<RevisionResult>(
      `/content/${encodeURIComponent(key)}/${lang}`,
      'PUT',
      payload,
      csrf,
      { 'Idempotency-Key': retry.current.id },
    );
    const saved = await call<Draft>(`/content/${encodeURIComponent(key)}/${lang}`);
    install(saved);
    setNotice('owner.saved');
    if (navigateAfterSave && !params.key) {
      savedNavigation.current = { key, lang, revision: saved.revision.revision_id };
      navigate(`/${locale}/admin/editor/${key}/${lang}`, { replace: true });
    }
    return saved;
  }
  async function action(op: string, withPlan = false) {
    if (op === 'publish') {
      let savedDuringAction = false;
      try {
        const published = await publishEditorDraft({
          key,
          locale: lang,
          draft: base,
          dirty,
          csrf,
          withPlan,
          save: async () => {
            const saved = await save(false);
            savedDuringAction = !!saved;
            return saved;
          },
          confirmPlan: (collection) =>
            window.confirm(
              t('progress.planImpact', {
                path: collection.key,
                lessons: [
                  ...new Set([
                    ...(collection.draft_plan_id
                      ? collection.draft_members
                      : collection.assigned_members
                    ).map((m) => m.key),
                    key,
                  ]),
                ].join(', '),
              }),
            ),
        });
        if (published) {
          install(published);
          setNotice(published.kind === 'lesson' ? 'editor.lessonPublished' : 'owner.publishDone');
        }
      } finally {
        if (!params.key && (savedDuringAction || base)) {
          navigate(`/${locale}/admin/editor/${key}/${lang}`, { replace: true });
        }
      }
      return;
    }
    if (!base || dirty) {
      return;
    }
    if (op === 'unpublish' && base.kind === 'lesson') {
      if (!base.collection_key) {
        setNotice('editor.pathRequired');
        return;
      }
      const path = await call<QuizPath>(`/content/${key}/${lang}/quiz-gate`);
      const collection = await call<CollectionDocument>('/collections/' + path.key);
      if (
        !window.confirm(
          t('progress.planImpact', {
            path: path.key,
            lessons: [
              ...new Set([
                ...(collection.draft_plan_id
                  ? collection.draft_members
                  : collection.assigned_members
                ).map((m) => m.key),
                key,
              ]),
            ].join(', '),
          }),
        )
      ) {
        return;
      }
    }
    await call(
      `/content/${key}/${lang}/${op}`,
      'POST',
      {
        expected_version: base.version,
        revision_id: op === 'unpublish' ? base.published_revision_id : base.revision.revision_id,
        ...(op === 'review' ? { source_revision_id: base.revision.source_revision_id } : {}),
      },
      csrf,
    );
    install(await call<Draft>(`/content/${key}/${lang}`));
    setNotice(
      op === 'publish' && base.kind === 'lesson'
        ? 'editor.lessonPublished'
        : 'owner.' + op + 'Done',
    );
  }
  async function loadHistory(cursor = '') {
    const r = await call<{ items: typeof history; next_cursor: string }>(
      `/content/${key}/${lang}/revisions?cursor=${cursor}`,
    );
    setHistory((old) => (cursor ? [...old, ...r.items] : r.items));
    setNextHistory(r.next_cursor);
  }
  async function restore(id: string) {
    if (!base || !window.confirm(t('owner.confirmRestore'))) {
      return;
    }
    await call(
      `/content/${key}/${lang}/restore`,
      'POST',
      { expected_version: base.version, revision_id: id },
      csrf,
    );
    install(await call<Draft>(`/content/${key}/${lang}`));
    setNotice('owner.restored');
  }
  function download() {
    const url = URL.createObjectURL(
      new Blob([JSON.stringify({ key, locale: lang, draft: working }, null, 2)], {
        type: 'application/json',
      }),
    );
    const a = document.createElement('a');
    a.href = url;
    a.download = (key || 'draft') + '-' + lang + '.json';
    a.click();
    URL.revokeObjectURL(url);
  }
  function format(action: MarkdownAction) {
    const el = target.current;
    if (!el) {
      return;
    }
    const edit = markdownEdit(working.markdown, el.selectionStart, el.selectionEnd, action);
    change('markdown', edit.value);
    selection.current = { start: edit.selectionStart, end: edit.selectionEnd };
    requestAnimationFrame(() => {
      el.focus();
      el.setSelectionRange(edit.selectionStart, edit.selectionEnd);
    });
  }
  function insert() {
    if (!selected || !alt.trim()) {
      return;
    }
    const label = alt.replace(/[[\]\\\n]/g, ' ');
    const markdown =
      (selected.kind === 'image' ? '!' : '') + '[' + label + '](' + selected.url + ')';
    const { start, end } = selection.current;
    change('markdown', working.markdown.slice(0, start) + markdown + working.markdown.slice(end));
    selection.current = { start: start + markdown.length, end: start + markdown.length };
    setSelected(undefined);
    setAlt('');
    setTab('write');
  }
  function pick(m: UsedMedia) {
    setMedia((old) => (old.some((x) => x.id === m.id) ? old : [...old, m]));
    setSelected(m);
    setAlt('');
  }
  function presentation(id: string, field: keyof MediaPresentation, value: string) {
    const list = working.media_presentations || [];
    const old = list.find((p) => p.media_id === id) || {
      media_id: id,
      caption: '',
      transcript: '',
    };
    change('media_presentations', [
      ...list.filter((p) => p.media_id !== id),
      { ...old, [field]: value },
    ]);
  }
  const toolbar = Object.fromEntries(
    [
      'h2',
      'h3',
      'bold',
      'italic',
      'code',
      'link',
      'quote',
      'ul',
      'ol',
      'codeblock',
      'table',
      'hr',
      'image',
      'video',
      'split',
    ].map((k) => [k, t('toolbar.' + k)]),
  ) as Record<MarkdownAction | 'image' | 'video' | 'split', string>;
  const stateLabel = busy
    ? 'auth.busy'
    : dirty
      ? 'config.unsaved'
      : base?.published_revision_id
        ? base.published_revision_id === base.revision.revision_id
          ? 'owner.published'
          : 'owner.changed'
        : 'owner.draft';
  const statistics = editorStatistics(working.markdown);
  return (
    <>
      <h1 className="vb-visually-hidden">{t('owner.editor')}</h1>
      {notice && (
        <p
          role="status"
          className="workspace-alert"
          data-tone={
            notice === 'editor.pathRequired' ||
            notice.startsWith('request.') ||
            notice === 'owner.failed' ||
            notice === 'config.conflict' ||
            notice === 'owner.reviewRequired' ||
            notice.startsWith('conversion.conversion_')
              ? 'error'
              : notice === 'config.unsaved'
                ? 'warning'
                : 'info'
          }
        >
          {t(notice)}
        </p>
      )}
      {!ready && <Button onClick={() => void load()}>{t('public.retry')}</Button>}
      {ready && (
        <>
          <fieldset
            disabled={busy}
            className="owner-fields content-editor"
            onDragOver={(e) => e.preventDefault()}
            onDrop={(e) => {
              const file = e.dataTransfer.files[0];
              if (file) {
                e.preventDefault();
                setPendingFile(file);
                setTab('media');
              }
            }}
            onPaste={(e) => {
              const file = e.clipboardData.files[0];
              if (file) {
                e.preventDefault();
                setPendingFile(file);
                setTab('media');
              }
            }}
          >
            <div className="vb-editor editor-layout" data-tab={tab}>
              <div className="vb-paper editor-paper">
                <textarea
                  rows={2}
                  className="vb-titlein editor-title"
                  aria-label={t('owner.title')}
                  placeholder={t('editor.titlePlaceholder')}
                  lang={lang}
                  dir="auto"
                  value={working.title}
                  maxLength={200}
                  onChange={(e) => {
                    const title = e.target.value;
                    setWorking((w) => ({
                      ...w,
                      title,
                      slug: titleSlugChange(
                        w.title,
                        w.slug,
                        title,
                        slugManual.current,
                        !!base?.slug_locked,
                      ),
                    }));
                  }}
                />
                <textarea
                  className="vb-summaryin editor-summary"
                  aria-label={t('owner.summary')}
                  placeholder={t('editor.summaryPlaceholder')}
                  lang={lang}
                  dir="auto"
                  rows={2}
                  maxLength={500}
                  value={working.summary || ''}
                  onChange={(e) => change('summary', e.target.value)}
                />
                <EditorTabs
                  current={tab}
                  onChange={setTab}
                  tabs={[
                    { key: 'write', label: t('owner.write') },
                    { key: 'preview', label: t('owner.preview') },
                    { key: 'media', label: t('owner.media'), count: media.length },
                    ...(working.kind === 'doc'
                      ? []
                      : [
                          {
                            key: 'quiz',
                            label: t('owner.quiz'),
                            count: working.quiz?.questions.length || 0,
                          },
                        ]),
                    ...(narrow ? [{ key: 'details', label: t('owner.details') }] : []),
                  ]}
                />
                <div
                  className="vb-paper-body editor-paper-body"
                  hidden={narrow && tab === 'details'}
                >
                  <section
                    id="panel-write"
                    role="tabpanel"
                    aria-labelledby="tab-write"
                    hidden={tab !== 'write'}
                  >
                    <div className="vb-panes editor-panes" data-split={split}>
                      <div>
                        <div>
                          <MarkdownToolbar
                            onEdit={format}
                            target={target}
                            labels={toolbar}
                            label={t('editor.formatting')}
                            onUpload={() => setTab('media')}
                            split={split}
                            onToggleSplit={() => setSplit((v) => !v)}
                          />
                        </div>
                        <textarea
                          ref={target}
                          className="vb-md owner-markdown editor-markdown"
                          aria-label={t('owner.markdown')}
                          lang={lang}
                          dir="auto"
                          value={working.markdown}
                          onSelect={(e) => {
                            selection.current = {
                              start: e.currentTarget.selectionStart,
                              end: e.currentTarget.selectionEnd,
                            };
                          }}
                          onChange={(e) => change('markdown', e.target.value)}
                        />
                      </div>
                      {split && (
                        <I18nProvider locale={lang} syncDocument={false}>
                          <div className="vb-pane-preview editor-reading-pane">
                            <MarkdownReader
                              markdown={working.markdown}
                              media={media}
                              presentations={working.media_presentations}
                            />
                          </div>
                        </I18nProvider>
                      )}
                    </div>
                  </section>
                  <section
                    id="panel-preview"
                    role="tabpanel"
                    aria-labelledby="tab-preview"
                    hidden={tab !== 'preview'}
                  >
                    <I18nProvider locale={lang} syncDocument={false}>
                      <MarkdownReader
                        markdown={working.markdown}
                        media={media}
                        presentations={working.media_presentations}
                      />
                    </I18nProvider>
                  </section>
                  <section
                    id="panel-media"
                    role="tabpanel"
                    aria-labelledby="tab-media"
                    hidden={tab !== 'media'}
                  >
                    <MediaLibrary
                      csrf={csrf}
                      onSelect={pick}
                      pendingFile={pendingFile}
                      onPendingFileHandled={() => setPendingFile(undefined)}
                    />
                    {selected && (
                      <div className="owner-panel">
                        <h3>{selected.filename}</h3>
                        <Field label={t('owner.mediaText')}>
                          <input
                            value={alt}
                            maxLength={300}
                            onChange={(e) => setAlt(e.target.value)}
                          />
                        </Field>
                        <Button disabled={!alt.trim()} onClick={insert}>
                          {t('owner.insert')}
                        </Button>
                        {selected.kind === 'image' && (
                          <Button onClick={() => change('cover_media_id', selected.id)}>
                            {t('owner.cover')}
                          </Button>
                        )}
                      </div>
                    )}
                    {media
                      .filter((m) => m.kind === 'video' && working.markdown.includes(m.url))
                      .map((m) => (
                        <fieldset key={m.id}>
                          <legend>{m.filename}</legend>
                          {(['caption', 'transcript'] as const).map((k) => (
                            <Field key={k} label={t('owner.' + k)}>
                              <input
                                value={
                                  working.media_presentations?.find((p) => p.media_id === m.id)?.[
                                    k
                                  ] || ''
                                }
                                onChange={(e) => presentation(m.id, k, e.target.value)}
                              />
                            </Field>
                          ))}
                          {(['poster_media_id', 'caption_track_media_id'] as const).map((k) => (
                            <Field key={k} label={t('owner.' + k)}>
                              <select
                                value={
                                  working.media_presentations?.find((p) => p.media_id === m.id)?.[
                                    k
                                  ] || ''
                                }
                                onChange={(e) => presentation(m.id, k, e.target.value)}
                              >
                                <option value="">{t('owner.none')}</option>
                                {media
                                  .filter(
                                    (v) =>
                                      v.kind === (k === 'poster_media_id' ? 'image' : 'captions'),
                                  )
                                  .map((v) => (
                                    <option value={v.id} key={v.id}>
                                      {v.filename}
                                    </option>
                                  ))}
                              </select>
                            </Field>
                          ))}
                        </fieldset>
                      ))}
                  </section>
                  <section
                    id="panel-quiz"
                    role="tabpanel"
                    aria-labelledby="tab-quiz"
                    hidden={tab !== 'quiz'}
                  >
                    {working.kind !== 'doc' && (
                      <QuizBuilder
                        value={working.quiz}
                        onChange={(v) => change('quiz', v)}
                        base={base}
                        dirty={dirty}
                        csrf={csrf}
                        contentLocale={lang}
                        translation={working.source_locale !== lang}
                        onRefreshSource={() => void run(refreshSource)}
                        onSave={() => save(false)}
                        contentKind={working.kind}
                        onBusyChange={setBusy}
                        onPublishPlan={() => action('publish', true)}
                      />
                    )}
                  </section>
                </div>
                <div className="vb-status editor-status">
                  <span>
                    {t('editor.words', { n: statistics.words })} ·{' '}
                    {t('editor.minutes', { n: statistics.minutes })}
                  </span>
                  <span
                    className="vb-state"
                    data-s={
                      !base
                        ? 'new'
                        : dirty
                          ? 'dirty'
                          : base.published_revision_id
                            ? 'live'
                            : 'saved'
                    }
                  >
                    {t(!base ? 'editor.notSaved' : dirty ? 'config.unsaved' : 'owner.saved')}
                  </span>
                </div>
              </div>
              <aside className="editor-sidebar" aria-label={t('owner.details')}>
                <section className="owner-panel editor-publish-card">
                  <h2>{t('owner.publish')}</h2>
                  <span className="workspace-status">{t(stateLabel)}</span>
                  <p className="vb-help">{t('editor.publishHelp')}</p>
                  {working.kind === 'lesson' && (
                    <p className="vb-help">{t('editor.pathPublishHelp')}</p>
                  )}
                  <div className="editor-publish-actions">
                    <Button
                      variant="primary"
                      disabled={
                        busy ||
                        !key ||
                        !working.title ||
                        (!!working.quiz && !validQuiz(working.quiz))
                      }
                      onClick={() =>
                        void run(async () => {
                          await save();
                        })
                      }
                    >
                      {t('owner.save')}
                    </Button>
                    <Button
                      disabled={
                        busy ||
                        !key ||
                        !working.title ||
                        (!!working.quiz && !validQuiz(working.quiz))
                      }
                      onClick={() => void run(() => action('publish'))}
                    >
                      {t(dirty || !base ? 'editor.savePublish' : 'owner.publish')}
                    </Button>
                    {working.kind === 'lesson' && (
                      <Button
                        disabled={
                          busy ||
                          !key ||
                          !working.title ||
                          (!!working.quiz && !validQuiz(working.quiz))
                        }
                        onClick={() => void run(() => action('publish', true))}
                      >
                        {t('progress.publishPlan')}
                      </Button>
                    )}
                    {base && (
                      <Button
                        disabled={busy || dirty}
                        onClick={() => {
                          setNotice('');
                          setTab('write');
                          setHistory([]);
                          setNextHistory('');
                          setSelected(undefined);
                          setPendingFile(undefined);
                          retry.current = undefined;
                          navigate('/' + locale + '/admin/new');
                        }}
                      >
                        {t('owner.new')}
                      </Button>
                    )}
                    {base?.source_locale !== lang && base && (
                      <Button
                        disabled={busy || dirty}
                        onClick={() => void run(() => action('review'))}
                      >
                        {t('owner.review')}
                      </Button>
                    )}
                    {base?.published_revision_id && (
                      <Button
                        disabled={busy || dirty}
                        onClick={() => void run(() => action('unpublish'))}
                      >
                        {t('owner.unpublish')}
                      </Button>
                    )}
                  </div>
                </section>
                <div
                  id="panel-details"
                  role={narrow ? 'tabpanel' : 'region'}
                  aria-label={t('owner.details')}
                  aria-labelledby={narrow ? 'tab-details' : undefined}
                  hidden={narrow && tab !== 'details'}
                  className="editor-metadata"
                >
                  <section className="owner-panel">
                    <h2>{t('owner.kind')}</h2>
                    <p className="vb-meta">
                      {t('owner.locale')}: {t('language.' + lang)}
                    </p>
                    <div className="owner-row">
                      <StableKeyField value={key} disabled={!!params.key} onChange={setKey} />
                      <Field label={t('owner.kind')}>
                        <select
                          value={working.kind}
                          onChange={(e) => {
                            const kind = e.target.value as DraftInput['kind'];
                            if (kind === 'doc' && tab === 'quiz') {
                              setTab('write');
                            }
                            setWorking((w) => ({
                              ...w,
                              kind,
                              position:
                                kind === 'article'
                                  ? undefined
                                  : kind === base?.kind
                                    ? base.position
                                    : 0,
                              expected_collection_version: undefined,
                              collection_key:
                                kind === base?.kind ? base.collection_key || undefined : undefined,
                            }));
                          }}
                        >
                          {['article', 'lesson', 'doc'].map((k) => (
                            <option key={k} value={k} disabled={k === 'doc' && !!working.quiz}>
                              {t('owner.' + k)}
                            </option>
                          ))}
                        </select>
                      </Field>
                    </div>
                    {base && working.kind !== base.kind && (
                      <p className="vb-help">{t('conversion.inlineHelp')}</p>
                    )}
                  </section>
                  <section className="owner-panel editor-details-card">
                    <h2>{t('owner.details')}</h2>
                    <Field label={t('owner.slug')}>
                      <input
                        value={working.slug}
                        disabled={!!base?.slug_locked}
                        onChange={(e) => {
                          slugManual.current = true;
                          change('slug', e.target.value);
                        }}
                      />
                    </Field>
                    <Field label={t('owner.category')}>
                      <select
                        value={working.category_key || ''}
                        onChange={(e) => change('category_key', e.target.value)}
                      >
                        <option value="">{t('owner.none')}</option>
                        {allTerms
                          .filter((v) => v.kind === 'category')
                          .map((v) => (
                            <option key={v.key} value={v.key}>
                              {v.label}
                            </option>
                          ))}
                      </select>
                    </Field>
                    <TaxonomyCreate
                      kind="category"
                      locale={lang}
                      csrf={csrf}
                      disabled={busy}
                      onCreated={(term) => {
                        setCreatedTerms((items) => [...items, term]);
                        change('category_key', term.key);
                      }}
                    />
                    <section className="editor-tag-card">
                      <h3>{t('public.tags')}</h3>
                      <EditorTags
                        value={working.tag_keys || []}
                        terms={allTerms.filter((term) => term.kind === 'tag')}
                        onChange={(keys) => change('tag_keys', keys)}
                      />
                      <TaxonomyCreate
                        kind="tag"
                        locale={lang}
                        csrf={csrf}
                        disabled={busy || (working.tag_keys?.length || 0) >= 8}
                        onCreated={(term) => {
                          setCreatedTerms((items) => [...items, term]);
                          setWorking((current) => ({
                            ...current,
                            tag_keys: [...new Set([...(current.tag_keys || []), term.key])].slice(
                              0,
                              8,
                            ),
                          }));
                          setNotice('config.unsaved');
                        }}
                      />
                    </section>
                    <Field label={t('owner.coverID')}>
                      <input
                        value={working.cover_media_id || ''}
                        onChange={(e) => change('cover_media_id', e.target.value)}
                      />
                    </Field>
                    {working.kind !== 'article' && (
                      <>
                        <ReferencePicker
                          label={t('owner.collectionKey')}
                          value={working.collection_key || ''}
                          disabled={!!base?.collection_key && working.kind === base.kind}
                          kind={working.kind === 'lesson' ? 'learning' : 'docs'}
                          locale={lang}
                          onChange={(key, choice) =>
                            setWorking((w) => ({
                              ...w,
                              collection_key: key,
                              expected_collection_version: choice?.version,
                            }))
                          }
                        />
                        <p className="vb-help">
                          {t(
                            base?.collection_key && working.kind === base.kind
                              ? 'editor.pathAssigned'
                              : 'editor.pathHelp',
                          )}
                        </p>
                        <Field label={t('owner.position')}>
                          <input
                            type="number"
                            min={0}
                            disabled={!!base?.collection_key && working.kind === base.kind}
                            max={500}
                            value={working.position ?? 0}
                            onChange={(e) => change('position', Number(e.target.value))}
                          />
                        </Field>
                      </>
                    )}
                    {working.source_locale !== lang && (
                      <Field label={t('owner.sourceRevision')}>
                        <input
                          value={working.source_revision_id || ''}
                          onChange={(e) => change('source_revision_id', e.target.value)}
                        />
                      </Field>
                    )}
                    {base && (
                      <nav aria-label={t('public.translations')}>
                        {(['en', 'ar', 'fr'] as const).map((l) => (
                          <a
                            key={l}
                            href={`/${locale}/admin/editor/${key}/${l}?source=${base.source_locale}`}
                          >
                            {t('language.' + l)}
                          </a>
                        ))}
                      </nav>
                    )}
                  </section>
                </div>
                <section className="owner-panel editor-utilities">
                  <h2>{t('editor.draftTools')}</h2>
                  <Button onClick={download}>{t('owner.exportDraft')}</Button>
                  <Button
                    disabled={busy}
                    onClick={() => {
                      if (!dirty || window.confirm(t('owner.confirmDiscard'))) {
                        void load();
                      }
                    }}
                  >
                    {t('config.reload')}
                  </Button>
                  {base && (
                    <Button disabled={busy} onClick={() => void run(() => loadHistory())}>
                      {t('owner.history')}
                    </Button>
                  )}
                </section>
              </aside>
            </div>
          </fieldset>
          {!!history.length && (
            <section className="owner-panel editor-history">
              <h2>{t('owner.history')}</h2>
              <ol>
                {history.map((h) => (
                  <li key={h.revision_id}>
                    <span>
                      {h.title} · {date(h.created_at, { dateStyle: 'medium' })}
                    </span>
                    <Button disabled={busy} onClick={() => void run(() => restore(h.revision_id))}>
                      {t('owner.restore')}
                    </Button>
                  </li>
                ))}
              </ol>
              {nextHistory && (
                <Button onClick={() => void run(() => loadHistory(nextHistory))}>
                  {t('public.more')}
                </Button>
              )}
            </section>
          )}
        </>
      )}
    </>
  );
}
