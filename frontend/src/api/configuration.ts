import type { Locale } from './content';
import type { LabelOverrides } from '../../../ui/src/i18n/labels';

export type Localized = Partial<Record<Locale, string>>;

export interface SiteSettings {
  logo_mode: 'wordmark' | 'monogram' | 'image';
  wordmark: string;
  monogram: string;
  logo_media_id: string;
  favicon_media_id: string;
  primary: string;
  secondary: string;
  menu_icons: Record<string, string>;
}

export interface ProfileLink {
  id: string;
  label: Localized;
  url: string;
  icon: string;
  visible: boolean;
}

export interface NowRow {
  id: string;
  label: Localized;
  text: Localized;
}

export interface Experience {
  id: string;
  role: Localized;
  organization: Localized;
  description: Localized;
  start: string;
  end: string;
  current: boolean;
  url: string;
}

export interface ProfileSettings {
  name: Localized;
  headline: Localized;
  bio: Localized;
  sticker: Localized;
  portrait_media_id: string;
  portrait_alt: Localized;
  banner_media_id: string;
  banner_alt: Localized;
  banner_enabled: boolean;
  banner_x: number;
  banner_y: number;
  now: NowRow[];
  updated_month: string;
  links: ProfileLink[];
  main_action: ProfileLink | null;
  experience: Experience[];
  public_email: string;
  location: Localized;
  copyright: Localized;
  footer: Localized;
  about_key: string;
  show_now: boolean;
  show_writing: boolean;
  show_learning: boolean;
  writing_limit: number;
  learning_limit: number;
}

export type ConfigKind = 'settings' | 'profile' | 'labels';

export type Snapshot = SiteSettings | ProfileSettings | LabelOverrides;

export interface ConfigDocument<T = Snapshot> {
  kind: string;
  version: number;
  revision_id: string;
  snapshot: T;
}

export interface Review {
  path: string;
  locale: Locale;
  source_hash: string;
  value_hash: string;
  value?: unknown;
}

export interface ConfigPreview<T = Snapshot> extends ConfigDocument<T> {
  stale_reviews: Review[];
  required_reviews: Review[];
}

export interface Palette {
  version: number;
  light: Record<string, string>;
  dark: Record<string, string>;
  adjusted: boolean;
  checks: { theme: string; check: string; ratio: number; pass: boolean }[];
}

export interface PublicConfiguration {
  site: SiteSettings;
  profile: ProfileSettings;
  labels: LabelOverrides;
  palette: Palette;
  versions: Record<string, number>;
  about_url: string;
  effective: Record<string, { text: string; locale: Locale; fallback: boolean; stale: boolean }>;
}
