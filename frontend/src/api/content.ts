import type { QuizDefinition, ReaderLearningState } from './learning';
import type { MediaRecord } from './contracts';

export type Locale = 'en' | 'ar' | 'fr';

export type ContentKind = 'article' | 'lesson' | 'doc';

export interface MediaPresentation {
  media_id: string;
  poster_media_id?: string;
  caption_track_media_id?: string;
  caption: string;
  transcript: string;
}

export interface DraftInput {
  expected_collection_version?: number;
  quiz?: QuizDefinition;
  expected_version: number;
  kind: ContentKind;
  source_locale: Locale;
  slug: string;
  title: string;
  summary?: string;
  markdown: string;
  source_revision_id?: string;
  category_key?: string;
  tag_keys?: string[];
  cover_media_id?: string;
  media_presentations?: MediaPresentation[];
  collection_key?: string;
  position?: number;
}

export interface RevisionResult {
  revision_id: string;
  version: number;
  created: boolean;
}

export interface RevisionAction {
  expected_version: number;
  revision_id: string;
  source_revision_id?: string;
}

export interface Revision {
  quiz?: QuizDefinition;
  revision_id: string;
  title: string;
  summary: string;
  markdown: string;
  slug: string;
  source_revision_id: string;
  category_key: string;
  tag_keys: string[];
  cover_media_id: string;
  created_at: string;
  media: MediaRecord[];
  media_presentations: MediaPresentation[];
}

export interface Draft {
  collection_key: string;
  position: number;
  slug_locked: boolean;
  key: string;
  kind: ContentKind;
  source_locale: Locale;
  locale: Locale;
  version: number;
  published_revision_id: string;
  reviewed: boolean;
  translation_stale: boolean;
  revision: Revision;
}

export interface Alternative {
  locale: Locale;
  slug: string;
  url: string;
}

export interface PublicMedia {
  id: string;
  url: string;
  kind: MediaRecord['kind'];
  mime_type: string;
  width: number;
  height: number;
}

export interface PublicContent {
  learning?: ReaderLearningState;
  reading_minutes?: number;
  key: string;
  kind: ContentKind;
  locale: Locale;
  slug: string;
  title: string;
  summary: string;
  revision_id: string;
  published_at: string;
  locked: boolean;
  translation_stale: boolean;
  markdown?: string;
  media?: PublicMedia[];
  media_presentations?: MediaPresentation[];
  cover_media_id?: string;
  category_key?: string;
  tag_keys?: string[];
  alternatives: Alternative[];
}

export interface DiscoveryEntry {
  url: string;
  category_key?: string;
  tag_keys: string[];
  reading_minutes?: number;
  key: string;
  kind: ContentKind;
  locale: Locale;
  slug: string;
  title: string;
  summary: string;
  published_at: string;
  locked: boolean;
}

export interface DiscoveryPage {
  items: DiscoveryEntry[];
  total: number;
  next_cursor: string;
}

export interface TaxonomyInput {
  expected_version: number;
  label: string;
  slug: string;
}

export interface TaxonomyTerm {
  id: string;
  key: string;
  kind: 'category' | 'tag';
  locale: Locale;
  label: string;
  slug: string;
  version?: number;
  count: number;
}

export interface CollectionInput {
  expected_version: number;
  kind: 'learning' | 'docs';
  slug: string;
  title: string;
  summary?: string;
  visible: boolean;
}

export interface CollectionResult {
  id: string;
  version: number;
  plan_id?: string;
}

export interface PlanMember {
  key: string;
  announcements: Partial<Record<Locale, string>>;
}

export interface PlanInput {
  expected_version: number;
  members: PlanMember[];
}

export interface ApplyPlanInput {
  expected_version: number;
  plan_id: string;
}

export interface CollectionLocale {
  locale: Locale;
  slug: string;
  title: string;
  summary: string;
  visible: boolean;
}

export interface CollectionDocument {
  assigned_members: PlanMember[];
  member_titles: Record<string, string>;
  draft_gate_policy: number;
  id: string;
  key: string;
  kind: 'learning' | 'docs';
  version: number;
  draft_plan_id: string;
  active_plan_id: string;
  locales: CollectionLocale[];
  draft_members: PlanMember[];
  active_members: PlanMember[];
}

export interface CollectionEntry {
  completed: boolean;
  key?: string;
  title: string;
  summary?: string;
  position: number;
  state: 'open' | 'locked' | 'soon';
  url?: string;
  slug?: string;
}

export interface PublicCollection {
  url: string;
  key: string;
  kind: 'learning' | 'docs';
  locale: Locale;
  slug: string;
  title: string;
  summary: string;
  published_count: number;
  announced_count: number;
  total: number;
  items: CollectionEntry[];
}

export interface PageRoute {
  kind: string;
  locale: Locale;
  url: string;
  title: string;
  summary: string;
  alternatives: Alternative[];
  content?: PublicContent;
  collection?: PublicCollection;
}

export interface CollectionPage {
  items: PublicCollection[];
  next_cursor: string;
}
