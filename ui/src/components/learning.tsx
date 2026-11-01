/* eslint-disable no-control-regex -- Reject control bytes in untrusted URLs. */
import { useEffect, useId, useRef, useState } from 'react';
import {
  QuizError,
  type LessonLink,
  type QuizForReader,
  type QuizResult,
  type QuizSubmission,
  type QuizVersion,
} from '../types';
import { useI18n } from '../i18n/I18nProvider';
import { Icon } from './Icon';
import { Button, Sticker } from './primitives';

/** Sidebar of a learning path. Three states: open (link), locked (plain text + lock), soon (plain text). */
export function LessonSidebar({
  title,
  progress,
  lessons,
}: {
  title: string;
  progress?: string;
  lessons: LessonLink[];
}) {
  const { t } = useI18n();
  return (
    <nav className="vb-side" aria-label={t('lesson.list')}>
      <h4>
        {title}
        {progress && ` · ${progress}`}
      </h4>
      <ol>
        {lessons.map((l) => (
          <li key={l.id}>
            {l.state === 'locked' ? (
              <span className="locked">
                <span className="n">{l.number}</span>
                {l.title}
                <Icon name="lock" className="vb-lock" />
                <span className="vb-visually-hidden"> ({t('lesson.lockedShort')})</span>
              </span>
            ) : l.state === 'soon' || !l.href ? (
              <span className="soon">
                <span className="n">{l.number}</span>
                {l.title}
                <small>{t('lesson.soon')}</small>
              </span>
            ) : (
              <a href={l.href} aria-current={l.current ? 'page' : undefined}>
                <span className="n">{l.number}</span>
                {l.title}
                {l.completed && <small>{t('progress.completed')}</small>}
              </a>
            )}
          </li>
        ))}
      </ol>
    </nav>
  );
}

/** Only explicitly public teaser text is allowed above the locked panel. */
export function LockedLesson({
  teaser,
  quizHref,
  steps,
}: {
  teaser?: string;
  quizHref: string;
  steps: { label: string; done: boolean }[];
}) {
  const { t, dir } = useI18n();
  return (
    <div className="vb-locked">
      {teaser && (
        <div className="teaser" aria-hidden="true">
          {teaser}
        </div>
      )}
      <div className="vb-lockpanel" role="region" aria-label={t('lesson.locked')}>
        <span className="big-lock">
          <Icon name="lock" size={28} />
        </span>
        <h2>{t('lesson.locked')}</h2>
        <p>{t('lesson.lockedHelp')}</p>
        <ol className="vb-steps">
          {steps.map((s) => (
            <li key={s.label} data-done={s.done}>
              {s.label}
            </li>
          ))}
        </ol>
        <Button variant="primary" href={quizHref}>
          {t('lesson.goQuiz')} {dir === 'rtl' ? '←' : '→'}
        </Button>
      </div>
    </div>
  );
}

interface QuizProps {
  quiz: QuizForReader | null;
  loadError?: QuizError;
  onSubmit: (submission: QuizSubmission) => Promise<QuizResult>;
  onPassed?: (result: QuizResult) => void;
  onReload: () => void;
}
const versionKey = (version: QuizVersion) =>
  JSON.stringify([version.quiz, version.content, version.path, version.epoch]);
const errorLabel = (error: QuizError) =>
  (
    ({
      network_error: 'quiz.networkError',
      revision_conflict: 'quiz.stale',
      rate_limited: 'quiz.rateLimited',
      unavailable: 'quiz.unavailable',
    }) as const
  )[error.code];

/** Keying the attempt to its versions also discards obsolete async responses after a reload. */
export function LessonQuiz(props: QuizProps) {
  const { t } = useI18n();
  if (props.loadError) {
    return (
      <div role="alert">
        <p>{t(errorLabel(props.loadError))}</p>
        <Button onClick={props.onReload}>{t('quiz.reload')}</Button>
      </div>
    );
  }
  if (!props.quiz) {
    return <p role="status">{t('quiz.loading')}</p>;
  }
  return (
    <QuizAttempt
      key={props.quiz.id + ':' + props.quiz.locale + ':' + versionKey(props.quiz.version)}
      {...props}
      quiz={props.quiz}
    />
  );
}

