import { Button, Field, useI18n } from '@ui';
import type { SiteSettings } from '../../../api/configuration';
import { Section } from '../../../features/configuration/controls';
import { useFieldIssue } from '../../../features/configuration/ValidationIssues';

type Rules = NonNullable<SiteSettings['redirects']>;

export function RedirectSettings({
  rules = [],
  onChange,
}: {
  rules?: Rules;
  onChange: (value: Rules) => void;
}) {
  const { t } = useI18n();
  const issue = useFieldIssue('redirects');
  return (
    <Section name="seo.redirects">
      <p>{t('seo.redirectHelp')}</p>
      <div {...issue}>
        {rules.map((rule, index) => (
          <div key={index}>
            {(['from', 'to'] as const).map((key) => (
              <Field label={t('seo.' + key)} key={key}>
                <input
                  dir="ltr"
                  value={rule[key]}
                  maxLength={512}
                  onChange={(event) =>
                    onChange(
                      rules.map((item, position) =>
                        position === index ? { ...item, [key]: event.target.value } : item,
                      ),
                    )
                  }
                />
              </Field>
            ))}
            <Button
              type="button"
              onClick={() => onChange(rules.filter((_, position) => position !== index))}
            >
              {t('config.remove')}
            </Button>
          </div>
        ))}
      </div>
      <Button
        type="button"
        disabled={rules.length >= 100}
        onClick={() => onChange([...rules, { from: '', to: '' }])}
      >
        {t('seo.addRedirect')}
      </Button>
    </Section>
  );
}
