import { Label, useI18n } from '@ui';
import { isLocale, localeCodes, publicLocaleDefaults } from '../../../../../ui/src/i18n/locales';
import type { Locale } from '../../../api/content';
import { Section } from '../../../features/configuration/controls';
import { useFieldIssue } from '../../../features/configuration/ValidationIssues';

export function PublicLanguageSettings({
  value = publicLocaleDefaults,
  defaultLocale,
  onChange,
}: {
  value?: Locale[];
  defaultLocale?: Locale;
  onChange: (locales: Locale[], defaultLocale?: Locale) => void;
}) {
  const { t } = useI18n();
  const issue = useFieldIssue('public_locales');
  const defaultIssue = useFieldIssue('public_default_locale');
  const visibleLocales = localeCodes.filter((code) => value.includes(code));
  return (
    <Section name="config.publicLanguages">
      <p>{t('config.publicLanguagesHelp')}</p>
      <div
        className="config-language-options"
        role="group"
        aria-label={t('config.publicLanguages')}
        {...issue}
      >
        {localeCodes.map((locale) => {
          const visible = value.includes(locale);
          return (
            <label key={locale} className="config-check">
              <input
                type="checkbox"
                checked={visible}
                disabled={visible && value.length === 1}
                onChange={() => {
                  const next = localeCodes.filter((choice) =>
                    choice === locale ? !visible : value.includes(choice),
                  );
                  onChange(
                    next,
                    defaultLocale && next.includes(defaultLocale) ? defaultLocale : undefined,
                  );
                }}
              />
              <Label name={'language.' + locale} />
            </label>
          );
        })}
      </div>
      <label className="config-field">
        <Label name="config.choosePublicDefault" />
        <select
          {...defaultIssue}
          value={defaultLocale ?? visibleLocales[0]}
          onChange={(event) => {
            if (isLocale(event.target.value)) {
              onChange(value, event.target.value);
            }
          }}
        >
          {visibleLocales.map((locale) => (
            <option key={locale} value={locale}>
              {t('language.' + locale)}
            </option>
          ))}
        </select>
      </label>
    </Section>
  );
}
