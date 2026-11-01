import { call, type State } from './api';
import { redirectOwnerToLogin } from './navigation';
import { credentialJSON, requestOptions, type RequestJSON } from './webauthn';

export async function authenticatedTransfer(url: string, options: RequestInit): Promise<Response> {
  const send = (csrf?: string) =>
    fetch(url, {
      ...options,
      headers: { ...options.headers, ...(csrf ? { 'X-CSRF-Token': csrf } : {}) },
    });
  const session = await call<State>('/session');
  let response = await send(session.csrf);
  if (response.status !== 401 && response.status !== 403) {
    return response;
  }
  let current: State;
  try {
    current = await renewOwnerSession();
  } catch (error) {
    if (response.status === 401) {
      redirectOwnerToLogin();
    }
    throw error;
  }
  response = await send(current.csrf);
  if (response.status === 401) {
    redirectOwnerToLogin();
  }
  return response;
}

export async function renewOwnerSession(): Promise<State> {
  if (!navigator.credentials?.get) {
    throw new DOMException('Passkeys unavailable', 'NotSupportedError');
  }
  const started = await call<State>('/auth/start', 'POST', {}, undefined, {
    'X-Blog-Request': '1',
  });
  const challenge = await call<{ publicKey: RequestJSON }>(
    '/auth/passkeys/login/options',
    'POST',
    {},
    started.csrf,
  );
  const credential = (await navigator.credentials.get({
    publicKey: requestOptions(challenge.publicKey),
  })) as PublicKeyCredential | null;
  if (!credential) {
    throw new DOMException('Cancelled', 'NotAllowedError');
  }
  const renewed = await call<State>(
    '/auth/passkeys/login/verify',
    'POST',
    credentialJSON(credential),
    started.csrf,
  );
  const current = renewed.csrf ? renewed : await call<State>('/session');
  return current;
}
