import { useId, useState, type ReactNode } from 'react';
import { useI18n } from '../../../../ui/src/i18n/I18nProvider';
import { call, AuthError } from '../auth/api';

export interface OwnerEntry {
  key: string;
  kind: string;
  source_locale: string;
  locale: string;
  title: string;
  slug: string;
  status: string;
  version: number;
  draft_revision_id: string;
  published_revision_id: string;
}
type Selection = Record<string, OwnerEntry>;
const id = (item: OwnerEntry) => item.key + '/' + item.locale;
const limit = 500;

export function BulkPublication({
  items,
  query,
  csrf,
  loading,
  onApplied,
  children,
}: {
  items: OwnerEntry[];
  query: string;
  csrf: string;
  loading: boolean;
  onApplied: () => void;
  children: (selected: Selection, toggle: (item: OwnerEntry) => void) => ReactNode;
}) {
  const { t } = useI18n();
  const statusLabel = useId();
  const [selected, setSelected] = useState<Selection>({});
  const [operation, setOperation] = useState('publish');
  const [confirm, setConfirm] = useState(false);
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState('');
  const [result, setResult] = useState<{ changed: number; unchanged: number }>();
  const count = Object.keys(selected).length;
  function change(next: Selection) {
    if (Object.keys(next).length > limit) {
      setNotice('bulk.limit');
      return;
    }
    setSelected(next);
    setConfirm(false);
    setResult(undefined);
    setNotice('');
  }
  function toggle(item: OwnerEntry) {
    const next = { ...selected };
    if (next[id(item)]) {
      delete next[id(item)];
    } else {
      next[id(item)] = item;
    }
    change(next);
  }
  async function selectAll() {
    setBusy(true);
    setNotice('');
    setConfirm(false);
    try {
      const next: Selection = {};
      const q = new URLSearchParams(query);
      q.delete('cursor');
      for (;;) {
        const page = await call<{ items: OwnerEntry[]; next_cursor: string }>('/content?' + q);
        for (const item of page.items) {
          next[id(item)] = item;
        }
        if (
          Object.keys(next).length > limit ||
          (Object.keys(next).length === limit && page.next_cursor)
        ) {
          setNotice('bulk.limit');
          return;
        }
        if (!page.next_cursor) {
          break;
        }
        q.set('cursor', page.next_cursor);
      }
      change(next);
    } catch {
      setNotice('bulk.selectionFailed');
    } finally {
      setBusy(false);
    }
  }
  async function apply() {
    setBusy(true);
    setNotice('');
    setResult(undefined);
    try {
      const out = await call<{ changed: number; unchanged: number }>(
        '/content/bulk-publication',
        'POST',
        {
          operation,
          items: Object.values(selected).map((item) => ({
            key: item.key,
            locale: item.locale,
            expected_version: item.version,
            revision_id:
              operation === 'publish'
                ? item.draft_revision_id
                : item.published_revision_id || item.draft_revision_id,
          })),
        },
        csrf,
      );
      setSelected({});
      setResult(out);
      onApplied();
    } catch (e) {
      onApplied();
      setNotice(
        e instanceof AuthError && e.code === 'revision_conflict'
          ? 'bulk.conflict'
          : e instanceof AuthError && e.code === 'invalid_fields'
            ? 'bulk.blocked'
            : e instanceof AuthError && (e.code === 'unauthenticated' || e.code === 'forbidden')
              ? 'config.signIn'
              : 'bulk.uncertain',
      );
    } finally {
      setBusy(false);
      setConfirm(false);
    }
  }
  const pageSelected = items.length > 0 && items.every((item) => !!selected[id(item)]);
  return (
    <fieldset className="owner-fields posts-content" disabled={busy} aria-busy={busy}>
      <section className="posts-bulk" aria-label={t('bulk.actions')}>
        <div className="posts-bulk-row">
          <div className="owner-row">
            <label>
              <input
                type="checkbox"
                disabled={loading || !items.length}
                checked={pageSelected}
                onChange={() => {
                  const next = { ...selected };
                  for (const item of items) {
                    if (pageSelected) {
                      delete next[id(item)];
                    } else {
                      next[id(item)] = item;
                    }
                  }
                  change(next);
                }}
              />
              {t('bulk.selectPage')}
            </label>
            <button
              type="button"
              className="vb-btn vb-btn-secondary vb-btn-sm"
              disabled={loading}
              onClick={() => void selectAll()}
            >
              {t('bulk.selectAll')}
            </button>
            <button
              type="button"
              className="vb-btn vb-btn-quiet vb-btn-sm"
              disabled={!count}
              onClick={() => change({})}
            >
              {t('bulk.clear')}
            </button>
            <span role="status">{t('bulk.selected', { n: count })}</span>
          </div>
          <div className="owner-row">
            <label>
              <span id={statusLabel}>{t('bulk.status')}</span>
              <select
                aria-labelledby={statusLabel}
                value={operation}
                onChange={(e) => {
                  setOperation(e.target.value);
                  setConfirm(false);
                }}
              >
                <option value="publish">{t('bulk.publish')}</option>
                <option value="unpublish">{t('bulk.unpublish')}</option>
              </select>
            </label>
            <button
              type="button"
              className="vb-btn vb-btn-primary"
              disabled={!count}
              onClick={() => setConfirm(true)}
            >
              {t('bulk.apply')}
            </button>
          </div>
        </div>
        <details className="posts-bulk-help">
          <summary>{t('workspace.bulkHelp')}</summary>
          <p className="vb-help">{t('bulk.help')}</p>
        </details>
        {confirm && (
          <section className="owner-panel" aria-label={t('bulk.confirm')}>
            <h2>{t('bulk.confirm')}</h2>
            <p>
              {t(operation === 'publish' ? 'bulk.confirmPublish' : 'bulk.confirmUnpublish', {
                n: count,
              })}
            </p>
            <details>
              <summary>{t('bulk.selection')}</summary>
              <ul className="bulk-selection">
                {Object.values(selected).map((item) => (
                  <li key={id(item)}>
                    {item.title} · <bdi>{item.locale}</bdi>
                  </li>
                ))}
              </ul>
            </details>
            <div className="owner-row">
              <button type="button" className="vb-btn vb-btn-primary" onClick={() => void apply()}>
                {t('bulk.confirmApply')}
              </button>
              <button
                type="button"
                className="vb-btn vb-btn-secondary"
                onClick={() => setConfirm(false)}
              >
                {t('config.cancel')}
              </button>
            </div>
          </section>
        )}
        {notice && (
          <p role="alert" className="workspace-alert" data-tone="error">
            {t(notice)}
          </p>
        )}
        {result && (
          <p role="status" className="workspace-alert" data-tone="success">
            {t('bulk.result', { changed: result.changed, unchanged: result.unchanged })}
          </p>
        )}
      </section>
      {children(selected, toggle)}
    </fieldset>
  );
}
