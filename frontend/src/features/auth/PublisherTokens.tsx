import { ValidationIssues } from '../configuration/ValidationIssues';
import { useState } from 'react';
import { Button, Field, useI18n } from '@ui';
import { call, AuthError, requestErrorKey, type FieldIssue } from './api';
import { useResource } from '../../pages/public/api';
import { ResourceState } from '../../pages/public/ResourceState';
import styles from './auth.module.css';

interface Token {
  id: string;
  label: string;
  scopes: string[];
  collections: string[];
  allow_articles: boolean;
  expires_at: string;
  secret?: string;
}

export function PublisherTokens({ csrf }: { csrf: string }) {
  const { t, date } = useI18n();
  const resource = useResource<Token[]>('/api/v1/security/publisher-tokens');
  const [label, setLabel] = useState('');
  const [collections, setCollections] = useState('');
  const [articles, setArticles] = useState(false);
  const [scopes, setScopes] = useState(['draft']);
  const [secret, setSecret] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [issues, setIssues] = useState<FieldIssue[]>([]);
  async function run(action: () => Promise<void>) {
    setBusy(true);
    setError(null);
    setIssues([]);
    try {
      await action();
      resource.retry();
    } catch (error) {
      setError(requestErrorKey(error));
      setIssues(error instanceof AuthError ? error.fields : []);
    } finally {
      setBusy(false);
    }
  }
  return (
    <section className={styles.panel}>
      <h2>{t('publisher.title')}</h2>
      <p>{t('publisher.help')}</p>
      <ResourceState resource={resource} />
      <fieldset disabled={busy}>
        <Field label={t('publisher.label')}>
          <input
            data-config-path="security.token_label"
            value={label}
            maxLength={120}
            onChange={(e) => setLabel(e.target.value)}
          />
        </Field>
        <Field label={t('publisher.collections')}>
          <input
            data-config-path="security.token_collections"
            value={collections}
            onChange={(e) => setCollections(e.target.value)}
          />
        </Field>
        <label>
          <input
            type="checkbox"
            checked={articles}
            onChange={(e) => setArticles(e.target.checked)}
          />
          {t('publisher.articles')}
        </label>
        {['draft', 'media', 'quiz', 'publish'].map((scope) => (
          <label key={scope} className={styles.actions}>
            <input
              type="checkbox"
              checked={scopes.includes(scope)}
              onChange={(e) =>
                setScopes((old) =>
                  e.target.checked ? [...old, scope] : old.filter((s) => s !== scope),
                )
              }
            />
            {t('publisher.' + scope)}
          </label>
        ))}
        <Button
          disabled={!label.trim() || !scopes.length || (!collections.trim() && !articles)}
          onClick={() =>
            void run(async () => {
              const result = await call<Token>(
                '/security/publisher-tokens',
                'POST',
                {
                  label,
                  scopes,
                  collections: collections
                    .split(',')
                    .map((s) => s.trim())
                    .filter(Boolean),
                  allow_articles: articles,
                  days: 90,
                },
                csrf,
              );
              setSecret(result.secret || '');
            })
          }
        >
          {t('publisher.create')}
        </Button>
      </fieldset>
      {secret && (
        <div>
          <p>{t('publisher.once')}</p>
          <Field label={t('publisher.secret')}>
            <textarea readOnly value={secret} dir="ltr" autoComplete="off" />
          </Field>
          <Button onClick={() => setSecret('')}>{t('publisher.dismiss')}</Button>
        </div>
      )}
      {issues.length ? (
        <ValidationIssues issues={issues} />
      ) : (
        error && <p role="alert">{t(error)}</p>
      )}
      <ul className={styles.list}>
        {resource.data?.map((token) => (
          <li key={token.id}>
            <div>
              <strong>{token.label}</strong>
              <p>
                {token.scopes.map((s) => t('publisher.' + s)).join(' · ')} ·{' '}
                <bdi>{token.collections.join(', ')}</bdi>
                {token.allow_articles ? ' · ' + t('publisher.articles') : ''}
              </p>
              <p>{t('publisher.expires', { date: date(token.expires_at) })}</p>
            </div>
            <Button
              disabled={busy}
              onClick={() => {
                const next = window.prompt(t('publisher.label'), token.label);
                if (next?.trim()) {
                  void run(async () => {
                    await call(
                      '/security/publisher-tokens',
                      'PATCH',
                      { id: token.id, label: next },
                      csrf,
                    );
                  });
                }
              }}
            >
              {t('publisher.rename')}
            </Button>
            <Button
              disabled={busy}
              onClick={() =>
                void run(async () => {
                  await call('/security/publisher-tokens', 'DELETE', { id: token.id }, csrf);
                })
              }
            >
              {t('publisher.revoke')}
            </Button>
          </li>
        ))}
      </ul>
    </section>
  );
}
