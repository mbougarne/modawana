import { useEffect, useRef, useState } from 'react';
import {
  Button,
  Icon,
  IconButton,
  Field,
  LessonQuiz,
  QuizError,
  useI18n,
  type QuizForReader,
  type QuizResult,
  type QuestionType,
} from '@ui';
import { call, AuthError, requestErrorKey } from '../../../features/auth/api';
import type { CollectionDocument, Draft } from '../../../api/content';
import { prepareLessonPlan } from './lessonPlan';
import {
  validQuiz,
  type QuizDefinition,
  type PrivateQuestion,
  type QuizPath,
} from '../../../api/learning';

export function QuizBuilder({
  value,
  onChange,
  base,
  dirty,
  csrf,
  contentLocale,
  translation,
  onRefreshSource,
  onSave,
  onBusyChange,
  onPublishPlan,
  contentKind,
}: {
  value?: QuizDefinition;
  onChange: (v: QuizDefinition | undefined) => void;
  base?: Draft;
  dirty: boolean;
  csrf: string;
  contentLocale: string;
  translation: boolean;
  onRefreshSource: () => void;
  onSave?: () => Promise<Draft | undefined>;
  onBusyChange?: (busy: boolean) => void;
  onPublishPlan?: () => Promise<void>;
  contentKind?: string;
}) {
  const { t } = useI18n();
  const [notice, setNotice] = useState('');
  const [busy, setBusy] = useState(false);
  const [preview, setPreview] = useState<Draft>();
  const [pathError, setPathError] = useState('');
  const [path, setPath] = useState<QuizPath>();
  const pathGeneration = useRef(0);
  const endpoint = base ? `/content/${base.key}/${base.locale}` : '';
  useEffect(() => {
    setPath(undefined);
    setPathError('');
    let active = true;
    const generation = ++pathGeneration.current;
    if (base?.kind === 'lesson') {
      call<QuizPath>(endpoint + '/quiz-gate').then(
        (v) => {
          if (active && generation === pathGeneration.current) {
            setPath(v);
          }
        },
        (error) => {
          if (active && generation === pathGeneration.current) {
            setPathError(requestErrorKey(error));
          }
        },
      );
    }
    return () => {
      active = false;
    };
  }, [endpoint, base?.version]);
  useEffect(() => {
    if (dirty) {
      setPreview(undefined);
    }
  }, [dirty]);
  useEffect(() => {
    setPreview((current) =>
      current?.revision.revision_id === base?.revision.revision_id ? current : undefined,
    );
  }, [base?.revision.revision_id]);
  function question(type: QuestionType): PrivateQuestion {
    return {
      id: crypto.randomUUID(),
      type,
      text: '',
      options:
        type === 'bool'
          ? [
              { id: 'true', text: t('quiz.true') },
              { id: 'false', text: t('quiz.false') },
            ]
          : [
              { id: crypto.randomUUID(), text: '' },
              { id: crypto.randomUUID(), text: '' },
            ],
      correct: [],
      explanation: '',
    };
  }
  function update(next: QuizDefinition, canonical = false) {
    onChange({
      ...next,
      assessment_id: canonical && !translation ? undefined : next.assessment_id,
    });
    setPreview(undefined);
  }
  function changeQuestion(i: number, q: PrivateQuestion, canonical = false) {
    if (value) {
      update({ ...value, questions: value.questions.map((v, j) => (i === j ? q : v)) }, canonical);
    }
  }
  async function run(fn: () => Promise<void>) {
    setBusy(true);
    onBusyChange?.(true);
    setNotice('');
    try {
      await fn();
    } catch (e) {
      setNotice(
        e instanceof AuthError &&
          (e.code === 'translation_review_required' ||
            e.fields.some((field) => field.code === 'assessment_review'))
          ? 'builder.reviewNeeded'
          : requestErrorKey(e),
      );
    } finally {
      setBusy(false);
      onBusyChange?.(false);
    }
  }
  async function ensureSaved() {
    const draft = dirty || !base ? await onSave?.() : base;
    if (!draft?.revision.quiz) {
      setNotice('builder.saveNeeded');
      return;
    }
    return draft;
  }
  const previewQuiz = preview?.revision.quiz;
  const reader: QuizForReader | undefined =
    previewQuiz && preview
      ? {
          id: previewQuiz.assessment_id!,
          version: { quiz: previewQuiz.assessment_id!, content: preview.revision.revision_id },
          locale: preview.locale,
          passMark: previewQuiz.pass_mark,
          showExplanations: previewQuiz.show_explanations,
          unlocksNext: false,
          questions: previewQuiz.questions.map(({ id, type, text, options }) => ({
            id,
            type,
            text,
            options,
          })),
        }
      : undefined;
  return (
    <section className="vb-qb owner-quiz-builder">
      <header className="vb-qb-head">
        <h2 className="vb-visually-hidden">{t('builder.title')}</h2>
        <label className="vb-switch">
          <input
            type="checkbox"
            role="switch"
            checked={!!value}
            disabled={translation && !value}
            onChange={(event) =>
              onChange(
                event.target.checked
                  ? { pass_mark: 1, show_explanations: true, questions: [question('single')] }
                  : undefined,
              )
            }
          />
          <span aria-hidden="true" />
          {t('builder.addQuiz')}
        </label>
        {translation && <Button onClick={onRefreshSource}>{t('builder.refreshSource')}</Button>}
        <p className="vb-help">{t('builder.practice')}</p>
      </header>
      {!value ? (
        <p className="vb-qb-off">{t('builder.empty')}</p>
      ) : (
        <>
          <div className="vb-qb-settings">
            <Field
              label={t('builder.passMark')}
              help={t('builder.ofQuestions', { n: value.questions.length })}
            >
              <input
                type="number"
                min={1}
                max={value.questions.length}
                value={value.pass_mark}
                disabled={translation}
                onChange={(e) => update({ ...value, pass_mark: Number(e.target.value) }, true)}
              />
            </Field>
            <label className="vb-check">
              <input
                type="checkbox"
                checked={value.show_explanations}
                disabled={translation}
                onChange={(e) => update({ ...value, show_explanations: e.target.checked }, true)}
              />
              {t('builder.explanations')}
            </label>
          </div>
          <div className="quiz-question-list">
            {value.questions.map((q, i) => (
              <fieldset
                className="vb-qcard"
                key={q.id}
                data-invalid={!validQuiz({ ...value, pass_mark: 1, questions: [q] })}
              >
                <legend>{t('builder.question', { n: i + 1 })}</legend>
                <div className="vb-qcard-top">
                  <span className="vb-qnum" aria-hidden="true">
                    {i + 1}
                  </span>
                  <Field label={t('builder.type')}>
                    <select
                      className="vb-select"
                      disabled={translation}
                      value={q.type}
                      onChange={(e) =>
                        changeQuestion(
                          i,
                          {
                            ...question(e.target.value as QuestionType),
                            id: q.id,
                            text: q.text,
                            explanation: q.explanation,
                          },
                          true,
                        )
                      }
                    >
                      {['single', 'multi', 'bool'].map((type) => (
                        <option key={type} value={type}>
                          {t('builder.' + type)}
                        </option>
                      ))}
                    </select>
                  </Field>
                  <div className="quiz-question-actions">
                    <IconButton
                      label={t('owner.up')}
                      disabled={translation || i === 0}
                      onClick={() => {
                        const questions = [...value.questions];
                        [questions[i - 1], questions[i]] = [questions[i], questions[i - 1]];
                        update({ ...value, questions }, true);
                      }}
                    >
                      <Icon name="up" />
                    </IconButton>
                    <IconButton
                      label={t('owner.down')}
                      disabled={translation || i === value.questions.length - 1}
                      onClick={() => {
                        const questions = [...value.questions];
                        [questions[i + 1], questions[i]] = [questions[i], questions[i + 1]];
                        update({ ...value, questions }, true);
                      }}
                    >
                      <Icon name="down" />
                    </IconButton>
                    <IconButton
                      label={t('owner.remove')}
                      disabled={translation}
                      onClick={() =>
                        update(
                          { ...value, questions: value.questions.filter((_, j) => j !== i) },
                          true,
                        )
                      }
                    >
                      <Icon name="trash" />
                    </IconButton>
                  </div>
                </div>
                <Field label={t('builder.prompt')}>
                  <textarea
                    lang={contentLocale}
                    dir="auto"
                    maxLength={2000}
                    value={q.text}
                    onChange={(e) => changeQuestion(i, { ...q, text: e.target.value })}
                  />
                </Field>
                {q.options.map((o, j) => (
                  <div className="quiz-answer-row" key={o.id}>
                    <Field label={t('builder.option', { n: j + 1 })}>
                      <input
                        lang={contentLocale}
                        dir="auto"
                        maxLength={1000}
                        value={o.text}
                        onChange={(e) =>
                          changeQuestion(i, {
                            ...q,
                            options: q.options.map((v, index) =>
                              index === j ? { ...v, text: e.target.value } : v,
                            ),
                          })
                        }
                      />
                    </Field>
                    <label className="quiz-answer-correct">
                      <input
                        type={q.type === 'multi' ? 'checkbox' : 'radio'}
                        name={'correct-' + q.id}
                        checked={q.correct.includes(o.id)}
                        disabled={translation}
                        onChange={(e) =>
                          changeQuestion(
                            i,
                            {
                              ...q,
                              correct:
                                q.type === 'multi'
                                  ? e.target.checked
                                    ? [...q.correct, o.id]
                                    : q.correct.filter((id) => id !== o.id)
                                  : [o.id],
                            },
                            true,
                          )
                        }
                      />
                      <span className="vb-visually-hidden">{t('builder.correct')}</span>
                    </label>
                    {q.type !== 'bool' && (
                      <div className="quiz-answer-actions">
                        <IconButton
                          label={t('owner.up')}
                          disabled={translation || j === 0}
                          onClick={() => {
                            const options = [...q.options];
                            [options[j - 1], options[j]] = [options[j], options[j - 1]];
                            changeQuestion(i, { ...q, options }, true);
                          }}
                        >
                          <Icon name="up" />
                        </IconButton>
                        <IconButton
                          label={t('owner.down')}
                          disabled={translation || j === q.options.length - 1}
                          onClick={() => {
                            const options = [...q.options];
                            [options[j + 1], options[j]] = [options[j], options[j + 1]];
                            changeQuestion(i, { ...q, options }, true);
                          }}
                        >
                          <Icon name="down" />
                        </IconButton>
                        <IconButton
                          label={t('owner.remove')}
                          disabled={translation || q.options.length <= 2}
                          onClick={() =>
                            changeQuestion(
                              i,
                              {
                                ...q,
                                options: q.options.filter((v) => v.id !== o.id),
                                correct: q.correct.filter((id) => id !== o.id),
                              },
                              true,
                            )
                          }
                        >
                          <Icon name="trash" />
                        </IconButton>
                      </div>
                    )}
                  </div>
                ))}
                {q.type !== 'bool' && (
                  <Button
                    disabled={translation || q.options.length >= 8}
                    onClick={() =>
                      changeQuestion(
                        i,
                        { ...q, options: [...q.options, { id: crypto.randomUUID(), text: '' }] },
                        true,
                      )
                    }
                  >
                    {t('builder.addOption')}
                  </Button>
                )}
                <Field label={t('builder.explanation')}>
                  <textarea
                    maxLength={4000}
                    value={q.explanation}
                    lang={contentLocale}
                    dir="auto"
                    onChange={(e) => changeQuestion(i, { ...q, explanation: e.target.value })}
                  />
                </Field>
              </fieldset>
            ))}
          </div>
          <div className="vb-qb-add">
            <span className="vb-meta">{t('builder.addQuestion')}</span>
            {(['single', 'multi', 'bool'] as QuestionType[]).map((type) => (
              <Button
                key={type}
                disabled={translation || value.questions.length >= 20}
                onClick={() =>
                  update({ ...value, questions: [...value.questions, question(type)] }, true)
                }
              >
                + {t('builder.' + type)}
              </Button>
            ))}
          </div>
          {!validQuiz(value) && (
            <p role="alert" className="workspace-alert" data-tone="error">
              {t('builder.invalid')}
            </p>
          )}
          {translation && <p>{t('builder.translation')}</p>}
          <div className="owner-row">
            <Button
              disabled={busy || !validQuiz(value) || ((!base || dirty) && !onSave)}
              onClick={() =>
                void run(async () => {
                  if (preview) {
                    setPreview(undefined);
                    return;
                  }
                  const draft = await ensureSaved();
                  if (draft) {
                    setPreview(draft);
                  }
                })
              }
            >
              {t(dirty || !base ? 'builder.savePreview' : 'builder.preview')}
            </Button>
            <Button
              disabled={busy || !validQuiz(value) || ((!base || dirty) && !onSave)}
              onClick={() =>
                void run(async () => {
                  const draft = await ensureSaved();
                  if (!draft) {
                    return;
                  }
                  await call(
                    `/content/${draft.key}/${draft.locale}/quiz-review`,
                    'POST',
                    { expected_version: draft.version, revision_id: draft.revision.revision_id },
                    csrf,
                  );
                  setNotice('builder.reviewed');
                })
              }
            >
              {t(dirty || !base ? 'builder.saveReview' : 'builder.review')}
            </Button>
          </div>
          <p className="vb-help">{t('builder.actionsHelp')}</p>
          {pathError && (
            <p role="alert" className="workspace-alert" data-tone="error">
              {t(pathError)}
            </p>
          )}
          {(contentKind || base?.kind) === 'lesson' && (
            <div className="owner-panel">
              <p>{t('builder.gateHelp')}</p>
              <Button
                disabled={busy || !validQuiz(value) || ((!base || dirty) && !onSave)}
                onClick={() =>
                  void run(async () => {
                    const draft = await ensureSaved();
                    if (!draft) {
                      return;
                    }
                    if (draft.kind !== 'lesson' || !draft.collection_key) {
                      setNotice('builder.pathNeeded');
                      return;
                    }
                    const collection = await call<CollectionDocument>(
                      '/collections/' + encodeURIComponent(draft.collection_key),
                    );
                    await prepareLessonPlan(collection, draft.key, csrf);
                    const gateEndpoint = `/content/${draft.key}/${draft.locale}/quiz-gate`;
                    const current = await call<QuizPath>(gateEndpoint);
                    await call(
                      gateEndpoint,
                      'PUT',
                      {
                        expected_version: current.version,
                        plan_id: current.plan_id,
                        assessment_id: draft.revision.quiz!.assessment_id,
                        enabled: !current.enabled,
                      },
                      csrf,
                    );
                    const refreshed = await call<QuizPath>(gateEndpoint);
                    pathGeneration.current++;
                    setPath(refreshed);
                    setPathError('');
                    setNotice('builder.gateSaved');
                  })
                }
              >
                {t(path?.enabled ? 'builder.removeGate' : 'builder.addGate')}
              </Button>
              {path?.plan_id && onPublishPlan && (
                <Button
                  disabled={busy || dirty}
                  onClick={() =>
                    void run(async () => {
                      await onPublishPlan();
                      const refreshed = await call<QuizPath>(endpoint + '/quiz-gate');
                      pathGeneration.current++;
                      setPath(refreshed);
                    })
                  }
                >
                  {t('progress.publishPlan')}
                </Button>
              )}
            </div>
          )}
          {preview && reader && (
            <div>
              <p>{t('builder.previewHelp')}</p>
              <LessonQuiz
                quiz={reader}
                onReload={() => setPreview(undefined)}
                onSubmit={async (submission) => {
                  try {
                    return await call<QuizResult>(
                      `/content/${preview.key}/${preview.locale}/quiz-preview`,
                      'POST',
                      submission,
                      csrf,
                    );
                  } catch (e) {
                    throw new QuizError(
                      e instanceof AuthError && e.code === 'revision_conflict'
                        ? 'revision_conflict'
                        : 'network_error',
                    );
                  }
                }}
              />
            </div>
          )}
        </>
      )}
      {notice && (
        <p
          role="status"
          className="workspace-alert"
          data-tone={
            notice === 'builder.reviewed' || notice === 'builder.gateSaved' ? 'success' : 'error'
          }
        >
          {t(notice)}
        </p>
      )}
    </section>
  );
}
