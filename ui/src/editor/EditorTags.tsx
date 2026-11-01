import { useState } from 'react';
import { useI18n } from '../i18n/I18nProvider';

export function EditorTags({
  value,
  terms,
  onChange,
}: {
  value: string[];
  terms: { key: string; label: string }[];
  onChange: (keys: string[]) => void;
}) {
  const { t } = useI18n();
  const [search, setSearch] = useState('');
  const matches = terms.filter(
    (term) =>
      !value.includes(term.key) &&
      (term.key + ' ' + term.label).toLocaleLowerCase().includes(search.toLocaleLowerCase()),
  );
  function add(key: string) {
    if (value.length < 8 && !value.includes(key)) {
      onChange([...value, key]);
      setSearch('');
    }
  }
  return (
    <div className="editor-tags">
      <div className="vb-taginput">
        {value.map((key) => {
          const label = terms.find((term) => term.key === key)?.label || key;
          return (
            <span className="vb-tok" key={key}>
              <bdi>{label}</bdi>
              <button
                type="button"
                aria-label={t('tag.remove', { tag: label })}
                onClick={() => onChange(value.filter((item) => item !== key))}
              >
                ×
              </button>
            </span>
          );
        })}
        <input
          type="search"
          aria-label={t('editor.findTags')}
          placeholder={t('editor.findTags')}
          value={search}
          onChange={(event) => setSearch(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === 'Enter') {
              event.preventDefault();
              if (matches.length === 1) {
                add(matches[0].key);
              }
            }
          }}
        />
      </div>
      <div className="vb-suggest">
        {matches.slice(0, 5).map((term) => (
          <button
            type="button"
            key={term.key}
            disabled={value.length >= 8}
            onClick={() => add(term.key)}
          >
            + <bdi>{term.label}</bdi>
          </button>
        ))}
      </div>
      {matches.length > 5 && <p className="vb-help">{t('picker.more')}</p>}
      {search && !matches.length && <p role="status">{t('picker.empty')}</p>}
      <p className="vb-help">
        {t('tag.max', { n: 8 })} · {value.length}/8
      </p>
    </div>
  );
}
