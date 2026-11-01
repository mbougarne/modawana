import { isOwnerRequest, redirectOwnerToLogin } from './navigation';

export type FieldIssue = { path: string; code: string };

export class AuthError extends Error {
  constructor(
    public code: string,
    public fields: FieldIssue[] = [],
  ) {
    super(code);
  }
}

export async function call<T>(
  path: string,
  method = 'GET',
  body?: unknown,
  csrf?: string,
  extra: Record<string, string> = {},
): Promise<T> {
  const options: RequestInit = {
    method,
    credentials: 'same-origin',
    cache: 'no-store',
    headers: {
      ...(body !== undefined ? { 'Content-Type': 'application/json' } : {}),
      ...(csrf ? { 'X-CSRF-Token': csrf } : {}),
      ...extra,
    },
    body: body !== undefined ? JSON.stringify(body) : undefined,
  };
  let response = await fetch('/api/v1' + path, options);
  const ownerRequest = isOwnerRequest(path);
  if (
    ownerRequest &&
    (response.status === 401 || response.status === 403) &&
    !/^\/(auth|session|security)(\/|$)/.test(path) &&
    typeof navigator !== 'undefined' &&
    typeof navigator.credentials?.get === 'function'
  ) {
    let session = await call<State>('/session');
    if (response.status === 401 || session.state !== 'owner') {
      const { renewOwnerSession } = await import('./reauthenticate');
      try {
        session = await renewOwnerSession();
      } catch (error) {
        if (response.status === 401) {
          redirectOwnerToLogin();
        }
        throw error;
      }
    }
    response = await fetch('/api/v1' + path, {
      ...options,
      headers: { ...options.headers, 'X-CSRF-Token': session.csrf || '' },
    });
  }
  // Security writes check CSRF before authentication, so a missing owner cookie
  // can return 403. Confirm session loss without retrying a sensitive mutation.
  if (ownerRequest && response.status === 403 && path.startsWith('/security/')) {
    try {
      const session = await call<State>('/session');
      if (session.state !== 'owner') {
        redirectOwnerToLogin();
      }
    } catch {
      // A failed probe is not proof of expiry; preserve the original denial.
    }
  }
  if (ownerRequest && response.status === 401) {
    redirectOwnerToLogin();
  }
  let json;
  try {
    json = await response.json();
  } catch {
    throw new AuthError(
      (
        {
          401: 'unauthenticated',
          403: 'forbidden',
          409: 'revision_conflict',
          413: 'too_large',
          422: 'invalid_fields',
          429: 'rate_limited',
          503: 'unavailable',
        } as Record<number, string>
      )[response.status] || 'internal_error',
    );
  }
  if (!response.ok) {
    const fields = Array.isArray(json.error?.fields)
      ? json.error.fields
          .filter(
            (field: unknown): field is FieldIssue =>
              !!field &&
              typeof field === 'object' &&
              typeof (field as FieldIssue).path === 'string' &&
              typeof (field as FieldIssue).code === 'string',
          )
          .slice(0, 300)
      : [];
    throw new AuthError(json.error?.code || 'internal_error', fields);
  }
  return json as T;
}

export type State = { state: string; csrf?: string; session?: Session };

export type Session = {
  id: string;
  method: string;
  authenticated_at: string;
  created_at: string;
  expires_at: string;
};

export type Key = {
  id: string;
  label: string;
  created_at: string;
  last_used_at: string | null;
  tested_at: string | null;
};

export type Factors = {
  totp_enabled: boolean;
  totp_available: boolean;
  login_email: string;
  recovery_codes_remaining: number;
};

export type Audit = { event: string; created_at: string; request_id: string };

export function requestErrorKey(error: unknown): string {
  if (error instanceof DOMException) {
    return error.name === 'NotAllowedError' || error.name === 'AbortError'
      ? 'auth.cancelled'
      : error.name === 'NotSupportedError' || error.name === 'SecurityError'
        ? 'auth.unsupported'
        : 'request.network';
  }
  if (!(error instanceof AuthError)) {
    return 'request.network';
  }
  const messages: Record<string, string> = {
    unauthenticated: 'request.session',
    forbidden: 'request.confirm',
    revision_conflict: 'request.conflict',
    invalid_fields: 'request.invalid',
    rate_limited: 'request.rate',
    too_large: 'request.tooLarge',
    not_found: 'request.missing',
    unavailable: 'request.unavailable',
    internal_error: 'request.server',
    bad_request: 'request.invalid',
    unsupported_media_type: 'request.invalid',
  };
  return messages[error.code] || 'request.server';
}
