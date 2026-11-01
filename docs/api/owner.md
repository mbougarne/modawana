# Owner API

[العربية](owner.ar.md) · [API index](README.md) · [Operating guide](../owner-security.md)

All paths below are relative to `/api/v1`. JSON responses use `no-store`; errors
use the common envelope. Input is bounded to 64 KiB. Non-WebAuthn bodies containing inputs are
strict typed objects; WebAuthn bodies use the library's standard credential JSON.
No physical path, private key, cookie token or encrypted seed is returned.

| Method/path | Input → success payload | Authorization |
| --- | --- | --- |
| POST /auth/start | `{}` → `{state:"anonymous",csrf}` and restricted cookie | Exact Origin, `X-Blog-Request: 1`; no CORS |
| GET /session | → `{state,csrf?,session?}`; state anonymous/bootstrap/recovery/password/managed/owner | Existing cookies; creates no grant |
| POST /auth/grant | `{grant,email}` → `{state:"enrollment",csrf}` and rotated restricted cookie | Anonymous flow + CSRF; local one-use grant |
| POST /auth/passkeys/login/options | `{}` → `{publicKey:…}` | Anonymous flow + CSRF |
| POST /auth/passkeys/login/verify | Browser assertion JSON → `{state:"owner",csrf}` | Matching one-use challenge; sets rotated owner cookie |
| POST /auth/passkeys/register/options | `{}` → `{publicKey:…}` | Bootstrap/recovery flow, or fresh owner plus anonymous flow |
| POST /auth/passkeys/register/verify | Browser attestation JSON; `X-Passkey-Label` is encodeURIComponent(name) → `{state:"sign_in_to_test"}` | Same bound flow, fresh owner again for managed addition; no owner session from registration |
| POST /session | `{email,password}` → `{state:"password",csrf}` | Anonymous flow + CSRF; no full cookie |
| POST /session/totp | `{code}` → `{state:"owner",csrf}` | Password-pending flow + CSRF |
| POST /session/recovery | `{code}` → `{state:"owner" or "recovery",csrf}` | Password pending when fallback exists; otherwise restricted re-enrollment only |
| DELETE /session | `{}` → `{ok:true}` | Owner + CSRF; revokes current cookie |
| GET /security/passkeys | → `[{id,label,created_at,last_used_at,tested_at}]` | Owner |
| PATCH /security/passkeys | `{id,label}` → `{ok:true}` | Fresh owner + CSRF |
| DELETE /security/passkeys | `{id}` → `{ok:true}` | Fresh owner + CSRF; requires another tested method; revokes sessions |
| GET /security/factors | → `{totp_enabled,totp_available,login_email,recovery_codes_remaining}` | Owner; private email never public profile data |
| POST /security/totp/options | `{password}` → `{secret}` and restricted enrollment cookie | Fresh owner + CSRF, external key configured |
| POST /security/totp/verify | `{code}` → `{state:"sign_in"}` | Same owner/session plus pending encrypted seed; activates and revokes sessions |
| DELETE /security/totp | `{}` → `{ok:true}` | Fresh owner + CSRF; another tested method required |
| POST /security/recovery-codes | `{}` → `{codes:[10 strings]}` once | Fresh owner + CSRF; replaces previous codes |
| GET /security/sessions | → `[{id,method,authenticated_at,created_at,expires_at}]` | Owner; up to 100 active rows |
| DELETE /security/sessions | `{id?}` → `{ok:true}`; omitted/empty id revokes all | Fresh owner + CSRF |
| GET /security/events | → `[{event,request_id,created_at}]` | Owner; latest 100 of bounded 1000 retained events |

Every write after `/auth/start` requires exact Origin and `X-CSRF-Token` tied to
its cookie. Security/logout use owner CSRF; authentication ceremonies use restricted
flow CSRF. Public GET requests do not create authentication state. `state` describes
authentication only, never proof that future content/publishing endpoints exist.
Five-minute freshness is checked again inside sensitive database transactions.

401 is generic authentication failure, 403 denied/needs fresh authentication,
409 a factor conflict, 429 a throttled attempt (Retry-After: 900), 503 unavailable
operator key/service, and 500 internal failure. Invalid authentication payloads
fail generically; do not expose whether an email/factor exists. A failed or lost
mutation response is not a success confirmation; reload state before retrying.
Passkey challenges are single-use even on failed verification and expire in five
minutes. Codes/seeds remain only in transient UI state and never localStorage.

## Owner action feedback

Account/Security can return `error.fields: [{path,code}]` with safe schema paths:
`security.label`, `security.password`, `security.code`, `security.passkeys`,
`security.request` and `security.token_*`. Reasons explain name/password limits,
invalid or expired authenticator setup, token input constraints, last-factor and
tested-passkey requirements. Input errors use 422, blocked factor removal 409 and
unconfirmed writes 403. Session/freshness checks, rate limits, transactions and
uniform anonymous credential errors remain. No password, seed, token or credential
ID is returned in validation errors.

### Quiz assessment feedback

A missing exact assessment review returns HTTP 422 `invalid_fields` with a safe
field reason `{ "path": "quiz", "code": "assessment_review" }`. Existing collection
field reasons retain their more specific paths/codes. Preview and review remain
owner/Origin/CSRF-protected operations on the exact saved revision; neither
publishes content or records browser learning progress. A saved gate is private
until the path plan is explicitly activated.
