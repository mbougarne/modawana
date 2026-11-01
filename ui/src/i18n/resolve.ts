import type { Locale } from '../types';
import { DEFAULT_LABELS, type L, type LabelOverrides, type LabelText } from './labels';
import { dirOf, intlOf } from './locales';

export type Vars = Record<string, string | number>;

export interface ResolvedLabel {
  text: string;
  locale: Locale;
  dir: 'ltr' | 'rtl';
  fallback: boolean;
}
const nonempty = (value: LabelText | undefined): value is LabelText =>
  value !== undefined && value !== '';

export function resolveLabel(
  key: string,
  locale: Locale,
  overrides?: LabelOverrides,
  vars?: Vars,
): ResolvedLabel {
  const base = Object.hasOwn(DEFAULT_LABELS, key)
    ? (DEFAULT_LABELS as Record<string, L>)[key]
    : undefined;
  const override = overrides && Object.hasOwn(overrides, key) ? overrides[key] : undefined;
  const english = nonempty(override?.en) ? override.en : (base?.en ?? key);
  // An explicit empty override must bypass the shipped translation.
  const localized = override && Object.hasOwn(override, locale) ? override[locale] : base?.[locale];
  const fallback = !nonempty(localized);
  const actualLocale = fallback ? 'en' : locale;
  const value = fallback ? english : localized;
  const text =
    typeof value === 'string'
      ? value
      : (value[new Intl.PluralRules(intlOf(actualLocale)).select(Number(vars?.n ?? 0))] ??
        value.other);
  return {
    text: text.replace(/\{(\w+)\}/g, (token, name: string) =>
      vars && Object.hasOwn(vars, name) ? String(vars[name]) : token,
    ),
    locale: actualLocale,
    dir: dirOf(actualLocale),
    fallback: actualLocale !== locale,
  };
}
