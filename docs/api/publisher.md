# Scoped publisher and Go CLI

[العربية](publisher.ar.md) · [API index](README.md) · [Delivery runner](../integrations/lesson-delivery.md)

Owner Security creates named tokens with independent `draft`, `media`, `quiz` and
`publish` capabilities. Select collection keys; articles outside collections need
an explicit additional allowance. Creation requires a full owner authentication
within five minutes, rechecked in the transaction. The UI defaults to draft-only,
90-day expiry; the API accepts 1–365 days. At most 50 unexpired, unrevoked tokens
are active. Only a SHA-256 hash is stored. Save the displayed secret in the runner's
secret store; subsequent reads never return it. Rename or revoke from Security.

`/api/v1/security/publisher-tokens`: GET lists metadata, POST accepts
`{label,scopes,collections,allow_articles,days}` and returns metadata plus `secret`
once, PATCH accepts `{id,label}`, DELETE accepts `{id}`. These routes use the
owner cookie and the normal Origin/CSRF boundary. Publisher credentials cannot
create credentials or become owner/learning sessions.

## Machine routes

The dedicated `/api/v1/publisher/` namespace uses `Authorization: Bearer …`, not
browser cookies. Query strings are rejected. Credentials never belong in URLs,
Markdown, bundles or logs. The routes call the same content/media services as the
owner UI, with token revocation, capabilities and collection restrictions checked
inside their mutation transactions.

| Route | Capability and behavior |
| --- | --- |
| `GET /content/{key}/{locale}` | `draft`; read the scoped current draft for reconciliation. Quiz-bearing drafts additionally require `quiz`. |
| `PUT /content/{key}/{locale}` | `draft`; same draft JSON as the owner API. Adding/changing/removing a quiz requires `quiz`. |
| `POST /content/{key}/{locale}/publish` | `publish`; exact `{expected_version,revision_id}`. Translation review and active assessment constraints remain enforced. No path-plan application. |
| `POST /media` | `media`; one multipart `file`, content purpose only. Token uploads or references already retained in an allowed collection/article can be attached to drafts. |

Table paths are relative to `/api/v1/publisher`. Every write requires
`Idempotency-Key`; exact replay returns its result for seven days, changed-payload
reuse returns 409. Draft/publication version conflicts also return 409. Reconcile
with GET before deliberately preparing a new operation. Do not automatically bump
expected versions after a conflict. Revoke takes effect even for retries.

Tokens cannot review translations/assessments, edit gates, apply plans, reset
progress, alter branding or authentication, or impersonate a learner. A token
cannot substitute a different assessment behind an active gate. The owner can
review the new assessment and explicitly publish it with a matching saved plan.

`quiz` controls draft answer-key access and quiz editing; `publish` independently
allows publication of an exact saved revision in scope, including one with a quiz.
Publication does not grant answer-key access, quiz editing or review authority.
The existing translation-review and active-gate assessment checks still apply.

## CLI

Build with `node scripts/project.mjs build`. The runner supplies `BLOG_URL` and
`BLOG_PUBLISHER_TOKEN` through its secret environment. HTTPS is required except
for a literal loopback IP used in tests. `BLOG_PUBLISHER_CA` optionally names a
trusted PEM CA file; it does not disable certificate verification. Redirects are
not followed. The CLI never takes a token as a command-line flag.

```sh
build/publisher upload --file image.webp --mime image/webp --idempotency lesson-001-image
build/publisher draft --key lesson-001 --locale en --input draft.json --markdown lesson.md --idempotency lesson-001-draft
build/publisher get --key lesson-001 --locale en
build/publisher publish --key lesson-001 --locale en --revision SAVED_REVISION_ID --expected-version 1 --idempotency lesson-001-publish
```

`draft.json` uses the owner `DraftInput` schema: `expected_version` (0 for new),
kind, source locale, slug, title, optional summary, collection key/position for a
lesson/doc, optional quiz, media references and source revision for translations.
`--markdown` supplies the separate file body. Outputs are JSON with immutable
revision IDs and versions, or structured API status/code errors; exit status 1
means failure. Retry an interrupted operation with the same payload and key.

