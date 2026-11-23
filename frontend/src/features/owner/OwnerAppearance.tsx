import { createContext, useContext, useState } from 'react';
import { useI18n } from '../../../../ui/src/i18n/I18nProvider';

const themes = ['classic', 'slate'] as const;
type AdminTheme = (typeof themes)[number];

const ownerThemeStorageKey = 'modawana.adminTheme';

export const OwnerAppearanceContext = createContext<{
  theme: AdminTheme;
  selectTheme: (theme: AdminTheme) => void;
  clearTheme: () => void;
} | null>(null);

export function useOwnerAppearance(preferred?: AdminTheme) {
  const [override, setTheme] = useState<AdminTheme | undefined>(() => {
    try {
      const stored = localStorage.getItem(ownerThemeStorageKey);
      return stored === 'slate' || stored === 'sidebar'
        ? 'slate'
        : stored === 'classic'
          ? 'classic'
          : undefined;
    } catch {
      return undefined;
    }
  });

  function selectTheme(next: AdminTheme) {
    setTheme(next);
    try {
      localStorage.setItem(ownerThemeStorageKey, next);
    } catch {
      // A browser that blocks storage can still apply the choice on this page.
    }
  }

  function clearTheme() {
    setTheme(undefined);
    try {
      localStorage.removeItem(ownerThemeStorageKey);
    } catch {
      // An unavailable browser preference does not affect the saved layout.
    }
  }

  return { theme: override ?? preferred ?? 'classic', selectTheme, clearTheme };
}

export function useOwnerAppearanceContext() {
  const appearance = useContext(OwnerAppearanceContext);
  if (!appearance) {
    throw new Error('Owner appearance settings require the owner workspace');
  }
  return appearance;
}

export function OwnerAppearanceSettings({ classicOnly = false }: { classicOnly?: boolean }) {
  const { t } = useI18n();
  const appearance = useOwnerAppearanceContext();
  if (classicOnly && appearance.theme !== 'classic') {
    return null;
  }
  return (
    <section className="owner-panel owner-appearance">
      <h2>{t('ownerAppearance.title')}</h2>
      <label className="owner-appearance-field">
        <span>{t('ownerAppearance.theme')}</span>
        <select
          value={appearance.theme}
          onChange={(event) =>
            appearance.selectTheme(event.target.value === 'slate' ? 'slate' : 'classic')
          }
        >
          {themes.map((theme) => (
            <option key={theme} value={theme}>
              {t('ownerAppearance.' + theme)}
            </option>
          ))}
        </select>
      </label>
      <p>{t('ownerAppearance.help')}</p>
    </section>
  );
}
