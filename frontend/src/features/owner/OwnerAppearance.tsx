import { createContext, useContext, useState } from 'react';
import { useI18n } from '../../../../ui/src/i18n/I18nProvider';

const themes = ['classic', 'sidebar'] as const;
type AdminTheme = (typeof themes)[number];

const storageKey = 'modawana.adminTheme';

export const OwnerAppearanceContext = createContext<{
  theme: AdminTheme;
  selectTheme: (theme: AdminTheme) => void;
} | null>(null);

export function useOwnerAppearance() {
  const [theme, setTheme] = useState<AdminTheme>(() => {
    try {
      return localStorage.getItem(storageKey) === 'sidebar' ? 'sidebar' : 'classic';
    } catch {
      return 'classic';
    }
  });

  function selectTheme(next: AdminTheme) {
    setTheme(next);
    try {
      localStorage.setItem(storageKey, next);
    } catch {
      // A browser that blocks storage can still apply the choice on this page.
    }
  }

  return { theme, selectTheme };
}

export function OwnerAppearanceSettings() {
  const { t } = useI18n();
  const appearance = useContext(OwnerAppearanceContext);
  if (!appearance) {
    throw new Error('Owner appearance settings require the owner workspace');
  }
  return (
    <section className="owner-panel owner-appearance">
      <h2>{t('ownerAppearance.title')}</h2>
      <label className="owner-appearance-field">
        <span>{t('ownerAppearance.theme')}</span>
        <select
          value={appearance.theme}
          onChange={(event) =>
            appearance.selectTheme(event.target.value === 'sidebar' ? 'sidebar' : 'classic')
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