## Local publisher launcher

Save only these values in `~/.config/modawana/publisher.env`, outside
Git, with directory permissions 0700 and file permissions 0600:

```sh
BLOG_URL=https://blog.example
BLOG_PUBLISHER_TOKEN='PASTE_YOUR_BLOG_TOKEN_HERE'
```

Replace the placeholder privately. An optional `BLOG_PUBLISHER_CA` names a trusted
PEM file. Never display this file, print environment variables, enable shell tracing
or put its token in a prompt, command argument or schedule. The Node launcher
parses dotenv data without executing shell expressions, rejects symlinks,
group/world-accessible files, other owners and unexpected configuration names,
then passes credentials to the Go publisher only through its child environment.
This reduces accidental disclosure; it does not hide the file from tools or users
that already have filesystem access.

Build the executable for your platform from the repository root (no VPS rebuild is needed):

```sh
cd backend
go build -trimpath -o ../build/publisher ./cmd/publisher
cd ..
node scripts/publish.mjs --check
```

`--check` validates local configuration and executable availability without making
a network request. It does not verify token validity, scopes or expiry. Pass all
normal CLI arguments to `node scripts/publish.mjs`, for example:

```sh
node scripts/publish.mjs get --key EXISTING_CONTENT_KEY --locale en
node scripts/publish.mjs draft --key lesson-001 --locale en --input draft.json --markdown lesson.md --idempotency lesson-001-draft
```

For a real draft, prepare the documented metadata/Markdown first and retain the
same input/idempotency key when retrying. Start with a read of an existing permitted
item when available. Do not use a nonexistent content key as an authentication
probe: scope checks run before draft lookup and may return 403 instead of 404.
An invalid/expired/revoked token returns 401. A 403 does not prove every requested
capability or collection is allowed.

For a deliberate private draft check, a token needs `draft` and permission for
articles outside collections. These fixtures are never imported automatically:

```sh
node scripts/publish.mjs draft --key publisher-connection-check --locale en --input scripts/fixtures/publisher-check.json --markdown scripts/fixtures/publisher-check.txt --idempotency publisher-connection-check-v1
node scripts/publish.mjs get --key publisher-connection-check --locale en
```

This creates one unpublished test article, not a public post. Retrying the exact
draft command reuses its idempotency key. Stop on a version conflict; do not
overwrite an existing item or increment versions automatically. Inspect it under
owner Posts. This checks article draft access, not media, quiz, publication or
learning-path permissions.

Publishing requires its exact saved revision/version and an
authorized publication request or configured original-language lesson policy;
having the token's capability alone does not authorize publishing arbitrary text.
Collection permissions, owner translation/assessment review and gates still apply.

A local automation task can invoke this launcher only when its filesystem and network
permissions allow it to read the private configuration, execute the publisher and
reach the HTTPS origin. A credential saved on one machine is not automatically available to
another chat, cloud runner or scheduled lesson. This launcher is not an MCP server.
See the [delivery checkpoint](../integrations/lesson-delivery.md) before changing
existing schedules. Test credential handling with `node --test scripts/publish.test.mjs`.

Verification: Go tests cover freshness, hash storage, collection/quiz isolation,
revoked credentials, conflicts, replay and privilege escalation. The real-server
browser suite exercises the EN/AR/FR token UI and actual Go CLI upload/draft retry,
exact publication and revocation. No production credentials/content are seeded.

## Remote MCP and scheduled generation

Use [MCP/OAuth](../integrations/mcp-oauth.md) for supported remote clients and
[the scheduler](../integrations/remote-publishing.md) for optional generated lessons.
Both reuse publisher permissions; AI-generation credentials are separate.

The launcher defaults to `~/.config/modawana/publisher.env`. Existing installations
can keep their current private file by setting `BLOG_PUBLISHER_CONFIG` to its absolute
path before running the launcher. No credentials are moved automatically.
