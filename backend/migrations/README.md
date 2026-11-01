# SQLite schema and migrations

[العربية](README.ar.md) · [Backend](../README.md) · [Configuration](../../docs/api/README.md)

SQLite stores metadata, revisions, authentication, configuration, collections, quizzes, browser progress, publisher and OAuth state. Migrations are ordered and checksummed. A new database starts without an owner, credentials, profile, content or uploads.

## Files, connections and transactions

Go uses database/sql with modernc.org/sqlite v1.61.0 and its matching libc v1.79.0, pinned in go.mod/go.sum. Runtime is pure Go without Node or a separate database server. BLOG_DATA_ROOT contains blog.sqlite and SQLite's WAL/SHM files. The canonical root must be external to code/releases, private (0700), and owned by the service account; the main database and existing sidecars must be private regular files (0600). Missing directories are created privately. Symlink database files are rejected. The trusted operator controls this directory; do not grant other users write access to its ancestors.

Every physical connection applies foreign_keys=1, busy_timeout=5000 milliseconds, journal_mode=WAL, synchronous=FULL and trusted_schema=0. The pool has at most four connections. Transactions begin IMMEDIATE to serialize writers; keep writes short and never hold them while receiving uploads, rendering Markdown or making network calls. BumpVersion updates only code-allowlisted tables using an expected counter inside the same transaction as the content/configuration change. Zero affected rows means ErrVersionConflict; two writers with the same expected version cannot both commit successfully. Readiness pings SQLite with a one-second request deadline. Startup has a 30-second migration deadline.

NewID uses crypto/rand and produces 128-bit opaque lowercase hexadecimal IDs. UTC timestamps use fixed millisecond precision (`2006-01-02T15:04:05.000Z`) and SQL defaults are UTC. Timestamp checks reject local offsets and malformed instants. IDs for new records come from Go, not slugs, user filenames or uploaded metadata.

## Enforced data boundaries

| Records | Database guarantees |
| --- | --- |
| content_items / content_versions | Unique immutable content key, unique item/locale and locale/slug, EN/AR/FR only, positive version counter, stable slug after first publication |
| revisions | Unique immutable relative Markdown path, checksum/size, immutable metadata; composite foreign keys keep draft/live pointers in their own language version |
| collection_items | Positive unique position within a collection, one collection per item, lesson→learning/doc→docs membership; published membership cannot move/disappear |
| media and reference tables | Generated relative path/checksum/type/size metadata; retained revision/configuration references prevent media deletion |
| config_documents / config_revisions | Unique kind, immutable bounded JSON object snapshot, schema/colour algorithm versions, applied pointer belongs to its document |
| owner / sessions / api_tokens | Singleton owner, hashed unique token identifiers, expiry/revocation and scopes/collection grants; recovery cannot create a full owner session and password-only is not an owner method |
| idempotency_records | Unique actor/operation/key hash, request hash and bounded result identifiers/status/version; no raw token or request-body copy |

Markdown and upload bytes are never columns. Configuration JSON is only for bounded settings/profile/label data; About and all article/lesson/doc bodies remain Markdown files. Services enforce schema, translation review, permissions, MIME validation and file confinement. Foreign keys alone do not prove a file exists or grant public access. Immutable files are written before metadata is committed.

A first language version and its draft revision are inserted together in one transaction: generate both IDs, insert the version with the draft pointer, insert its revision and commit. The circular ownership keys are deferred until commit. Publication must also name the exact revision and check review/access rules in the content service. Application validation remains mandatory even where SQL has a matching constraint. Versioned path plans, quiz bindings, private grading, browser identities and public attempts share transactional constraints. Missing plans and unsupported gate policies deny lesson bodies/media.

## Migration and backup procedure

The binary embeds numbered SQL migrations, beginning with 0001_core.sql. Startup validates consecutive IDs and every applied name/SHA-256 checksum, then applies pending SQL and ledger entries in one transaction. It rejects unversioned pre-existing tables, changed history, gaps, a newer schema than the binary understands, and foreign-key corruption. SQL failure rolls back the migration and its ledger changes. Do not modify an applied migration: append the next unused sequential migration ID.

