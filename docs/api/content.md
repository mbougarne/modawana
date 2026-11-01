# Content, discovery and collections API

[العربية](content.ar.md) · [API index](README.md) · [OpenAPI](v1.openapi.json)

Content services support public readers, owner editing, quiz gates and the separate [scoped publisher API/CLI](publisher.md). No content is seeded. Run the
owner setup in [owner security](../owner-security.md) first. Private routes require
a complete owner session; writes also require exact Origin and `X-CSRF-Token`.
Recovery/enrollment cookies and bearer tokens do not authorize these routes.

All paths below are relative to `/api/v1`. JSON is bounded, unknown fields are
rejected, and responses use `no-store`. Public content never contains a private
revision history, storage path or original upload filename. Public `url` fields
currently identify API resources; HTML reader routes are documented in [public pages](../public-pages.md).

## Drafts and history

| Method/path | Contract |
| --- | --- |
| GET /content?cursor=… | Private item list, 50 maximum, stable key order |
| GET /content/{key}/{locale} | Current draft, version, live revision ID and translation review/stale state |
| PUT /content/{key}/{locale} | Complete new draft snapshot; 201 on new locale, otherwise 200 |
| GET /content/{key}/{locale}/revisions?cursor=… | Private history, newest timestamp/ID first, up to 50 |
| GET /content/{key}/{locale}/revisions/{revision_id} | Private immutable historical document |
| POST /content/{key}/{locale}/restore | Copy the selected history into a new draft; live pointer unchanged |

A save requires `expected_version`, `kind`, `source_locale`, `slug`, `title` and
`markdown`. Optional fields: `summary`, `source_revision_id`, `category_key`,
`tag_keys`, `cover_media_id`, `media_presentations`, `collection_key`, `position`.
Use version 0 only for a new language version. Stable keys are lowercase ASCII
letters/digits, `_` and `-`, at most 120 characters, beginning with a letter/digit.
Source locale is fixed at creation. Kind (`article`, `lesson`, `doc`) changes only
through an owner Save or the compatible explicit conversion endpoint. Slugs
allow Unicode letters/numbers, `_` and `-`, at most 160 characters, and become
fixed after first publication, even after unpublication.

Limits: 1 MiB UTF-8 Markdown, 200-character title, 500-character summary, one
optional category and eight distinct tags. Goldmark parses references, including
reference-style Markdown; raw HTML, unsafe link schemes, external images,
unresolved relative attachments and malformed `/media/{id}` links are rejected.
Ordinary HTTP(S)/mailto links and root-relative/fragment links remain links.
Media must already exist. Video presentations name `media_id`, optional
`poster_media_id`, `caption_track_media_id`, `caption` (2000 characters) and
`transcript` (20000). Covers/posters must be images and tracks must be captions.
All auxiliary media are retained references and obey the parent's access rules.

Every save writes a fresh durable file, including metadata-only changes, then
commits the draft pointer and references. A stale expected version returns 409.
Optional `Idempotency-Key` (1–128 ASCII letters/digits or `._:-`) records the
result atomically for seven days: identical operation/payload replays the result;
a changed payload returns 409 `idempotency_conflict`. No body copy enters SQLite.
After expiry, version checks still prevent blind overwrites. Automated requests
require this header on the separate scoped publisher routes.

Restore uses `{expected_version,revision_id}` and preserves saved media/taxonomy.
It copies the old source linkage but never inherits translation review. A published
slug remains stable even when restoring a revision with an earlier draft slug.
History files stay immutable; do not edit them directly.

## Review and publication

`POST /content/{key}/{locale}/publish` requires `{expected_version,revision_id}`
for the exact current draft. It verifies the Markdown and every referenced media
checksum before changing the live pointer. `unpublish` uses the same fields for
the exact current live revision and retains all history. Unpublication remains
possible even when content bytes are damaged. Both support retry keys.

A translation save records the same item's source-language revision. Before a
new translation publication, the owner calls `review` with
`{expected_version,revision_id,source_revision_id}`. The source must be currently
published and match the translation's exact recorded source. Review advances the
version, so use the returned counter for publication. A source publication change
marks the old live translation stale without deleting it; stale or unreviewed
translations cannot newly publish. Source withdrawal also makes new publication
unavailable. Service hooks run in the transaction for later quiz/path policies.

