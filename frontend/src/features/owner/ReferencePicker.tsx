import { useEffect, useId, useRef, useState } from 'react';
import { Field, Button } from '../../../../ui/src/components/primitives';
import { useI18n } from '../../../../ui/src/i18n/I18nProvider';

type Choice = { key: string; title: string; kind: string; version: number };

export function ReferencePicker({
  label,
  value,
  kind,
  locale,
  collectionKey = '',
  excludeKeys = [],
  disabled = false,
  onChange,
}: {
  label: string;
  value: string;
  kind: 'learning' | 'docs' | 'lesson' | 'doc' | 'learning_item' | 'docs_item';
  locale: string;
  collectionKey?: string;
  excludeKeys?: string[];
  disabled?: boolean;
  onChange: (key: string, choice?: Choice) => void;
}) {
  const { t } = useI18n();
  const listId = useId();
  const input = useRef<HTMLInputElement>(null);
  const [query, setQuery] = useState(value);
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(0);
  const [attempt, setAttempt] = useState(0);
  const [result, setResult] = useState<{
    identity: string;
    items: Choice[];
    more?: boolean;
    error?: string;
  }>();
  const params = new URLSearchParams({
    locale,
    kind,
    q: query,
    collection_key: /^[a-z0-9][a-z0-9_-]{0,119}$/.test(collectionKey) ? collectionKey : '',
  });
  for (const key of excludeKeys) {
    params.append('exclude_key', key);
  }
  const identity = params.toString();
  useEffect(() => {
    setQuery(value);
  }, [value]);
  useEffect(() => {
    if (!open) {
      return;
    }
    const controller = new AbortController();
    let current = true;
    const timer = setTimeout(() => {
      fetch('/api/v1/owner/references?' + identity, {
        credentials: 'same-origin',
        cache: 'no-store',
        signal: controller.signal,
      })
        .then(async (response) => {
          if (!response.ok) {
            if (
              response.status === 404 ||
              (response.status === 422 && !response.headers?.get('X-Reference-Search-Version'))
            ) {
              throw 'picker.restartBackend';
            }
            throw response.status;
          }
          return response.json() as Promise<{ items: Choice[]; more: boolean }>;
        })
        .then((data) => {
          if (current) {
            setResult({ identity, items: data.items.slice(0, 5), more: data.more });
            setActive(0);
          }
        })
        .catch((error) => {
          if (current) {
            setResult({
              identity,
              items: [],
              error:
                error === 'picker.restartBackend'
                  ? error
                  : error === 422
                    ? 'request.invalid'
                    : error === 401
                      ? 'request.session'
                      : error === 503
                        ? 'request.unavailable'
                        : typeof error === 'number'
                          ? 'request.server'
                          : 'request.network',
            });
          }
        });
    }, 150);
    return () => {
      current = false;
      clearTimeout(timer);
      controller.abort();
    };
  }, [identity, open, attempt]);
  const data = result?.identity === identity ? result : undefined;
  function choose(choice: Choice) {
    setQuery(choice.key);
    onChange(choice.key, choice);
    input.current?.focus();
    setOpen(false);
  }
  return (
    <div
      className="reference-picker"
      onBlur={(event) => {
        if (!event.currentTarget.contains(event.relatedTarget as Node | null)) {
          setOpen(false);
        }
      }}
    >
      <Field label={label} help={t('picker.help')}>
        <input
          ref={input}
          autoComplete="off"
          name="collection-reference"
          role="combobox"
          aria-autocomplete="list"
          aria-expanded={open}
          aria-controls={open && !data?.error ? listId : undefined}
          aria-activedescendant={open && data?.items[active] ? listId + '-' + active : undefined}
          value={query}
          disabled={disabled}
          maxLength={200}
          onFocus={() => setOpen(true)}
          onChange={(event) => {
            setQuery(event.target.value);
            onChange(event.target.value);
            setOpen(true);
            setActive(0);
          }}
          onKeyDown={(event) => {
            const items = data?.items || [];
            if (event.key === 'Escape') {
              setOpen(false);
              event.preventDefault();
            } else if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
              event.preventDefault();
              setOpen(true);
              if (items.length) {
                setActive(
                  (n) => (n + (event.key === 'ArrowDown' ? 1 : items.length - 1)) % items.length,
                );
              }
            } else if (event.key === 'Enter' && open && items[active]) {
              event.preventDefault();
              choose(items[active]);
            }
          }}
        />
      </Field>
      {open && (
        <div className="reference-results">
          {!data && <p role="status">{t('public.loading')}</p>}
          {data?.error ? (
            <>
              <p role="alert">{t(data.error)}</p>
              <Button onClick={() => setAttempt((n) => n + 1)}>{t('public.retry')}</Button>
            </>
          ) : (
            <>
              <ul id={listId} role="listbox" aria-label={label}>
                {data?.items.map((item, index) => (
                  <li role="presentation" key={item.key}>
                    <button
                      id={listId + '-' + index}
                      type="button"
                      role="option"
                      aria-selected={active === index}
                      tabIndex={-1}
                      disabled={disabled}
                      onMouseDown={(event) => event.preventDefault()}
                      onClick={() => choose(item)}
                    >
                      <strong dir="auto">{item.title || item.key}</strong>
                      <bdi>{item.key}</bdi>
                    </button>
                  </li>
                ))}
              </ul>
              {data && !data.items.length && <p role="status">{t('picker.empty')}</p>}
              {data?.more && <p className="vb-help">{t('picker.more')}</p>}
            </>
          )}
        </div>
      )}
    </div>
  );
}
