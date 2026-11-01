import { useState } from 'react';
import { Button, Field, TextInput } from '../../../../../ui/src/components/primitives';
import { FileDropInput } from '../../../../../ui/src/components/FileDropInput';
import { Icon } from '../../../../../ui/src/components/Icon';
import { useI18n, Label } from '../../../../../ui/src/i18n/I18nProvider';
import {
  DEFAULT_LABELS,
  type LabelOverrides,
  type L,
  type LabelText,
} from '../../../../../ui/src/i18n/labels';
import type { Locale } from '../../../api/content';

const languages: Locale[] = ['en', 'fr', 'ar'];
const pageSize = 30;

export function LabelsForm({
  value,
  saved = {},
  onChange,
  onImport,
}: {
  value: LabelOverrides;
  saved?: LabelOverrides;
  onChange: (v: LabelOverrides) => void;
  onImport: (v: unknown) => Promise<void>;
}) {
  const { t } = useI18n();
  const [search, setSearch] = useState('');
  const [section, setSection] = useState('');
  const [missing, setMissing] = useState(false);
  const [page, setPage] = useState(0);
  const [key, setKey] = useState('');
  const [translations, setTranslations] = useState({ en: '', fr: '', ar: '' });
  const [error, setError] = useState(false);
  const base = DEFAULT_LABELS as Record<string, L>;
  const keys = [...new Set([...Object.keys(base), ...Object.keys(value)])].sort();
  const effective = (k: string, l: Locale): LabelText | undefined =>
    Object.hasOwn(value[k] ?? {}, l) ? value[k]?.[l] : base[k]?.[l];
  const isMissing = (k: string, l: Locale) => {
    const v = effective(k, l);
    return !v || (typeof v === 'object' && !v.other);
  };
  function set(k: string, l: Locale, v: LabelText) {
    onChange({ ...value, [k]: { ...value[k], [l]: v } });
  }
  function reset(k: string, l: Locale) {
    const out = structuredClone(value);
    delete out[k]?.[l];
    if (out[k] && !Object.keys(out[k]).length) {
      delete out[k];
    }
    onChange(out);
  }
  const matches = keys.filter(
    (k) =>
      (!section || k.split('.')[0] === section) &&
      (!missing || languages.some((l) => isMissing(k, l))) &&
      (!search ||
        (k + JSON.stringify({ ...base[k], ...value[k] }))
          .toLocaleLowerCase()
          .includes(search.toLocaleLowerCase())),
  );
  const pages = Math.max(1, Math.ceil(matches.length / pageSize));
  const currentPage = Math.min(page, pages - 1);
  const visible = matches.slice(currentPage * pageSize, (currentPage + 1) * pageSize);
  function download() {
    const blob = new Blob([JSON.stringify(value, null, 2) + '\n'], { type: 'application/json' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = 'interface-labels.json';
    a.click();
    URL.revokeObjectURL(url);
  }
  async function upload(f: File) {
    setError(false);
    try {
      if (f.size > 262144) {
        throw Error();
      }
      await onImport(JSON.parse(await f.text()));
      setPage(0);
    } catch {
      setError(true);
    }
  }
  return (
    <div className="labels-editor">
      <section className="vb-card labels-toolbar" aria-label={t('labels.filters')}>
        <div className="vb-ltools">
          <label className="labels-search">
            <span className="workspace-search-input">
              <Icon name="search" />
              <input
                type="search"
                aria-label={t('labels.search')}
                placeholder={t('labels.search')}
                value={search}
                onChange={(e) => {
                  setSearch(e.target.value);
                  setPage(0);
                }}
              />
            </span>
          </label>
          <select
            className="vb-select"
            aria-label={t('labels.section')}
            value={section}
            onChange={(e) => {
              setSection(e.target.value);
              setPage(0);
            }}
          >
            <option value="">{t('labels.all')}</option>
            {[...new Set(keys.map((k) => k.split('.')[0]))].map((s) => (
              <option key={s} value={s}>
                {s}
              </option>
            ))}
          </select>
          <label className="vb-check">
            <input
              type="checkbox"
              checked={missing}
              onChange={(e) => {
                setMissing(e.target.checked);
                setPage(0);
              }}
            />
            <Label name="labels.missing" />
          </label>
        </div>
        <div className="vb-lstats" aria-label={t('labels.translationStatus')}>
          {languages.map((l) => {
            const count = keys.filter((k) => isMissing(k, l)).length;
            return (
              <span key={l} data-missing={count > 0}>
                <bdi>{t('language.' + l)}</bdi>:{' '}
                {count > 0 ? t('labels.missingCount', { n: count }) : t('labels.complete')}
              </span>
            );
          })}
        </div>
        <p className="vb-meta" role="status">
          {t('labels.count', { n: matches.length })}
        </p>
      </section>
      {error && (
        <p role="alert" className="config-notice">
          <Label name="config.invalid" />
        </p>
      )}
      {visible.length > 0 ? (
        <div
          className="vb-ltable"
          role="table"
          aria-label={t('labels.table')}
          aria-rowcount={matches.length + 1}
        >
          <div className="vb-lrow vb-lhead" role="row">
            <span role="columnheader">{t('labels.key')}</span>
            {languages.map((l) => (
              <span key={l} role="columnheader">
                {t('language.' + l)}
              </span>
            ))}
          </div>
          {visible.map((k, index) => {
            const en = effective(k, 'en') || base[k]?.en;
            const plural = typeof (base[k]?.en ?? en) === 'object';
            return (
              <div
                className="vb-lrow"
                role="row"
                aria-rowindex={currentPage * pageSize + index + 2}
                key={k}
              >
                <div className="vb-lkey" role="cell">
                  <code dir="ltr">{k}</code>
                  <small title={t(base[k] ? 'labels.coreHelp' : 'labels.customHelp')}>
                    {k.split('.')[0]}
                  </small>
                </div>
                {languages.map((l) => {
                  const v = effective(k, l);
                  const changed = JSON.stringify(value[k]?.[l]) !== JSON.stringify(saved[k]?.[l]);
                  const forms =
                    l === 'ar' ? ['zero', 'one', 'two', 'few', 'many', 'other'] : ['one', 'other'];
                  return (
                    <div
                      role="cell"
                      key={l}
                      className="labels-cell ds-ui"
                      lang={l}
                      dir={l === 'ar' ? 'rtl' : 'ltr'}
                      data-changed={changed}
                    >
                      <span className="lang">{t('language.' + l)}</span>
                      {plural ? (
                        <details className="labels-plural">
                          <summary>
                            {t('labels.pluralForms')}
                            {isMissing(k, l) && ' · ' + t('config.fallback')}
                          </summary>
                          {v !== '' &&
                            forms.map((form) => (
                              <Field key={form} label={t('plural.' + form)}>
                                <TextInput
                                  lang={l}
                                  dir={l === 'ar' ? 'rtl' : 'ltr'}
                                  maxLength={1000}
                                  value={
                                    typeof v === 'object' ? (v[form as keyof typeof v] ?? '') : ''
                                  }
                                  onChange={(e) =>
                                    set(k, l, {
                                      ...(typeof v === 'object' ? v : { other: '' }),
                                      [form]: e.target.value,
                                    })
                                  }
                                />
                              </Field>
                            ))}
                        </details>
                      ) : typeof v === 'string' && v.includes('\n') ? (
                        <textarea
                          className="vb-input"
                          aria-label={`${k} · ${t('language.' + l)}`}
                          lang={l}
                          dir={l === 'ar' ? 'rtl' : 'ltr'}
                          data-missing={isMissing(k, l)}
                          data-changed={changed}
                          maxLength={1000}
                          rows={2}
                          value={v}
                          onChange={(e) => set(k, l, e.target.value)}
                        />
                      ) : (
                        <TextInput
                          className="vb-input"
                          aria-label={`${k} · ${t('language.' + l)}`}
                          lang={l}
                          dir={l === 'ar' ? 'rtl' : 'ltr'}
                          data-missing={isMissing(k, l)}
                          data-changed={changed}
                          maxLength={1000}
                          value={typeof v === 'string' ? v : ''}
                          placeholder={isMissing(k, l) && typeof en === 'string' ? en : undefined}
                          onChange={(e) => set(k, l, e.target.value)}
                        />
                      )}
                      <details className="labels-cell-options">
                        <summary>{t('labels.options')}</summary>
                        {isMissing(k, l) && <p className="vb-help">{t('config.fallback')}</p>}
                        {l !== 'en' && (
                          <label className="vb-check">
                            <input
                              type="checkbox"
                              checked={v === ''}
                              onChange={(e) => (e.target.checked ? set(k, l, '') : reset(k, l))}
                            />
                            {t('labels.useEnglish')}
                          </label>
                        )}
                        <Button size="sm" variant="quiet" onClick={() => reset(k, l)}>
                          <Label name="labels.reset" />
                        </Button>
                      </details>
                    </div>
                  );
                })}
              </div>
            );
          })}
        </div>
      ) : (
        <p className="vb-empty">{t('labels.empty')}</p>
      )}
      {pages > 1 && (
        <nav className="labels-pagination" aria-label={t('labels.pagination')}>
          <Button size="sm" disabled={currentPage === 0} onClick={() => setPage(currentPage - 1)}>
            {t('labels.previous')}
          </Button>
          <span className="vb-meta">{t('labels.page', { page: currentPage + 1, pages })}</span>
          <Button
            size="sm"
            disabled={currentPage + 1 === pages}
            onClick={() => setPage(currentPage + 1)}
          >
            {t('labels.next')}
          </Button>
        </nav>
      )}
      <details className="vb-card labels-add">
        <summary>{t('labels.add')}</summary>
        <div className="labels-add-fields">
          <Field label={t('labels.key')}>
            <TextInput
              dir="ltr"
              value={key}
              maxLength={160}
              onChange={(e) => setKey(e.target.value)}
            />
          </Field>
          {languages.map((l) => (
            <Field key={l} label={t('language.' + l)}>
              <TextInput
                lang={l}
                dir={l === 'ar' ? 'rtl' : 'ltr'}
                value={translations[l]}
                maxLength={1000}
                onChange={(e) => setTranslations({ ...translations, [l]: e.target.value })}
              />
            </Field>
          ))}
        </div>
        <p className="vb-help">{t('labels.customHelp')}</p>
        <Button
          size="sm"
          onClick={() => {
            if (
              !/^[a-z][a-z0-9-]{0,39}(\.[a-z][a-z0-9-]{0,39}){1,3}$/.test(key) ||
              keys.includes(key) ||
              !translations.en.trim()
            ) {
              setError(true);
              return;
            }
            setError(false);
            onChange({ ...value, [key]: translations });
            setKey('');
            setTranslations({ en: '', fr: '', ar: '' });
          }}
        >
          <Label name="labels.add" />
        </Button>
      </details>
      <details className="vb-card labels-transfer">
        <summary>{t('labels.transfer')}</summary>
        <div className="labels-transfer-controls">
          <Button size="sm" onClick={download}>
            <Label name="labels.export" />
          </Button>
          <FileDropInput
            label={t('labels.import')}
            help={t('labels.importLimit')}
            accept="application/json,.json"
            onFile={(file) => void upload(file)}
          />
        </div>
        <p className="vb-help">
          <Label name="labels.importHelp" />
        </p>
      </details>
    </div>
  );
}
