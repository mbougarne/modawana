import { useState } from 'react';
import { Button, Field } from '../../../../../ui/src/components/primitives';
import { useI18n } from '../../../../../ui/src/i18n/I18nProvider';
import type { Locale } from '../../../api/content';
import { AuthError, call } from '../../../features/auth/api';
import { editorialAPI, type EditorialConfig } from '../../../api/editorial';
import { useResource } from '../../public/api';

const options = ['summary', 'seo', 'alt_text', 'social', 'translation'] as const;
const sourceFields = ['title', 'summary', 'excerpt', 'cover'] as const;
type Kind = (typeof options)[number];
type SourceField = (typeof sourceFields)[number];

interface EditorialSource {
  key: string;
  locale: Locale;
  revision_id: string;
  title: string;
  summary: string;
  excerpt?: string;
  cover?: string;
  cover_id?: string;
}

interface Output {
  text: string;
  description: string;
}

export interface Suggestion extends Output {
  kind: Kind;
  field: SourceField;
  coverID?: string;
}

interface Props {
  itemKey: string;
  locale: Locale;
  sourceLocale: Locale;
  canApply: (suggestion: Suggestion) => boolean;
  apply: (suggestion: Suggestion) => void;
  csrf: string;
}

export function EditorialPanel(props: Props) {
  const settings = useResource<EditorialConfig>(editorialAPI);
  if (!settings.data?.enabled) {
    return null;
  }
  return <SuggestionForm {...props} />;
}

function SuggestionForm({ itemKey, locale, sourceLocale, canApply, apply, csrf }: Props) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const [kind, setKind] = useState<Kind>('summary');
  const [fields, setFields] = useState<SourceField[]>([]);
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState('');
  const [suggestion, setSuggestion] = useState<Suggestion>();
  const language = kind === 'translation' ? sourceLocale : locale;
  const source = useResource<EditorialSource>(
    open
      ? editorialAPI + '/context?' + new URLSearchParams({ key: itemKey, locale: language })
      : undefined,
  );
  const sourceData = source.data;
  const canSuggest =
    fields.length > 0 &&
    fields.every((field) => !!sourceData?.[field]) &&
    (kind !== 'alt_text' || fields.includes('cover'));

  function pick(field: SourceField) {
    setSuggestion(undefined);
    setNotice('');
    setFields((old) =>
      old.includes(field)
        ? old.filter((candidate) => candidate !== field)
        : kind === 'translation'
          ? [field]
          : [...old, field],
    );
  }

  async function generate() {
    if (!sourceData || !canSuggest || busy) {
      return;
    }
    setBusy(true);
    setNotice('');
    setSuggestion(undefined);
    try {
      const out = await call<Output>(
        '/owner/editorial/suggest',
        'POST',
        {
          kind,
          key: itemKey,
          locale: language,
          revision_id: sourceData.revision_id,
          target_locale: locale,
          fields,
        },
        csrf,
      );
      setSuggestion({ ...out, kind, field: fields[0], coverID: sourceData.cover_id });
    } catch (error) {
      const codes = [
        'editorial_disabled',
        'editorial_key',
        'editorial_quota',
        'editorial_busy',
        'editorial_timeout',
        'editorial_provider',
        'editorial_provider_limit',
        'editorial_output',
        'revision_conflict',
      ];
      setNotice(
        error instanceof AuthError && codes.includes(error.code)
          ? 'editorial.error.' + error.code
          : 'editorial.failed',
      );
    } finally {
      setBusy(false);
    }
  }

  return (
    <details
      className="owner-panel editorial-panel"
      onToggle={(event) => setOpen(event.currentTarget.open)}
    >
      <summary>{t('owner.editorial')}</summary>
      <p>{t('editorial.contextHelp')}</p>
      <fieldset className="owner-fields" disabled={busy}>
        <Field label={t('editorial.task')}>
          <select
            value={kind}
            onChange={(event) => {
              const value = options.find((option) => option === event.target.value);
              if (value) {
                setKind(value);
                setFields([]);
                setSuggestion(undefined);
                setNotice('');
              }
            }}
          >
            {options
              .filter((option) => option !== 'translation' || sourceLocale !== locale)
              .map((option) => (
                <option key={option} value={option}>
                  {t('editorial.kind.' + option)}
                </option>
              ))}
          </select>
        </Field>
        {source.loading && <p role="status">{t('public.loading')}</p>}
        {source.error && <p>{t('editorial.sourceUnavailable')}</p>}
        {sourceData && (
          <>
            {sourceFields
              .filter((field) => sourceData[field] && (field !== 'cover' || kind === 'alt_text'))
              .map((field) => (
                <div className="editorial-context" key={field}>
                  <label>
                    <input
                      type="checkbox"
                      checked={fields.includes(field)}
                      onChange={() => pick(field)}
                    />{' '}
                    {t('editorial.field.' + field)}
                  </label>
                  {field === 'cover' ? (
                    <img src={sourceData.cover} alt={t('editorial.field.cover')} />
                  ) : (
                    <p dir="auto" lang={sourceData.locale}>
                      {sourceData[field]}
                    </p>
                  )}
                </div>
              ))}
            {kind === 'alt_text' && !sourceData.cover && <p>{t('editorial.coverRequired')}</p>}
            <Button onClick={() => void generate()} disabled={!canSuggest}>
              {t('editorial.generate')}
            </Button>
          </>
        )}
      </fieldset>
      {notice && (
        <>
          <p role="alert">{t(notice)}</p>
          {notice === 'editorial.error.revision_conflict' && (
            <Button onClick={source.retry}>{t('editorial.refresh')}</Button>
          )}
        </>
      )}
      {suggestion && (
        <div className="owner-fields">
          <Field label={t('editorial.suggestion')}>
            <textarea
              dir="auto"
              lang={locale}
              rows={4}
              value={suggestion.text}
              onChange={(event) => setSuggestion({ ...suggestion, text: event.target.value })}
            />
          </Field>
          {suggestion.kind === 'seo' && (
            <Field label={t('editorial.description')}>
              <textarea
                dir="auto"
                lang={locale}
                rows={3}
                value={suggestion.description}
                onChange={(event) =>
                  setSuggestion({ ...suggestion, description: event.target.value })
                }
              />
            </Field>
          )}
          {canApply(suggestion) && (
            <Button
              variant="primary"
              disabled={!suggestion.text.trim()}
              onClick={() => apply(suggestion)}
            >
              {t(suggestion.kind === 'social' ? 'editorial.toSocial' : 'editorial.apply')}
            </Button>
          )}
          {!canApply(suggestion) && <p>{t('editorial.copyDraft')}</p>}
        </div>
      )}
    </details>
  );
}
