import { useState } from 'react';
import { Button, Field, useI18n } from '@ui';
import { call, requestErrorKey, type State } from './api';
import { useResource } from '../../pages/public/api';
import { ResourceState } from '../../pages/public/ResourceState';
import styles from './auth.module.css';

interface Token {
  id: string;
  label: string;
  scopes: string[];
  collections: string[];
  allow_articles: boolean;
}

export function OAuthPage() {
  const { t, locale } = useI18n();
  const id = new URLSearchParams(window.location.search).get('id') || '';
  const request = useResource<{ redirect_uri: string }>(
    '/api/v1/security/oauth?id=' + encodeURIComponent(id),
  );
  const tokens = useResource<Token[]>('/api/v1/security/publisher-tokens');
  const [selected, setSelected] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const token = tokens.data?.find((item) => item.id === selected);
  async function decide(approve: boolean) {
    setBusy(true);
    setError('');
    try {
      const session = await call<State>('/session');
      const result = await call<{ redirect: string }>(
        '/security/oauth',
        'POST',
        { id, token_id: selected, approve },
        session.csrf,
      );
      const target = new URL(result.redirect);
      if (
        target.origin !== 'https://chatgpt.com' ||
        !/^\/(connector_platform_oauth_redirect|connector\/oauth\/[A-Za-z0-9_-]+)$/.test(
          target.pathname,
        )
      ) {
        throw new Error();
      }
      window.location.assign(target.href);
    } catch (failure) {
      setError(requestErrorKey(failure));
      setBusy(false);
    }
  }
  return (
    <main className={styles.main}>
      <h1>{t('oauth.title')}</h1>
      <p>{t('oauth.help')}</p>
      <a href={`/${locale}/admin/security`}>{t('publisher.title')}</a>
      <ResourceState resource={request} />
      <ResourceState resource={tokens} />
      {error && <p role="alert">{t(error)}</p>}
      {request.data && tokens.data && (
        <section className={styles.panel}>
          <p>
            {t('oauth.destination')} <bdi>{request.data.redirect_uri}</bdi>
          </p>
          <Field label={t('oauth.permission')}>
            <select
              value={selected}
              disabled={busy}
              onChange={(event) => setSelected(event.target.value)}
            >
              <option value="">{t('oauth.choose')}</option>
              {tokens.data.map((item) => (
                <option key={item.id} value={item.id}>
                  {item.label}
                </option>
              ))}
            </select>
          </Field>
          {token && (
            <>
              <p>{token.scopes.map((scope) => t('publisher.' + scope)).join(' · ')}</p>
              <p>
                {t('publisher.articles')}: {t(token.allow_articles ? 'oauth.yes' : 'oauth.no')}
              </p>
              <p>
                {t('publisher.collections')}:{' '}
                <bdi>{token.collections.join(', ') || t('oauth.none')}</bdi>
              </p>
            </>
          )}
          <p>{t('oauth.revoke')}</p>
          <div className={styles.actions}>
            <Button disabled={busy || !token} onClick={() => void decide(true)}>
              {t('oauth.allow')}
            </Button>
            <Button disabled={busy} onClick={() => void decide(false)}>
              {t('oauth.deny')}
            </Button>
          </div>
        </section>
      )}
    </main>
  );
}
