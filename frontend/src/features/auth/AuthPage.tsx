import { Logo } from '@ui';
import { ownerReturnTarget } from './navigation';
import { usePublicConfiguration } from '../configuration/PublicConfiguration';
import { siteBrand } from '../../pages/admin/settings/siteBrand';
import { ValidationIssues } from '../configuration/ValidationIssues';
import { OAuthConnections } from './OAuthConnections';
import { PublisherTokens } from './PublisherTokens';
import { OwnerWorkspace, OwnerPageHeading } from '../owner/OwnerWorkspace';
import { useEffect, useRef, useState, type FormEvent } from 'react';
import { Label, Button, Field, TextInput, useI18n, type LabelKey, DEFAULT_LABELS } from '@ui';
import {
  call,
  AuthError,
  requestErrorKey,
  type FieldIssue,
  type State,
  type Key,
  type Session,
  type Factors,
  type Audit,
} from './api';
import {
  creationOptions,
  requestOptions,
  credentialJSON,
  type CreationJSON,
  type RequestJSON,
} from './webauthn';
import styles from './auth.module.css';

type Mode = 'passkey' | 'password' | 'otp' | 'recovery' | 'grant' | 'enroll';

export function AuthPage() {
  const { t, locale, dir, date } = useI18n();
  const { data: configuration } = usePublicConfiguration();
  const [ceremonyPending, setCeremonyPending] = useState(false);
  const [state, setState] = useState<State>({ state: 'loading' });
  const [mode, setMode] = useState<Mode>('passkey');
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState<LabelKey | string | null>(null);
  const [issues, setIssues] = useState<FieldIssue[]>([]);
  const validationScope = useRef<HTMLElement | null>(null);
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [visible, setVisible] = useState(false);
  const [code, setCode] = useState('');
  const [grant, setGrant] = useState('');
  const [label, setLabel] = useState('');
  const [keys, setKeys] = useState<Key[]>([]);
  const [sessions, setSessions] = useState<Session[]>([]);
  const [factors, setFactors] = useState<Factors | null>(null);
  const [events, setEvents] = useState<Audit[]>([]);
  const [codes, setCodes] = useState<string[]>([]);
  const [seed, setSeed] = useState('');
  const [confirm, setConfirm] = useState('');
  const message = useRef<HTMLDivElement>(null);
  const heading = useRef<HTMLHeadingElement>(null);
  const controller = useRef<AbortController | null>(null);
  const owner = state.state === 'owner';
  function error(e: unknown) {
    setIssues(e instanceof AuthError ? e.fields : []);
    if (e instanceof AuthError) {
      setNotice(
        e.code === 'rate_limited'
          ? 'auth.rate'
          : e.code === 'forbidden'
            ? 'auth.fresh'
            : e.code === 'revision_conflict'
              ? 'auth.conflict'
              : e.code === 'unauthenticated'
                ? owner
                  ? 'request.session'
                  : 'auth.failed'
                : requestErrorKey(e),
      );
    } else if (
      e instanceof DOMException &&
      (e.name === 'NotSupportedError' || e.name === 'SecurityError')
    ) {
      setNotice('auth.unsupported');
    } else if (
      e instanceof DOMException &&
      (e.name === 'NotAllowedError' || e.name === 'AbortError')
    ) {
      setNotice('auth.cancelled');
    } else {
      setNotice('auth.network');
    }
  }
  async function refresh() {
    const current = await call<State>('/session');
    setState(current);
    if (current.state === 'owner') {
      const [k, s, f, a] = await Promise.all([
        call<Key[]>('/security/passkeys'),
        call<Session[]>('/security/sessions'),
        call<Factors>('/security/factors'),
        call<Audit[]>('/security/events'),
      ]);
      setKeys(k);
      setSessions(s);
      setFactors(f);
      setEvents(a);
      setEmail(f.login_email);
    } else if (current.state === 'bootstrap' || current.state === 'recovery') {
      setMode('enroll');
    } else if (current.state === 'password') {
      setMode('otp');
    } else {
      setMode('passkey');
    }
  }
  useEffect(() => {
    refresh().catch(error);
    return () => controller.current?.abort();
  }, []);
  useEffect(() => {
    if (notice) {
      message.current?.focus();
    }
  }, [notice]);
  useEffect(() => {
    heading.current?.focus();
    if (!owner) {
      setCodes([]);
      setSeed('');
    }
  }, [mode, owner]);
  async function run(fn: () => Promise<void>) {
    if (busy) {
      return;
    }
    validationScope.current = document.activeElement?.closest('form') || null;
    setIssues([]);
    setBusy(true);
    setNotice(null);
    try {
      await fn();
    } catch (e) {
      error(e);
    } finally {
      controller.current = null;
      setCeremonyPending(false);
      setBusy(false);
    }
  }
  function finishLogin() {
    const target = ownerReturnTarget(new URLSearchParams(window.location.search).get('return'));
    if (target) {
      window.location.assign(target);
    } else {
      history.replaceState(null, '', `/${locale}/admin/security`);
    }
  }
  async function start() {
    const next = await call<State>('/auth/start', 'POST', {}, undefined, { 'X-Blog-Request': '1' });
    return next.csrf!;
  }
  async function passkey(register = false) {
    if (
      !window.PublicKeyCredential ||
      !navigator.credentials ||
      typeof navigator.credentials.create !== 'function' ||
      typeof navigator.credentials.get !== 'function' ||
      !window.isSecureContext
    ) {
      setNotice('auth.unsupported');
      return;
    }
    const token =
      register && (state.state === 'bootstrap' || state.state === 'recovery')
        ? state.csrf!
        : await start();
    const base = '/auth/passkeys/' + (register ? 'register' : 'login');
    controller.current = new AbortController();
    setCeremonyPending(true);
    const options = await call<{ publicKey: CreationJSON & RequestJSON }>(
      base + '/options',
      'POST',
      {},
      token,
    );
    const credential = (await (register
      ? navigator.credentials.create({
          publicKey: creationOptions(options.publicKey),
          signal: controller.current.signal,
        })
      : navigator.credentials.get({
          publicKey: requestOptions(options.publicKey),
          signal: controller.current.signal,
        }))) as PublicKeyCredential | null;
    if (!credential) {
      throw new DOMException('Cancelled', 'NotAllowedError');
    }
    setCeremonyPending(false);
    await call<State>(
      base + '/verify',
      'POST',
      credentialJSON(credential),
      token,
      register ? { 'X-Passkey-Label': encodeURIComponent(label) } : {},
    );
    if (register) {
      setNotice('auth.testNew');
      setLabel('');
      setMode('passkey');
      setState({ state: 'anonymous' });
      setCodes([]);
      setSeed('');
    } else {
      await refresh();
      finishLogin();
    }
  }
  async function submit(event: FormEvent) {
    event.preventDefault();
    await run(async () => {
      if (mode === 'enroll') {
        await passkey(true);
        return;
      }
      if (mode === 'grant') {
        const token = await start();
        const next = await call<State>('/auth/grant', 'POST', { grant, email }, token);
        setGrant('');
        setState({ ...next, state: 'bootstrap' });
        setMode('enroll');
        return;
      }
      if (mode === 'password') {
        const token = await start();
        const next = await call<State>('/session', 'POST', { email, password }, token);
        setPassword('');
        setState(next);
        setMode('otp');
        return;
      }
      const token = state.csrf || (await start());
      const next = await call<State>(
        mode === 'otp' ? '/session/totp' : '/session/recovery',
        'POST',
        { code },
        token,
      );
      setCode('');
      setState(next);
      if (next.state === 'owner') {
        await refresh();
        finishLogin();
      } else {
        setMode('enroll');
      }
    });
  }
  function back() {
    setMode('passkey');
    setPassword('');
    setCode('');
    setGrant('');
    setNotice(null);
    setState({ state: 'anonymous' });
  }
  async function signout() {
    await call('/session', 'DELETE', {}, state.csrf);
    setCodes([]);
    setSeed('');
    setPassword('');
    setState({ state: 'anonymous' });
    setMode('passkey');
    history.replaceState(null, '', `/${locale}/login`);
  }
  async function mutate(path: string, method: string, body?: unknown) {
    await call(path, method, body, state.csrf);
    await refresh();
    setNotice('auth.saved');
  }
  const passwordField = (
    <>
      <Field label={t(owner ? 'auth.newPassword' : 'auth.password')}>
        <TextInput
          data-testid="password"
          data-config-path="security.password"
          type={visible ? 'text' : 'password'}
          autoComplete={owner ? 'new-password' : 'current-password'}
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          required
          minLength={owner ? 12 : undefined}
          maxLength={1024}
        />
      </Field>
      <Button aria-pressed={visible} onClick={() => setVisible(!visible)}>
        {t(visible ? 'auth.hidePassword' : 'auth.showPassword')}
      </Button>
    </>
  );
  const otpField = (
    <Field label={t('auth.code')}>
      <TextInput
        data-testid="otp"
        data-config-path="security.code"
        value={code}
        onChange={(e) => setCode(e.target.value.replace(/\D/g, '').slice(0, 6))}
        inputMode="numeric"
        autoComplete="one-time-code"
        pattern="[0-9]{6}"
        maxLength={6}
        required
        dir="ltr"
      />
    </Field>
  );
  const content = (
    <>
      {owner ? (
        <OwnerPageHeading
          title={t('auth.security')}
          description={t('workspace.securityHelp')}
          headingRef={heading}
        />
      ) : (
        <h1 ref={heading} tabIndex={-1} className="ds-title">
          {t('auth.title')}
        </h1>
      )}
      {notice && !issues.length && (
        <div ref={message} tabIndex={-1} role="status" className={styles.notice}>
          {t(notice)}
        </div>
      )}
      <ValidationIssues issues={issues} scope={validationScope} />
      {busy && (
        <div role="status">
          <Label name="auth.busy" />{' '}
          {ceremonyPending && (
            <Button onClick={() => controller.current?.abort()}>
              <Label name="auth.cancel" />
            </Button>
          )}
        </div>
      )}
      {state.state === 'loading' ? (
        <p>
          <Label name="auth.busy" />
        </p>
      ) : owner ? (
        <>
          <div className="owner-security-grid">
            <div className={styles.panel}>
              <p className={styles.help}>
                <Label name="auth.backupHelp" />
              </p>
              <div className={styles.actions}>
                <Button disabled={busy} onClick={() => run(() => passkey())}>
                  <Label name="auth.reauth" />
                </Button>
                <Button
                  disabled={busy}
                  onClick={() => {
                    setState({ state: 'anonymous' });
                    setMode('password');
                    setCodes([]);
                    setSeed('');
                  }}
                >
                  <Label name="auth.passwordOption" />
                </Button>
              </div>
            </div>
            <section className={styles.panel} data-config-path="security.passkeys">
              <h2>
                <Label name="auth.keys" />
              </h2>
              <ul className={styles.list}>
                {keys.map((key) => (
                  <li className={styles.item} key={key.id}>
                    <form
                      className={styles.form}
                      onSubmit={(e) => {
                        e.preventDefault();
                        const value = new FormData(e.currentTarget).get('label');
                        run(() =>
                          mutate('/security/passkeys', 'PATCH', { id: key.id, label: value }),
                        );
                      }}
                    >
                      <Field label={t('auth.keyLabel')}>
                        <TextInput
                          data-config-path="security.label"
                          name="label"
                          defaultValue={key.label}
                          maxLength={120}
                          required
                        />
                      </Field>
                      <p className={styles.help}>
                        {t(key.tested_at ? 'auth.tested' : 'auth.untested')} ·{' '}
                        {t('auth.created', { date: date(key.created_at) })}
                        {key.last_used_at && (
                          <> · {t('auth.lastUsed', { date: date(key.last_used_at) })}</>
                        )}
                      </p>
                      <div className={styles.actions}>
                        <Button type="submit" disabled={busy}>
                          <Label name="auth.rename" />
                        </Button>
                        <Button disabled={busy} onClick={() => setConfirm(key.id)}>
                          <Label name="auth.revoke" />
                        </Button>
                      </div>
                    </form>
                    {confirm === key.id && (
                      <div className={styles.actions}>
                        <Button
                          disabled={busy}
                          onClick={() =>
                            run(async () => {
                              await mutate('/security/passkeys', 'DELETE', { id: key.id });
                              setConfirm('');
                              setMode('passkey');
                            })
                          }
                        >
                          <Label name="auth.confirmRevoke" />
                        </Button>
                        <Button onClick={() => setConfirm('')}>
                          <Label name="auth.cancel" />
                        </Button>
                      </div>
                    )}
                  </li>
                ))}
              </ul>
              <form
                className={styles.form}
                onSubmit={(e) => {
                  e.preventDefault();
                  run(() => passkey(true));
                }}
              >
                <Field label={t('auth.keyLabel')}>
                  <TextInput
                    data-config-path="security.label"
                    value={label}
                    onChange={(e) => setLabel(e.target.value)}
                    maxLength={120}
                    required
                  />
                </Field>
                <Button data-testid="add-passkey" disabled={busy} type="submit">
                  <Label name="auth.addKey" />
                </Button>
              </form>
            </section>
            <section className={styles.panel}>
              <h2>
                <Label name="auth.fallback" />
              </h2>
              <p>
                <Label name="auth.email" />: <bdi>{factors?.login_email}</bdi>
              </p>
              <p className={styles.help}>
                <Label name="auth.emailHelp" /> <Label name="auth.fallbackHelp" />
              </p>
              <p>{t(factors?.totp_enabled ? 'auth.enabled' : 'auth.disabled')}</p>
              {!factors?.totp_available ? (
                <p>
                  <Label name="auth.keyMissing" />
                </p>
              ) : seed ? (
                <form
                  className={styles.form}
                  onSubmit={(e) => {
                    e.preventDefault();
                    run(async () => {
                      await call('/security/totp/verify', 'POST', { code }, state.csrf);
                      setSeed('');
                      setCode('');
                      setMode('passkey');
                      await refresh();
                      setNotice('auth.saved');
                    });
                  }}
                >
                  <p>
                    <Label name="auth.seedHelp" />
                  </p>
                  <p className={styles.secret} data-testid="totp-secret" dir="ltr">
                    {seed}
                  </p>
                  {otpField}
                  <Button type="submit" disabled={busy}>
                    <Label name="auth.verify" />
                  </Button>
                </form>
              ) : (
                <form
                  className={styles.form}
                  onSubmit={(e) => {
                    e.preventDefault();
                    run(async () => {
                      const result = await call<{ secret: string }>(
                        '/security/totp/options',
                        'POST',
                        { password },
                        state.csrf,
                      );
                      setPassword('');
                      setSeed(result.secret);
                      setCode('');
                    });
                  }}
                >
                  {passwordField}
                  <Button type="submit" disabled={busy}>
                    <Label name="auth.enable" />
                  </Button>
                </form>
              )}
              {factors?.totp_enabled && (
                <Button
                  disabled={busy}
                  onClick={() =>
                    run(async () => {
                      await mutate('/security/totp', 'DELETE', {});
                      setMode('passkey');
                    })
                  }
                >
                  <Label name="auth.disable" />
                </Button>
              )}
            </section>
            <section className={styles.panel}>
              <h2>
                <Label name="auth.codes" />
              </h2>
              <p className={styles.help}>
                <Label name="auth.codesHelp" />
              </p>
              <p>{t('auth.remaining', { n: factors?.recovery_codes_remaining || 0 })}</p>
              <Button
                disabled={busy}
                onClick={() =>
                  run(async () => {
                    const result = await call<{ codes: string[] }>(
                      '/security/recovery-codes',
                      'POST',
                      {},
                      state.csrf,
                    );
                    setCodes(result.codes);
                    await refresh();
                    setNotice('auth.saved');
                  })
                }
              >
                <Label name="auth.generate" />
              </Button>
              {codes.length > 0 && (
                <>
                  <ul data-testid="recovery-codes" className={styles.secret} dir="ltr">
                    {codes.map((c) => (
                      <li key={c}>{c}</li>
                    ))}
                  </ul>
                  <Button onClick={() => setCodes([])}>
                    <Label name="auth.hideCodes" />
                  </Button>
                </>
              )}
            </section>
            <section className={styles.panel}>
              <h2>
                <Label name="auth.sessions" />
              </h2>
              <ul className={styles.list}>
                {sessions.map((session) => (
                  <li className={styles.item} key={session.id}>
                    <span>
                      {session.id === state.session?.id
                        ? t('auth.current')
                        : t('auth.created', { date: date(session.created_at) })}
                    </span>
                    <Button
                      disabled={busy}
                      onClick={() =>
                        run(() => mutate('/security/sessions', 'DELETE', { id: session.id }))
                      }
                    >
                      <Label name="auth.revoke" />
                    </Button>
                  </li>
                ))}
              </ul>
              <Button
                disabled={busy}
                onClick={() => run(() => mutate('/security/sessions', 'DELETE', {}))}
              >
                <Label name="auth.revokeAll" />
              </Button>
            </section>
            <PublisherTokens csrf={state.csrf!} />
            <OAuthConnections csrf={state.csrf!} />
            <section className={styles.panel}>
              <h2>
                <Label name="auth.events" />
              </h2>
              <ul data-testid="security-events" className={styles.list}>
                {events.slice(0, 20).map((e, i) => (
                  <li key={i}>
                    {t(
                      'securityEvent.' + e.event in DEFAULT_LABELS
                        ? (('securityEvent.' + e.event) as LabelKey)
                        : 'auth.event',
                    )}{' '}
                    · {date(e.created_at)}
                  </li>
                ))}
              </ul>
            </section>
          </div>
        </>
      ) : (
        <>
          <p className={styles.help}>
            <Label name="auth.guidance" />
          </p>
          {mode === 'passkey' ? (
            <section className={styles.panel}>
              <Button
                data-testid="passkey-login"
                variant="primary"
                disabled={busy}
                onClick={() => run(() => passkey())}
              >
                <Label name="auth.passkey" />
              </Button>
              <Button disabled={busy} onClick={() => setMode('password')}>
                <Label name="auth.passwordOption" />
              </Button>
              <Button disabled={busy} onClick={() => setMode('recovery')}>
                <Label name="auth.recoveryOption" />
              </Button>
              <Button disabled={busy} onClick={() => setMode('grant')}>
                <Label name="auth.setupOption" />
              </Button>
            </section>
          ) : (
            <form className={styles.panel} onSubmit={submit}>
              {(mode === 'password' || mode === 'grant') && (
                <Field label={t('auth.email')} help={t('auth.emailHelp')}>
                  <TextInput
                    type="email"
                    autoComplete="username"
                    value={email}
                    onChange={(e) => setEmail(e.target.value)}
                    maxLength={254}
                    required
                    dir="ltr"
                  />
                </Field>
              )}
              {mode === 'password' && passwordField}
              {mode === 'otp' && otpField}
              {mode === 'recovery' && (
                <Field label={t('auth.recoveryCode')}>
                  <TextInput
                    value={code}
                    onChange={(e) => setCode(e.target.value.trim())}
                    autoComplete="off"
                    required
                    dir="ltr"
                  />
                </Field>
              )}
              {mode === 'grant' && (
                <Field label={t('auth.grant')} help={t('auth.grantHelp')}>
                  <TextInput
                    value={grant}
                    onChange={(e) => setGrant(e.target.value.trim())}
                    autoComplete="off"
                    required
                    dir="ltr"
                  />
                </Field>
              )}
              {mode === 'enroll' && (
                <>
                  <p>
                    <Label name="auth.restricted" />
                  </p>
                  <Field label={t('auth.keyLabel')}>
                    <TextInput
                      value={label}
                      onChange={(e) => setLabel(e.target.value)}
                      maxLength={120}
                      required
                    />
                  </Field>
                </>
              )}
              <Button type="submit" variant="primary" disabled={busy}>
                {t(
                  mode === 'enroll'
                    ? 'auth.enroll'
                    : mode === 'otp'
                      ? 'auth.verify'
                      : 'auth.continue',
                )}
              </Button>
              {mode === 'otp' && (
                <Button
                  disabled={busy}
                  onClick={() => {
                    setMode('recovery');
                    setCode('');
                  }}
                >
                  <Label name="auth.recoveryOption" />
                </Button>
              )}
              <Button disabled={busy} onClick={back}>
                <Label name="auth.back" />
              </Button>
            </form>
          )}
          <p className={styles.help}>
            <Label name="auth.backupHelp" />
          </p>
          <a href={`/${locale}`}>
            <Label name="auth.viewSite" />
          </a>
        </>
      )}
    </>
  );
  return owner ? (
    <OwnerWorkspace
      current="security"
      csrf={state.csrf}
      busy={busy}
      mainId="content"
      className="owner-security"
      languagePath="/admin/security"
      onSignOut={() => run(signout)}
    >
      {content}
    </OwnerWorkspace>
  ) : (
    <div className={`vb vb-login ${styles.page} ${styles.login}`} lang={locale} dir={dir}>
      <a className="vb-skip" href="#content">
        <Label name="nav.skip" />
      </a>
      <header className={`vb-login-top ${styles.header}`}>
        {configuration && <Logo brand={siteBrand(configuration.site)} href={'/' + locale} />}
        <nav className={styles.nav} aria-label={t('nav.language')}>
          {(['en', 'fr', 'ar'] as const).map((l) => (
            <a
              key={l}
              href={`/${l}/login`}
              lang={l}
              aria-label={t(`language.${l}`)}
              aria-current={l === locale ? 'page' : undefined}
            >
              {l === 'ar' ? 'ع' : l.toUpperCase()}
            </a>
          ))}
        </nav>
      </header>
      <main id="content" className={`vb-login-card ${styles.main}`} aria-busy={busy}>
        {content}
      </main>
    </div>
  );
}
