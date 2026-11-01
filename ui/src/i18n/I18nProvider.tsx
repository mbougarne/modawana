import { createContext, useCallback, useContext, useEffect, useMemo, type ReactNode } from 'react';
import type { FallbackPurpose, Locale, Localized } from '../types';
import { type LabelKey, type LabelOverrides } from './labels';
import { resolveLabel, type ResolvedLabel, type Vars } from './resolve';
import { dirOf, formatDate, pickLocale } from './locales';

export interface I18n {
  locale: Locale;
  dir: 'ltr' | 'rtl';
  /** Interface label in the current language → English → the key itself. */
  t: (key: LabelKey | (string & {}), vars?: Vars) => string;
  /** Explicit profile/label fallback only; never published content. */
  pick: (value: Localized | undefined, purpose: FallbackPurpose) => string | undefined;
  label: (key: string, vars?: Vars) => ResolvedLabel;
  date: (iso: string, opts?: Intl.DateTimeFormatOptions) => string;
}

const Ctx = createContext<I18n | null>(null);

export function I18nProvider({
  locale,
  overrides,
  children,
  syncDocument = true,
}: {
  locale: Locale;
  overrides?: LabelOverrides;
  children: ReactNode;
  /** Set <html lang dir> (default true). Turn off when rendering a nested preview. */
  syncDocument?: boolean;
}) {
  const dir = dirOf(locale);
  useEffect(() => {
    if (!syncDocument) {
      return;
    }
    document.documentElement.lang = locale;
    document.documentElement.dir = dir;
  }, [locale, dir, syncDocument]);

  const label = useCallback(
    (key: string, vars?: Vars) => resolveLabel(key, locale, overrides, vars),
    [locale, overrides],
  );
  const t = useCallback((key: string, vars?: Vars) => label(key, vars).text, [label]);

  const value = useMemo<I18n>(
    () => ({
      locale,
      dir,
      t,
      label,
      pick: (v, purpose) => pickLocale(v, locale, purpose),
      date: (iso, opts) => formatDate(iso, locale, opts),
    }),
    [locale, dir, t, label],
  );

  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function useI18n(): I18n {
  const v = useContext(Ctx);
  if (!v) {
    throw new Error('useI18n must be used inside <I18nProvider>');
  }
  return v;
}

/** Shorthand: const t = useT(); t('nav.articles') */
export const useT = () => useI18n().t;

/** Language metadata for a label that falls back inside a differently directed page. */
export function Label({ name, vars }: { name: LabelKey | (string & {}); vars?: Vars }) {
  const { label } = useI18n();
  const resolved = label(name, vars);
  return (
    <span lang={resolved.locale} dir={resolved.dir}>
      {resolved.text}
    </span>
  );
}