- `GET /public/content/{locale}/{slug}` returns only the current published revision.
  Unknown/unpublished content is 404. A locked lesson is 403 `lesson_locked` with
  a safe `teaser`; it has no Markdown, manifest or protected presentation metadata.
- `GET /public/alternatives/{key}` lists published locale/slug/API URL choices only.
  The content response contains the same alternatives; no implicit language substitution.
- New requests observe publication/unpublication immediately. An already loaded
  browser response or downloaded media cannot be retracted.

## Taxonomy and discovery

`PUT /taxonomy/{category|tag}/{key}/{locale}` accepts
`{expected_version,label,slug}`. IDs and keys remain stable; version is shared
across locales. Labels are at most 120 characters, keys at most 80, slugs at most
160, and each kind has a 500-term limit. Owner edits to shared localized labels
apply immediately; revision membership only changes on publication.
`GET /taxonomy?locale=en` includes private terms; `/public/taxonomy?locale=en`
contains only terms used by published records in that locale, with published counts.

`GET /public/content` and `/public/search` accept required `locale` plus optional
`kind`, localized category `slug` as `category`, localized tag `slug` as `tag`,
`q`, `limit`, `cursor`. Filters combine with AND. Search is literal title/summary
substring matching, at most 200 characters; SQLite's default lowercasing is ASCII,
with no stemming or body search. Default page size is 20, maximum 50. Order is
publication time descending then immutable version ID; the opaque cursor is bound
to the filter/page-size combination. Counts omit unpublished locales. Published
lessons may appear as locked summaries. Pagination reads current state per request;
publication changes between pages are not a pinned snapshot.

## Collections and active plans

| Method/path | Contract |
| --- | --- |
| GET /collections?cursor=… | Private collection list, up to 50 |
| GET /collections/{key} | Locales, current version, draft/active plan IDs and members |
| PUT /collections/{key}/{locale} | `{expected_version,kind,slug,title,summary,visible}`; kind is learning/docs |
| PUT /collections/{key}/plan | `{expected_version,members:[{key,announcements:{en:"Explicit title"}}]}` |
| POST /collections/{key}/apply | `{expected_version,plan_id}`; exact current draft plan |
| GET /public/collections/{locale}/{slug} | Active visible locale, published/explicitly announced entries only |

A collection holds at most 500 members. Lessons belong to learning paths, docs to
guides, articles to neither. An item is assigned once; cross-collection moves are
rejected. `collection_key`/`position` on a draft can assign membership (position
1–500); it also advances the collection version. Existing positions must match;
use the plan endpoint to reorder. Plan array order defines unique positions.
Saving a plan changes no public order or announcements. Applying atomically swaps
the active pointer and positions after checking the shared collection version.
Published membership cannot be removed and collection/item slugs remain stable.

`announcements` supplies deliberate public titles for each locale. Omitted values
remain hidden; no draft title or count is copied. An announced-only collection can
be visible under the appendix's explicit roadmap extension. A Soon entry has no
key, slug or URL; its content route stays 404. Public positions count only visible
entries, hiding private gaps. `published_count`, `announced_count` and `total` do
not represent learner completion. Disabled/missing locales and empty public
collections return 404.

Lesson bodies require membership in the active plan. Open plans use
`gate_policy_version=0`; reviewed quiz gates use version 1 and require all earlier
applicable boundaries in the current progress epoch. Missing plans and unsupported
policies fail closed. Reordering cannot silently move a lesson before its retained
prerequisite. Publishing a new gate assessment can atomically apply the matching
reviewed plan; epoch reset is a separate freshly authenticated owner action. See
[learning](learning.md) for the shared content/media/search/collection access rules.

## Verification

`go -C backend test ./...`, `go -C backend test -race ./...`, `go -C backend vet ./...`
and `node scripts/project.mjs check` verify domain, storage, authorization and
conflict boundaries. `scripts/smoke-content.mjs`, invoked by `smoke-auth.mjs`, uses
the real HTTPS owner session, Go server and temporary SQLite/files for the entire
upload → draft/retry → review/publish → public search → unpublish → collection
plan/announcement flow. No production content or credentials are created.

## Public page integration

Discovery adds canonical `url`, `category_key`, `tag_keys` and optional `reading_minutes`. Alternatives and collection-entry URLs now address public pages. Public collections add their canonical `url`; the new page resolver and paginated collection index are documented in [Public pages](../public-pages.md). Existing content JSON routes remain available.

## Bulk publication

