import type { QuizQuestion, QuizForReader, QuizVersion } from '@ui';

export interface PrivateQuestion extends QuizQuestion {
  correct: string[];
  explanation: string;
}

export interface QuizDefinition {
  assessment_id?: string;
  pass_mark: number;
  show_explanations: boolean;
  questions: PrivateQuestion[];
}

export interface QuizPath {
  key: string;
  version: number;
  plan_id: string;
  enabled: boolean;
}

export function validQuiz(d: QuizDefinition): boolean {
  if (
    d.questions.length < 1 ||
    d.questions.length > 20 ||
    !Number.isInteger(d.pass_mark) ||
    d.pass_mark < 1 ||
    d.pass_mark > d.questions.length
  ) {
    return false;
  }
  const seen = new Set<string>();
  return d.questions.every((q) => {
    if (
      !/^[a-zA-Z0-9_-]{1,64}$/.test(q.id) ||
      seen.has(q.id) ||
      !q.text.trim() ||
      [...q.text].length > 2000 ||
      [...q.explanation].length > 4000 ||
      q.options.length < 2 ||
      q.options.length > 8
    ) {
      return false;
    }
    seen.add(q.id);
    const options = new Set(q.options.map((o) => o.id));
    return (
      options.size === q.options.length &&
      q.options.every(
        (o) => /^[a-zA-Z0-9_-]{1,64}$/.test(o.id) && o.text.trim() && [...o.text].length <= 1000,
      ) &&
      new Set(q.correct).size === q.correct.length &&
      q.correct.every((id) => options.has(id)) &&
      q.correct.length >= 1 &&
      (q.type === 'multi' || q.correct.length === 1) &&
      (q.type !== 'bool' ||
        (q.options.length === 2 && q.options[0].id === 'true' && q.options[1].id === 'false'))
    );
  });
}

export interface ReaderLearningState {
  version: QuizVersion;
  quiz?: QuizForReader;
  completed: boolean;
  can_mark_read: boolean;
  unavailable: boolean;
  alternatives: import('./content').Alternative[];
  prerequisites: import('./content').Alternative[];
}