Before starting a newer binary against existing data:

1. Stop every application/writer and confirm they have exited. Keep the previous binary, frontend build and private operator configuration.
2. Back up the **entire** data directory while writers remain stopped. Include database/WAL/SHM and all Markdown/uploads, not just blog.sqlite. Place the backup outside the live data directory and protect its permissions.
3. Verify the copy and retain it until the new release is verified. The following Linux example assumes BLOG_DATA_ROOT is the actual canonical root and all writers are stopped:

```sh
umask 077
backup="${BLOG_DATA_ROOT}.backup-$(date -u +%Y%m%dT%H%M%SZ)"
cp -a -- "$BLOG_DATA_ROOT" "$backup"
diff -qr -- "$BLOG_DATA_ROOT" "$backup"
```

4. Start the new binary with the documented environment. Migration failure exits before HTTP starts. Check /readyz and the relevant release tests. For coordinated online backups and credential-safe restoration, use the [recovery guide](../../docs/operations/recovery.md). This copy procedure requires stopped writers.

There are no down migrations. Never run an older binary against an upgraded database, delete migration rows to force startup, or disable foreign keys. Restore a matching full backup and matching binary/build/configuration into a separate private data directory while stopped; verify it before switching the live path. If a newer release already accepted writes, reverting to an older backup loses those later writes, so plan a forward fix or explicit data reconciliation. No automatic incompatible downgrade or destructive repair is performed.

## Verification

From backend/ run `go test ./...`, `go test -race ./...`, `go vet ./...` and `go mod verify`. Database tests cover fresh creation/reopen, four actual connections, constraints and wrong-owner pointers, reference-protected media, settings history, collection ordering, two concurrent writers, recovery-session boundaries, migration append/rerun/checksum/gap/newer-schema/rollback and unsafe files. Test content lives only in temporary directories. CI also starts/restarts the real Go server against one data root, checking readiness and the production browser build. Hardware passkeys and recovery ceremonies are not tested by these database tests.

Migration 0006 adds immutable configuration translation reviews/audit and a public flag for each retained configuration-media reference. Inactive logo images and disabled banners remain private while their historical references prevent deletion.  See the [configuration contract](../../docs/api/configuration.md).

Migration 0007 adds `revisions.reading_minutes`, computed for new revisions. Historical immutable revisions retain 0 (unknown); discovery omits this estimate for unknown/locked content. No Markdown is rewritten.  See [Public pages](../../docs/public-pages.md).

See [learning API](../../docs/api/learning.md) for retention limits and `build/blog cleanup-learning`. Neither migration seeds learner data or stores Markdown bodies.

Migration 0010 adds durable completion independent of retained attempts; 0011 adds scoped publisher media ownership and explicit article permission. Existing data is preserved; neither seeds credentials. New migrations start at 0012.

## 0012 — Explicit content conversion

Append-only migration 0012 preserves existing items, versions and revision pointers.
It adds transaction conversion guards, type audit events and former public URL aliases.
Ordinary type/source-locale updates and published membership changes remain blocked.
Only the owner conversion service can detach an unplanned item and change type in
one transaction; active/draft plan members remain protected. Use the existing backup
and startup procedure above.

## 0013 — Collection removal

Adds a retained removal marker and prevents reactivation or new membership of removed
collections. The owner operation detaches documents, converts lessons to unpublished
articles and retains immutable plans/revisions. It requires the current collection
version. Back up before upgrading; removed keys remain reserved.

## 0014 — MCP OAuth

Adds bounded OAuth clients, consent/code requests, grants and consumed refresh
hashes. No credentials or permissions are seeded. Grants reference existing scoped
publisher tokens and are removed when those tokens are deleted. Restoration
clears all OAuth records. Back up and verify isolated restore with the old binary
before opening this schema with the candidate; use the matching old snapshot for
rollback. See [OAuth operations](../../docs/integrations/mcp-oauth.md).