Owner `GET /content?locale=en&kind=article&status=draft&q=…` entries also expose
`draft_revision_id` and `published_revision_id` (empty if unpublished).
`POST /content/bulk-publication` requires the existing owner cookie, Origin and
X-CSRF-Token. The body is at most 256 KiB and rejects unknown fields:

```json
{
  "operation": "publish",
  "items": [
    {
      "key": "example-article",
      "locale": "en",
      "expected_version": 3,
      "revision_id": "0123456789abcdef0123456789abcdef"
    }
  ]
}
```

Use `publish` with the observed saved draft revision, or `unpublish` with the
observed live revision (the draft revision for an already-unpublished no-op).
Selections contain 1–500 unique key/locale pairs; all expected versions are required.
The endpoint returns `{changed, unchanged}`. No-ops still validate observed versions
and revisions, but do not increment versions. Changes share the editor's checks in
one transaction: a conflict (409), invalid/review/plan failure (422), missing item
(404), or any other error rolls back all updates. Existing auth/CSRF errors apply.
No review or plan payloads, automatic reviews or partial-success mode are accepted.
No idempotency key is supported for this endpoint: after a lost response, read current
states before retrying with newly selected versions.

No files, revisions or drafts are deleted or rewritten. No schema migration is
needed. Unpublish returns to the existing draft/non-public state; it does not
clear first-publication slug locks. Existing discovery, protected media and learning
boundaries continue to derive visibility from the applied publication/plan state.

## Explicit type conversion

POST `/content/{key}/{locale}/type` accepts `{expected_version, from_kind, kind,
collection_key, expected_collection_version, position}`. Target collection/version
are required for lesson/doc; zero position appends to existing initial membership.
Article requires no collection/position. Response is RevisionResult using the same
revision ID and incremented version. Optional Idempotency-Key scopes retries to the
owner and exact payload. All languages increment versions together. Types in current
draft/active plans, docs with current quizzes and About-selected docs reject with
actionable `error.fields`. Missing/stale versions never overwrite. Public aliases
retain current access/gating.

## Editable type and private reference choices

Owner PUT draft saves can change kind and prose atomically. For a new lesson/doc
type on an existing item, include collection_key, expected_collection_version and
position (0 appends). The shared conversion transaction preserves publication/history
and checks dependencies; failure rolls back the new draft and type together. Other
locale versions increment once. Publisher/import operations cannot retype existing items.

GET /owner/references?locale=en&kind=learning&q=… returns {items,more}, at most five
{key,title,kind,version} choices. kind supports learning/docs/lesson/doc; collection_key
optionally restricts content members. Full owner session required, no-store. Search
uses all matching owner metadata, with explicit locale and no content-body fallback.

## Lesson assignment and first plan

Owner drafts return collection_key and position; collection documents return ordered assigned_members separately from draft_members. Assignment does not imply a saved plan. Position 0 on Save automatically appends an unassigned item or retains the assigned position without moving it. The editor saves a private initial plan when needed before Publish with path plan, using the returned ID/version. Publication and activation remain atomic; a failed plan save prevents publication. Translation, visibility and access gates remain unchanged.

## Simple collection save

PUT /collections/{key}/workspace?locale=en takes collection metadata plus members [{key,announcements,expected_version?}] in one transaction covering guarded conversion, plan and activation without publishing drafts. Collection version and owner/Origin/CSRF checks are mandatory; article version is required for conversion. Optional Idempotency-Key protects retries. Any failure rolls everything back. Legacy plan APIs remain compatible. CollectionDocument adds owner-only member_titles. Reference kind learning_item/docs_item offers articles and matching unassigned/same-collection content; repeated exclude_key values are excluded before the five-result limit.

## Search capability marker

/owner/references sets X-Reference-Search-Version: 2 before session validation. It contains no private metadata. The picker distinguishes a current 422 validation response from an older backend lacking the new search kinds. A missing route gives restart guidance; authentication and the existing contract are unchanged.

## Owner titles and field reasons

GET /collections accepts optional locale (default en) and returns title alongside key/kind/version. Private title fallback uses English/first available title; public content locale and key-cursor order are unchanged. Collection 422 errors include safe fields for title/slug/key/locale/summary/members and precise collection_* reasons, including duplicate slug, foreign membership, removed publication and prerequisite order/review. Never echo submitted values or SQL. Slugs remain explicit API input; title generation is owner UI behavior.