function QuizAttempt({ quiz, onSubmit, onPassed, onReload }: QuizProps & { quiz: QuizForReader }) {
  const { t, dir } = useI18n();
  const titleId = useId();
  const [answers, setAnswers] = useState<Record<string, string[]>>({});
  const [result, setResult] = useState<QuizResult | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<QuizError | null>(null);
  const [needAll, setNeedAll] = useState(false);
  const mounted = useRef(true);
  const pending = useRef(false);
  const submission = useRef<QuizSubmission | null>(null);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  const answered = quiz.questions.filter((q) => answers[q.id]?.length).length;
  const mustReload = error?.code === 'revision_conflict' || error?.code === 'unavailable';

  const pick = (qid: string, oid: string, multi: boolean) => {
    if (result || pending.current || mustReload) {
      return;
    }
    submission.current = null;
    setError(null);
    setAnswers((a) => {
      const current = a[qid] ?? [];
      return {
        ...a,
        [qid]: multi
          ? current.includes(oid)
            ? current.filter((x) => x !== oid)
            : [...current, oid]
          : [oid],
      };
    });
  };
  const check = async () => {
    if (pending.current || result || mustReload) {
      return;
    }
    if (answered < quiz.questions.length) {
      setNeedAll(true);
      return;
    }
    setNeedAll(false);
    setError(null);
    setBusy(true);
    pending.current = true;
    try {
      // Retry the exact same operation after an uncertain network outcome.
      submission.current ??= {
        version: quiz.version,
        answers,
        idempotencyKey: crypto.randomUUID(),
      };
      const response = await onSubmit(submission.current);
      if (!mounted.current) {
        return;
      }
      if (versionKey(response.version) !== versionKey(quiz.version)) {
        throw new QuizError('revision_conflict');
      }
      setResult(response);
      if (response.passed) {
        onPassed?.(response);
      }
    } catch (cause) {
      if (mounted.current) {
        setError(cause instanceof QuizError ? cause : new QuizError('network_error'));
      }
    } finally {
      pending.current = false;
      if (mounted.current) {
        setBusy(false);
      }
    }
  };
  const retry = () => {
    submission.current = null;
    setAnswers({});
    setResult(null);
    setError(null);
  };

  return (
    <section className="vb-quiz" id="quiz" aria-labelledby={titleId} aria-busy={busy}>
      <div className="vb-quiz-head">
        <h2 className="vb-h2" id={titleId}>
          {t('quiz.title')}
        </h2>
        <Sticker>{t('quiz.passWith', { n: quiz.questions.length, p: quiz.passMark })}</Sticker>
      </div>
      {quiz.questions.map((q, qi) => {
        const multi = q.type === 'multi';
        const feedback = result?.feedback[q.id];
        const correct = feedback?.correctOptionIds;
        return (
          <fieldset className="vb-q" key={q.id} disabled={busy || !!result || mustReload}>
            <legend>
              {qi + 1}. {q.text}
              {multi && <span className="vb-meta">{t('quiz.selectAll')}</span>}
            </legend>
            {q.options.map((o) => {
              const chosen = answers[q.id]?.includes(o.id) ?? false;
              const isRight = correct?.includes(o.id);
              const mark = !correct
                ? undefined
                : chosen && isRight
                  ? 'right'
                  : chosen
                    ? 'wrong'
                    : isRight && multi
                      ? 'right'
                      : undefined;
              return (
                <label className="vb-opt" key={o.id} data-result={mark}>
                  <input
                    type={multi ? 'checkbox' : 'radio'}
                    name={titleId + q.id}
                    checked={chosen}
                    onChange={() => pick(q.id, o.id, multi)}
                  />
                  <span>{o.text}</span>
                  {mark && (
                    <span className="mark">
                      {chosen && isRight
                        ? `✓ ${t('quiz.correct')}`
                        : chosen
                          ? `✗ ${t('quiz.wrong')}`
                          : t('quiz.also')}
                    </span>
                  )}
                </label>
              );
            })}
            {feedback && !correct && <p>{t(feedback.correct ? 'quiz.correct' : 'quiz.wrong')}</p>}
            {quiz.showExplanations && feedback?.explanation && (
              <p className="vb-explain">{feedback.explanation}</p>
            )}
          </fieldset>
        );
      })}
      <div aria-live="polite">
        {busy && <p role="status">{t('quiz.submitting')}</p>}
        {needAll && (
          <div role="alert" className="vb-result" data-pass="false">
            {t('quiz.answerAll')}
          </div>
        )}
        {error && <p role="alert">{t(errorLabel(error))}</p>}
        {result && (
          <div className="vb-result" data-pass={result.passed}>
            <b>
              {result.passed
                ? t('quiz.passed', { s: result.score, t: result.total })
                : t('quiz.failed', { s: result.score, t: result.total, p: quiz.passMark })}
            </b>
            {result.passed &&
              result.unlockedHref &&
              /^\/(?![/\\])[^\\\u0000-\u0020]*$/.test(result.unlockedHref) && (
                <div>
                  <Button variant="primary" href={result.unlockedHref}>
                    {t('quiz.continue')} {dir === 'rtl' ? '←' : '→'}
                  </Button>
                </div>
              )}
          </div>
        )}
      </div>
      <div className="vb-quiz-foot">
        {mustReload ? (
          <Button onClick={onReload}>{t('quiz.reload')}</Button>
        ) : (
          !result && (
            <Button variant="primary" onClick={check} disabled={busy}>
              {t('quiz.check')}
            </Button>
          )
        )}
        {result && !result.passed && <Button onClick={retry}>{t('quiz.retry')}</Button>}
        <span className="vb-meta">
          {t('quiz.answered', { n: answered, t: quiz.questions.length })}
        </span>
      </div>
    </section>
  );
}
