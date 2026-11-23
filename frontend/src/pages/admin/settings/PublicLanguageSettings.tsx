import { Label, useI18n } from '@ui';
import type { Locale } from '../../../api/content';
import { Section } from '../../../features/configuration/controls';
import { useFieldIssue } from '../../../features/configuration/ValidationIssues';

const supportedLocales: Locale[] = ['en', 'ar', 'fr'];

export function PublicLanguageSettings({
  value = supportedLocales,
  onChange,
}: {
  value?: Locale[];
  onChange: (locales: Locale[] | undefined) => void;
}) {
  const { t } = useI18n();
  const issue = useFieldIssue('public_locales');
  const defaultLocale = supportedLocales.find((locale) => value.includes(locale));
  return (
    <Section name="config.publicLanguages">
      <p>{t('config.publicLanguagesHelp')}</p>
      <div
        className="config-language-options"
        role="group"
        aria-label={t('config.publicLanguages')}
        {...issue}
      >
        {supportedLocales.map((locale) => {
          const visible = value.includes(locale);
          return (
            <label key={locale} className="config-check">
              <input
                type="checkbox"
                checked={visible}
                disabled={visible && value.length === 1}
                onChange={() => {
                  const next = supportedLocales.filter((choice) =>
                    choice === locale ? !visible : value.includes(choice),
                  );
                  onChange(next.length === supportedLocales.length ? undefined : next);
                }}
              />
              <Label name={'language.' + locale} />
            </label>
          );
        })}
      </div>
      {defaultLocale && (
        <p className="vb-meta">
          {t('config.publicDefaultLanguage', { language: t('language.' + defaultLocale) })}
        </p>
      )}
    </Section>
  );
}
