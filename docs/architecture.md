# Architecture

[العربية](architecture.ar.md) · [Documentation](README.md)

Modawana is a single-owner publishing application. One Go process serves
compiled React assets and a same-origin API behind an HTTPS reverse proxy.
SQLite holds metadata and state. Markdown bodies and uploaded media are files on
local persistent storage. There is no production Node service, public signup,
external database, or cross-origin browser API.

| Component | Responsibility |
| --- | --- |
| `backend/cmd/blog` | HTTP server, bootstrap, maintenance, backup and recovery commands |
| `backend/cmd/publisher` | Scoped publishing CLI, durable delivery and optional lesson scheduler |
| `backend/internal` | Authentication, authorization, content, configuration, media, quizzes and storage |
| `backend/migrations` | Ordered, checksummed SQLite migrations |
| `frontend/src` | React routes, owner dashboard, public readers, API client and safe Markdown rendering |
| `ui/src` | Shared components, EN/AR/FR labels, providers, fonts and generated tokens |
| `design-system` | Canonical tokens, generation scripts and shared component stylesheet |
| `deploy` | Docker, Compose, reverse-proxy and systemd examples |
| `scripts` | Development, quality, publishing, backup and release tooling |

## Persistence and publication

Only Go accesses the database and managed files. SQLite uses foreign keys, WAL,
bounded busy timeouts, and short transactions. Run one application instance on a
local filesystem; network filesystems and horizontal replicas are unsupported.
A runtime lease prevents two servers using the same data root.

Every save creates an immutable Markdown revision. Database metadata records its
relative path, checksum, and parent revision. Publication selects an exact saved
revision; editing a draft does not change the live version. Optimistic versions
reject conflicting edits. Idempotency keys make exact retries repeatable without
creating duplicate revisions. Migrations are append-only and checksummed.

Translations publish independently after review against their source revision.
Missing content translations use explicit language navigation, never silent
body fallback. Public profile and interface labels have documented fallback rules.

## Access boundaries

Owner access requires a passkey or password plus TOTP. Recovery permits restricted
factor re-enrollment and requires a successful replacement sign-in. Publisher
tokens have separate draft, media, quiz and publish scopes, plus collection limits.
OAuth grants inherit their parent token's limits and can be revoked independently.

Published gated lessons still require server-verified prerequisites. The same
policy protects bodies, media, metadata, discovery, previews, feeds and social cards.
CSS visibility is not access control. Browser-scoped learning identities are
separate from owner sessions. Quiz grading and progress changes are transactional;
answer keys are withheld from reader questions.

Markdown is rendered by one safe frontend pipeline for previews and public reading.
Raw HTML, executable MDX, arbitrary scripts and uncontrolled remote embeds are not
supported. Logs omit secrets, query strings and draft bodies. Browser writes use
same-origin and CSRF checks; machine publishing uses scoped bearer authentication.

## Configuration and operations

Branding, profile and labels are versioned application settings. Operator origins,
authentication keys and filesystem paths are private runtime configuration, never
browser bundle variables. Production serves only manifest-listed build assets.

A backup combines a coordinated SQLite snapshot with all referenced files.
Restoration invalidates credentials and learning progress, enters maintenance,
and requires tested re-enrollment before resuming. Export/import is a separate,
draft-only content portability format. See [recovery](operations/recovery.md).

Optional [MCP](integrations/mcp-oauth.md) and [lesson delivery](integrations/lesson-delivery.md)
reuse the publisher's authorization and revision rules. Notion is an independent
destination; there is no automatic two-way content synchronization. Scheduled
generation runs in the publisher process with separately configured credentials.

For detailed contracts, see [configuration and learning](architecture-appendix-configurable-learning.md),
[API](api/README.md), [UI](ui-contracts.md), and [design](design.md).
