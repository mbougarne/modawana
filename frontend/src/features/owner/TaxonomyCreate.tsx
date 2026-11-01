import { useState } from 'react';
import { Button, Field, useI18n } from '@ui';
import { generateStableKey } from '@ui/components/StableKeyField';
import { slugFromTitle } from '../content/slug';
import { call, requestErrorKey } from '../auth/api';
import type { Locale, TaxonomyTerm } from '../../api/content';

export function TaxonomyCreate({
  kind,
  locale,
  csrf,
  disabled,
  onCreated,
}: {
  kind: 'category' | 'tag';
  locale: Locale;
  csrf: string;
  disabled: boolean;
  onCreated: (term: TaxonomyTerm) => void;
}) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const [label, setLabel] = useState('');
  const [slug, setSlug] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const title = t(kind === 'category' ? 'editor.createCategory' : 'editor.createTag');
  async function create() {
    if (busy || disabled) {
      return;
    }
    setBusy(true);
    setError('');
    try {
      const key = generateStableKey();
      const result = await call<TaxonomyTerm>(
        `/taxonomy/${kind}/${key}/${locale}`,
        'PUT',
        { expected_version: 0, label: label.trim(), slug },
        csrf,
      );
      onCreated(result);
      setLabel('');
      setSlug('');
      setOpen(false);
    } catch (error) {
      setError(requestErrorKey(error));
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="editor-taxonomy-create">
      <Button disabled={disabled || busy} aria-expanded={open} onClick={() => setOpen(!open)}>
        {title}
      </Button>
      {open && (
        <fieldset disabled={disabled || busy}>
          <legend>{title}</legend>
          <Field label={t('owner.label')}>
            <input
              value={label}
              maxLength={120}
              onChange={(e) => {
                setLabel(e.target.value);
                setSlug(slugFromTitle(e.target.value, 120));
              }}
            />
          </Field>
          <Field label={t('owner.slug')}>
            <input value={slug} maxLength={120} onChange={(e) => setSlug(e.target.value)} />
          </Field>
          {error && <p role="alert">{t(error)}</p>}
          <Button disabled={!label.trim() || !slug} onClick={() => void create()}>
            {t('owner.saveTerm')}
          </Button>
        </fieldset>
      )}
    </div>
  );
}
