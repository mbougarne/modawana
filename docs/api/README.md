# HTTP and configuration contracts

[العربية](README.ar.md) · [Documentation](../README.md) · [OpenAPI v1](v1.openapi.json)

The same-origin API covers authentication, security, content, media, configuration, learning and scoped publishing. Private resources remain private regardless of frontend routing. Keep OpenAPI and frontend/src/api/contracts.ts aligned with Go validation.

| Route | Behavior |
| --- | --- |
| GET/HEAD /healthz | Liveness; 200 with `{"status":"ok"}` |
| GET/HEAD /readyz | 200 when dependencies are ready; bounded check, otherwise 503 |
| GET/HEAD /api/v1/public/runtime | Explicit public allowlist: api_version, default_locale, locales |
| GET/HEAD /assets/{hashed-name} | Only immutable files enumerated by the checked build manifest |
| GET/HEAD /, /en, /ar, /fr | Escaped Go document shell loading React, localized lang/dir |
| Missing API/media/asset | JSON 404, never an HTML success |
| Unknown page | HTML 404 with noindex |

No CORS allowance is emitted. Browser API calls use relative paths. All JSON/errors and HTML use no-store; successful build assets use one-year immutable caching. Go serves the production build without a Node process. The shell's script nonce is fresh per response; CSP disallows external scripts, framing, objects and unnonced inline styles. BrandStyle accepts the per-response nonce exposed in a meta element; default colours need no override stylesheet. Future dynamic brand styles must retain this nonce or use a stylesheet route.

## Errors and bounded inputs

The envelope is `{"error":{"code":"not_found","message":"Resource not found.","request_id":"…"}}`. An optional fields map holds safe validation codes, never submitted secrets. Codes map to 400 malformed, 401 unauthenticated, 403 forbidden, 404 missing, 405 method, 409 revision/idempotency conflict, 413 too large, 415 content type, 422 fields, 429 throttled, 500 internal and 503 unavailable. Authentication throttling sets Retry-After. Human messages are generic; interface labels use localized code mappings.

DecodeJSON accepts a typed struct and an explicit per-route byte limit. It requires application/json, rejects null, arrays, unknown fields and trailing JSON, and bounds the body even without Content-Length. No mutation endpoint is added solely for testing this parser. Request IDs are generated server-side; inbound IDs are ignored. Logs contain generated ID, allowlisted method, status and duration. They never log bodies, cookies, authorization, raw URLs/queries, config values or raw handler errors. Reverse-proxy logging requires equivalent redaction.

## Operator configuration

Configuration is read from the environment, not automatically from .env files. All errors identify only the variable name. No environment variable is embedded into the frontend build.

| Variable | Rule |
| --- | --- |
| BLOG_ENV | production (default) or development |
| BLOG_BASE_URL | Exact origin, no trailing slash/path/query/userinfo; HTTPS except loopback HTTP in development |
| BLOG_RP_ID | Exact hostname of BLOG_BASE_URL; deliberately no parent-domain broadening |
| BLOG_WEBAUTHN_ORIGINS | Comma-separated exact configured origin; this single-origin foundation does not enable alternate origins |
| BLOG_DATA_ROOT | Required absolute path outside checkout, working release and frontend directories; existing symlink ancestors resolved; never serve this directory |
| BLOG_FRONTEND_DIST | Absolute recommended; default ../frontend/dist relative to working directory; manifest required in production |
| BLOG_LISTEN_ADDR | Explicit IP:port; default 127.0.0.1:8080; proxy handles external HTTPS |
| BLOG_TRUSTED_PROXIES | Empty by default, otherwise comma-separated IP CIDRs for actual proxy peers |

Client IP ignores forwarding headers from untrusted peers. For trusted peers, X-Forwarded-For is bounded, validated and scanned from right to left until the first untrusted hop. Malformed lists fall back to the direct peer. Forwarded host/protocol never replace configured origin or RP ID. Configure the proxy to overwrite or append the actual peer correctly. Authentication rate limiting uses this resolver, not raw headers. The auth service implements WebAuthn verification, exact-origin and CSRF checks.

Development orchestration supplies loopback settings and a checkout-specific temporary data directory unless BLOG_DATA_ROOT is provided. Temporary development data can be removed by the operating system; never use it for production. Production startup from the repository root after building:

```sh
BLOG_ENV=production \
BLOG_BASE_URL=https://blog.example \
BLOG_RP_ID=blog.example \
BLOG_WEBAUTHN_ORIGINS=https://blog.example \
BLOG_DATA_ROOT=/var/lib/modawana \
BLOG_FRONTEND_DIST="$PWD/frontend/dist" \
./build/blog
```

Use an existing writable private data directory owned by the service account. For complete VPS setup and upgrade commands follow the [deployment guide](../../deploy/README.md). Database setup and required pre-migration backup rules are in the [SQLite guide](../../backend/migrations/README.md).

## Shared authorization boundary and verification

internal/access defines typed principal, surface, resource/locale/method and visibility decisions for content, media, preview, search, feeds and collections. The zero decision and DenyAll reject every audience. PublicTeaser cannot grant a body. The live policy resolves current publication and active plan consistently for content, media, search and collections. A URL or published media reference alone is insufficient. Missing or unsupported lesson plans remain locked; quiz/learner rules extend this boundary.

Run `go test ./...` and `go vet ./...` in backend/. Tests cover input bounds, missing routes, proxy spoofing, origin/path validation, asset traversal/symlinks, ready failure and secret-free responses/logs. After the normal build and Playwright installation, `node scripts/smoke-production.mjs` starts only the Go binary, checks UI/API/assets at one origin, and runs the browser language/theme matrix against the real shell and CSP. The test harness uses Node; the serving application does not.

Authentication routes and wire payloads: [Owner API](owner.md). Optional `BLOG_AUTH_KEY_FILE` selects a private 32-byte binary file outside the data root; see the owner setup guide.

- [Controlled media](media.md): implemented upload and delivery contract.

- [Content, discovery and collections](content.md): implemented owner/public workflows.

- [Saved branding, profile and labels](configuration.md): owner revisions, reviews and applied public projections.

- [Public pages and route API](../public-pages.md): shared HTML/SPA resolution and public collection pagination.

- [Quizzes and learning identity](learning.md): quiz authoring/preview, browser identity, reset and retention contracts.

- [Scoped publisher](publisher.md): owner-managed tokens, Go CLI and durable separate delivery.

- [Owner portability](portability.md): fresh-session export/import with draft-only validated staging.
- [Deployment and runtime operations](../../deploy/README.md).
