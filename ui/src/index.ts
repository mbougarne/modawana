// Modawana UI kit — import from here.
// CSS is separate: import './styles/index.css' once at the app entry (fonts + tokens + base + components).

export * from './types';

export * from './tokens/tokens';

export { ThemeProvider, useTheme, BrandStyle, type ThemeChoice } from './theme/ThemeProvider';

export {
  deriveBrand,
  brandCss,
  brandChecks,
  contrast,
  isHex,
  DEFAULT_PRIMARY,
  DEFAULT_SECONDARY,
} from './theme/brand';

export { I18nProvider, Label, useI18n, useT, type I18n } from './i18n/I18nProvider';

export {
  DEFAULT_LABELS,
  type LabelKey,
  type LabelOverrides,
  type L,
  type LabelText,
} from './i18n/labels';

export { LOCALES, DEFAULT_LOCALE, dirOf, intlOf, pickLocale, formatDate } from './i18n/locales';

export { Icon, type IconName } from './components/Icon';

export { cx } from './components/cx';

export {
  Button,
  IconButton,
  Tag,
  Sticker,
  Chip,
  TagChip,
  Card,
  Field,
  TextInput,
  TextArea,
  Select,
  Checkbox,
  Switch,
  SaveState,
} from './components/primitives';

export { Logo, Shell, SiteHeader, SiteFooter, type NavItem } from './components/layout';

export { Hero, Portrait, NowCard, SectionHead, renderHeadline, renderBio } from './components/home';

export {
  PostList,
  FilterBar,
  EmptyState,
  ArticleTags,
  Prose,
  CodeBlock,
  MediaFigure,
  AuthorCard,
  type Filters,
} from './components/posts';

export { LessonSidebar, LockedLesson, LessonQuiz } from './components/learning';

export { OtpInput, TagInput } from './components/owner';

export { MarkdownToolbar } from './editor/MarkdownToolbar';

export {
  markdownEdit,
  applyEdit,
  imageMarkdown,
  videoMarkdown,
  type MarkdownAction,
  type EditResult,
} from './editor/markdown';

export { resolveLabel, type ResolvedLabel } from './i18n/resolve';
