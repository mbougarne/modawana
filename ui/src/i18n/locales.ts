import type { FallbackPurpose, Locale, Localized } from '../types';

export const LOCALES: { code: Locale; name: string; dir: 'ltr' | 'rtl'; intl: string }[] = [
  { code: 'en', name: 'English', dir: 'ltr', intl: 'en-GB' },
  { code: 'fr', name: 'Français', dir: 'ltr', intl: 'fr-FR' },
  // Moroccan Arabic month names (أكتوبر، شتنبر، غشت…) with Western digits.
  { code: 'ar', name: 'العربية', dir: 'rtl', intl: 'ar-MA' },
];

export const DEFAULT_LOCALE: Locale = 'en';

export const dirOf = (l: Locale) => (l === 'ar' ? 'rtl' : 'ltr') as 'ltr' | 'rtl';

export const intlOf = (l: Locale) => LOCALES.find((x) => x.code === l)?.intl ?? 'en-GB';

/** Fallback is explicit and limited to profile/interface-label strings. */
export function pickLocale(
  value: Localized | undefined,
  locale: Locale,
  purpose: FallbackPurpose,
): string | undefined {
  if (purpose !== 'profile' && purpose !== 'label') {
    throw new Error('Fallback is not allowed for content');
  }
  if (!value) {
    return undefined;
  }
  const v = value[locale];
  return v === undefined || v === '' ? value.en : v;
}

/**
 * Server-rendered pages mirror this output in Go (`formatPublicDate` in
 * backend/internal/http/theme.go): day month year, Latin digits, and the
 * ar-MA month names. Change the locales, numbering system or default options
 * here only together with that mirror.
 */
export function formatDate(
  iso: string,
  locale: Locale,
  opts: Intl.DateTimeFormatOptions = { day: 'numeric', month: 'short', year: 'numeric' },
) {
  const parsed = new Date(iso + (iso.length === 10 ? 'T12:00:00' : ''));
  if (Number.isNaN(parsed.getTime())) {
    return iso;
  }
  return new Intl.DateTimeFormat(intlOf(locale), { ...opts, numberingSystem: 'latn' }).format(
    parsed,
  );
}
