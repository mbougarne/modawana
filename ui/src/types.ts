// Presentation contracts; Go remains authoritative for validation and access.
export type Locale = 'en' | 'fr' | 'ar';

export type Localized<T = string> = Partial<Record<Locale, T>> & { en: T };

/** Only these text domains may use English fallback. Published content is locale-specific. */
export type FallbackPurpose = 'profile' | 'label';

export type PostKind = 'article' | 'lesson' | 'doc';

export interface Category {
  id: string;
  slug: string;
  label: string;
}

export interface PostSummary {
  locked?: boolean;
  id: string;
  slug: string;
  kind: PostKind;
  title: string;
  summary?: string;
  publishedAt: string;
  readingMinutes?: number;
  category?: Category;
  tags: string[];
  href: string;
}

export interface PublishedContent {
  locale: Locale;
  revision: string;
  title: string;
  markdown: string;
  availableLanguages: { locale: Locale; href: string }[];
}

export type QuestionType = 'single' | 'multi' | 'bool';

export interface QuizOption {
  id: string;
  text: string;
}

/** Public projection: deliberately no explanation or answer-key field. */
export interface QuizQuestion {
  id: string;
  type: QuestionType;
  text: string;
  options: QuizOption[];
}

export interface QuizVersion {
  quiz: string;
  content: string;
  path?: string;
  epoch?: number;
}

export interface QuizForReader {
  id: string;
  version: QuizVersion;
  locale: Locale;
  passMark: number;
  unlocksNext: boolean;
  showExplanations: boolean;
  questions: QuizQuestion[];
}

export interface QuizSubmission {
  version: QuizVersion;
  answers: Record<string, string[]>;
  idempotencyKey: string;
}

/** Post-submission feedback only. Correctness and unlock actions are server decisions. */
export interface QuizFeedback {
  correct: boolean;
  correctOptionIds?: string[];
  explanation?: string;
}

export interface QuizResult {
  version: QuizVersion;
  score: number;
  total: number;
  passed: boolean;
  feedback: Record<string, QuizFeedback>;
  unlockedHref?: string;
}

export type QuizErrorCode = 'network_error' | 'revision_conflict' | 'rate_limited' | 'unavailable';

export class QuizError extends Error {
  constructor(
    public code: QuizErrorCode,
    public retryAfter?: number,
  ) {
    super(code);
  }
}

export type LessonState = 'open' | 'locked' | 'soon';

export interface LessonLink {
  completed?: boolean;
  id: string;
  number: number;
  title: string;
  href?: string;
  state: LessonState;
  current?: boolean;
}

export interface ProfileLink {
  id: string;
  label: Localized;
  url: string;
  visible: boolean;
  icon?: 'github' | 'linkedin' | 'rss';
}

export interface ExperienceEntry {
  id: string;
  role: Localized;
  organization: Localized;
  description: Localized;
  startMonth: string;
  endMonth?: string;
  current: boolean;
  url?: string;
}

export interface Profile {
  avatarMediaId?: string;
  avatarUrl?: string;
  avatarAlt: Localized;
  name: Localized;
  headline: Localized;
  bio: Localized;
  sticker: Localized;
  now: { id: string; label: Localized; text: Localized }[];
  nowUpdated?: string;
  mainAction?: ProfileLink;
  links: ProfileLink[];
  banner?: { mediaId: string; url: string; alt: Localized; focalPoint: { x: number; y: number } };
  experience: ExperienceEntry[];
  aboutDocumentKey?: string;
  publicEmail?: string;
  location?: Localized;
  copyrightName?: Localized;
  footerInvitation?: Localized;
  home: {
    showBanner: boolean;
    showNow: boolean;
    showWriting: boolean;
    showLearning: boolean;
    writingCount: number;
    learningCount: number;
  };
}

export interface Brand {
  primary: string;
  secondary: string;
  logo:
    | { kind: 'wordmark'; text: string }
    | { kind: 'monogram'; letters: string }
    | { kind: 'image'; url: string; alt: string };
  monogram: string;
}
