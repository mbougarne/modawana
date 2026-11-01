# Owner setup and security

[العربية](owner-security.ar.md) · [API](api/README.md) · [Passkeys and devices](passkey-access.md)

The application provides one owner, passkey-first sign-in, optional password +
TOTP, and restricted recovery. No owner or factor is seeded. The pages are `/{en|ar|fr}/login`
and `/{en|ar|fr}/admin/security`. Explicit HTML navigation to a localized admin
page without a usable owner session redirects to that locale's login page, retaining
a validated local return destination. `/admin/login` and `/{locale}/admin/login`
are browser aliases for the actual login page. API/non-HTML requests retain JSON
401 errors. Open admin screens return to login on unrecovered 401s; 403 permission
or freshness errors do not by themselves redirect. Existing passkey renewal and
password/TOTP login remain available. The Transfer browser route is also recognized.
Production authentication requires HTTPS at the configured origin. For local
Chrome testing without certificates, `node scripts/project.mjs dev` uses
`http://localhost:3000` and RP ID `localhost`; Chrome supports Secure cookies and
WebAuthn on localhost. Safari may need trusted HTTPS. See the root
[local testing guide](../README.md#testing-owner-login-and-writing-locally),
including the built-in virtual authenticator option. Simulation does not prove
physical passkey compatibility. Never weaken cookies or production origin checks.

## Local setup

Set the environment from the API guide, build, and back up existing data before
migrations. Run as the service's OS user on the VPS:

```sh
./build/blog bootstrap
```

This prints one random grant to the operator terminal, valid for ten minutes.
Never put its value in an argument, URL, shell history, log or Git. Open the login
page, choose the local setup/recovery option, enter the grant and a private login
email, and register a named passkey. Redemption is one-use; no public registration
endpoint exists. The enrollment session lasts five minutes and cannot access
owner APIs. Sign in with the registered passkey to test it and enter Security.
If setup is abandoned after owner creation, use local recovery below.
For development, `node scripts/project.mjs recover` uses the same environment and
data root as `dev`; use `recover:https` for `dev:https`. Redeem the new grant with
the original owner email. Recovery revokes sessions/pending flows, preserves
content, and still requires registering and testing a replacement passkey.

Add an independent backup, test it with a fresh sign-in, and generate offline
recovery codes before using the site for production. Browser/provider selection
is native WebAuthn: no custom QR flow, platform-only restriction or guaranteed
cross-provider sync. Registration and login require user verification and a
discoverable credential. Names do not prove independence. The server retains
public credentials/counter/backup metadata, not private keys. Limit: 20 active
credentials. Revocation requires another tested passkey or an enrolled TOTP
alternative; an untested replacement cannot justify deleting the last working key.
Revoking a key revokes current owner sessions, including the initiating session. A TOTP configuration with an unavailable or undecryptable seed does not count as a usable replacement for the last passkey.

## Optional password + TOTP

To enable this opt-in alternative, provision a separate 32-byte binary encryption
key outside `BLOG_DATA_ROOT`, the repository and release directories. A protected
operator secrets directory is appropriate. For example, after creating that
private directory with the service account:

```sh
umask 077
openssl rand -out /srv/modawana-secrets/auth.key 32
export BLOG_AUTH_KEY_FILE=/srv/modawana-secrets/auth.key
```

Do not overwrite an existing key. The file must be regular, private (0600 or
stricter), not a symlink and outside the canonical data root. Back it up in a
separate protected recovery bundle; losing it makes existing TOTP seeds unusable.
The application does not generate a key silently. No key means TOTP enrollment
is unavailable; passkeys and local/passkey-only code recovery still work.

In Security, enter a new password of at least 12 characters (maximum 1024 UTF-8
bytes). Add the displayed secret to your authenticator using six digits, SHA-1
and 30-second steps, then verify a code. The seed is shown only during enrollment;
SQLite holds AES-GCM ciphertext bound to the owner, including pending seeds.
Verification activates the alternative and revokes sessions. A newly generated
seed replaces the old one only after verification; a failed setup preserves it.

Passwords use salted Argon2id v19, 64 MiB, three iterations and two lanes, with a
32-byte result and 16-byte salt. Two concurrent password-hash operations per Go
process bound memory use. The local Apple M3 Pro benchmark was about 59 ms; run
`go test ./internal/auth -bench BenchmarkArgon2id -benchtime=3x` in `backend/` on
the actual VPS before production. This is not a VPS performance claim.
Password success grants only a five-minute pending challenge. TOTP permits ±1
step of skew and atomically rejects every previously accepted step, including
the enrollment code. Keep the VPS clock synchronized. Disabling the alternative
requires a tested passkey and revokes sessions.

## Offline and local recovery

Security generates ten independent 256-bit one-use recovery codes, displays them
once, and stores SHA-256 hashes. Regeneration invalidates all previous codes. Store
them offline before leaving the page; the server cannot show them again.

- With password + TOTP enabled, verify the password first; a recovery code replaces
  the second factor. It cannot bypass the password.
- In passkey-only mode, a code grants five-minute factor enrollment only. Sessions
  and challenges are revoked; publishing is impossible with the recovery grant.
- If no factors or codes remain, run `./build/blog recover` locally. It revokes
  sessions/challenges and issues a ten-minute one-use grant. Redeem it with the
  existing private login email through the same form. There is no public MFA-disable
  route. After registering a replacement, sign in with it to test it. Only then
  are old factors and remaining codes revoked; generate new codes immediately.
- Recovery enrollment never becomes a full session by registration alone. The
  subsequent verified passkey assertion is required. Lost VPS access as well as
  all factors/codes cannot be repaired by the application; preserve independent
  VPS/provider recovery arrangements. Public-key database records cannot restore
  the owner's private keys. Inspect Security's audit after recovery.

## Sessions, CSRF and audit

`__Host-owner` and `__Host-auth` are Secure, HttpOnly, SameSite=Strict, Path=/,
without Domain. SQLite stores only hashes of opaque cookie/grant/code secrets.
Owner sessions have a two-hour idle and 12-hour absolute limit, and rotate on
complete authentication. Restricted flows are separate records, never owner
sessions. Sensitive changes require complete authentication within 30 minutes;
use a new passkey sign-in or the full password-plus-second-factor flow to refresh.
There is no trust-device shortcut, JWT or publisher token in browser storage.

`POST /api/v1/auth/start` requires exact Origin and `X-Blog-Request: 1` to establish
a pre-authentication cookie and CSRF value. It has no CORS allowance. All later
browser writes require exact Origin plus `X-CSRF-Token`, including login, logout,
registration, factors and recovery. `GET /session` returns current state/CSRF but
never cookie secrets and creates no login grant. Expired/revoked state is denied.

Failed attempts share IP/account counters across challenges: five failures per
trusted client IP or 25 across the singleton account cause a bounded 15-minute
pause. Start/options/grant paths also have attempt ceilings. Limits persist in
SQLite across restarts; forwarded addresses count only through configured trusted
proxies. Errors are generic. Audit retains the latest 1000 safe event names,
server request IDs and UTC timestamps. Never log credentials, seeds, OTPs, codes,
raw tokens or private email. Keep reverse-proxy logs equally safe.

## Verification and remaining hardware checks

`go test ./...`, `go test -race ./...`, frontend checks and builds exercise the
storage/security boundaries. `node scripts/smoke-auth.mjs` uses a built Go binary,
real SQLite, a temporary HTTPS proxy and Chromium virtual authenticators. It tests
setup, two credentials, passkey-only recovery, optional password/TOTP, local
recovery, logout, CSRF denial and direct private-route denial in EN/AR/FR at phone
width. Its generated owner/keys/data are temporary test fixtures, never production
seeds. Screenshots omit seed/code values. Run it after the normal build,
`npm --prefix frontend test`, and Playwright Chromium installation.

**Hardware verification:** virtual tests do not demonstrate phone QR, Bluetooth proximity, provider sync, or actual hardware compatibility. Before production use, record OS/browser/provider/key model and
results for: a new computer with no local passkey; phone-assisted login; an
independently enrolled physical key with UV; cancellation/unsupported browser;
loss of one credential; local/code recovery. RP ID/domain changes require a
planned enrollment/recovery migration, never broadening accepted origins silently.

Libraries checked on 2026-10-24: [go-webauthn v0.18.2](https://github.com/go-webauthn/webauthn/releases/tag/v0.18.2),
[pquerna/otp v1.5.0](https://github.com/pquerna/otp), and
[golang.org/x/crypto v0.58.0](https://pkg.go.dev/golang.org/x/crypto/argon2).
