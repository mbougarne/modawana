import { useState } from 'react';
import { Button, LessonQuiz, QuizError, useI18n, type QuizResult, type QuizSubmission } from '@ui';
import type { PublicContent } from '../../api/content';
import { AuthError, call } from '../auth/api';
import { BrowserProgress } from './BrowserProgress';
import type { Progress } from './BrowserProgress';

export function progressChanged() {
  window.dispatchEvent(new Event('learning-progress'));
}

export function ReaderLearning({ content }: { content: PublicContent }) {
  const { t } = useI18n();
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState(false);
  const state = content.learning;
  if (!state) {
    return null;
  }
  const path = `/learning/content/${encodeURIComponent(content.key)}/${content.locale}`;
  async function identity() {
    return call<Progress>('/learning/session', 'POST', {}, undefined, { 'X-Blog-Request': '1' });
  }
  async function submit(incoming: QuizSubmission): Promise<QuizResult> {
    try {
      const session = await identity();
      return await call<QuizResult>(path + '/attempts', 'POST', incoming, session.csrf);
    } catch (e) {
      if (e instanceof AuthError) {
        throw new QuizError(
          e.code === 'revision_conflict' || e.code === 'idempotency_conflict'
            ? 'revision_conflict'
            : e.code === 'rate_limited'
              ? 'rate_limited'
              : 'unavailable',
        );
      }
      throw new QuizError('network_error');
    }
  }
  return (
    <section className="public-quiz">
      {state.completed && <p role="status">{t('progress.completed')}</p>}
      {state.unavailable && <p role="status">{t('progress.unavailable')}</p>}
      {state.prerequisites.length > 0 && (
        <nav aria-label={t('progress.prerequisite')}>
          <p>{t('progress.prerequisite')}</p>
          {state.prerequisites.map((a) => (
            <a key={a.locale} href={a.url}>
              {t('language.' + a.locale)}{' '}
            </a>
          ))}
        </nav>
      )}
      {state.alternatives.length > 0 && (
        <nav aria-label={t('progress.quizLanguage')}>
          <p>{t('progress.quizLanguage')}</p>
          {state.alternatives
            .filter((a) => a.locale !== content.locale)
            .map((a) => (
              <a key={a.locale} href={a.url}>
                {t('language.' + a.locale)}{' '}
              </a>
            ))}
        </nav>
      )}
      {state.quiz && !content.locked && (
        <LessonQuiz
          quiz={state.quiz}
          onSubmit={submit}
          onPassed={progressChanged}
          onReload={progressChanged}
        />
      )}
      {state.can_mark_read && !state.completed && !content.locked && (
        <Button
          disabled={busy}
          onClick={async () => {
            setBusy(true);
            setFailed(false);
            try {
              const session = await identity();
              await call(path + '/read', 'POST', { version: state.version }, session.csrf);
              progressChanged();
            } catch {
              setFailed(true);
            } finally {
              setBusy(false);
            }
          }}
        >
          {t('progress.markRead')}
        </Button>
      )}
      {failed && <p role="alert">{t('progress.failed')}</p>}
      {(content.kind === 'lesson' || state.quiz) && <BrowserProgress />}
    </section>
  );
}
