# Public pages

[العربية](public-pages.ar.md) · [API](api/README.md)

The public site connects to applied configuration and
published revisions. The Go process serves the HTML shell and API; Webpack builds
React. The shell contains escaped title, description, canonical URL, published
language alternatives and Open Graph metadata. The article body still needs
JavaScript; this is not server-side rendering or JavaScript-free indexing.

## Routes and visibility

Replace `en` with `ar` or `fr` for the other explicit language routes.

| Route | Content |
| --- | --- |
| `/en` | Applied introduction, portrait/banner, Now, writing and learning summaries |
| `/en/about` | Applied professional profile/contact and optional published About Markdown |
| `/en/writing`, `/en/search` | Published metadata, URL filters and cursor pagination |
| `/en/articles/{slug}` | Published article |
| `/en/learn`, `/en/docs` | Nonempty visible active learning paths or documentation guides |
| `/en/learn/{path}`, `/en/docs/{guide}` | Ordered active public outline |
| `/en/learn/{path}/{slug}`, `/en/docs/{guide}/{slug}` | Published member with previous/next navigation |
| `/en/doc/{slug}` | Published document whose localized guide is not active/visible yet |

A document's standalone path is valid only while no localized active guide path
exists; the guide path becomes canonical when available. A wrong kind, wrong
collection, unpublished member, missing locale or unknown page returns a real
404. Arabic slugs are percent-encoded in canonical links. Encoded separators,
backslashes and dot traversal are rejected. Canonicals use `BLOG_BASE_URL`, never
forwarded Host headers. Private owner routes retain authentication and noindex.

`GET /api/v1/public/page?path=/en/articles/{slug}` uses the same resolver as HTML.
It returns `kind`, `locale`, `url`, `title`, `summary`, `alternatives`, and optional
`content`/`collection`. A locked lesson returns 403 with this safe page projection,
without Markdown/media, and noindex. HTML does likewise. This page endpoint's
403 shape differs from the older content endpoint's `{error,teaser}` shape.
Missing/corrupt files produce an error (HTML 503/API 500), never blank success or
a draft fallback. Owner cookies do not change a public page into a preview.

Public alternatives and collection-entry URLs now point to canonical pages;
existing content JSON endpoints remain available. Only published locale versions
with a resolvable public path are alternatives. Missing translations offer
explicit site-language choices; content is never silently replaced by English.
Profile and interface-label fallback remains allowed and marked with language and
direction where profile text is rendered.

`GET /api/v1/public/collections?locale=en&kind=learning&limit=20&cursor=…`
returns `{items,next_cursor}`. Kind is `learning` or `docs`; limit is 1–20.
The stable key cursor excludes empty/inactive/private outlines. Cards and outlines
show published/announced counts and server-backed completion. Soon entries are text
without key, slug, URL or draft title. Public positions hide private gaps. Open/locked
states, grading and completion are server decisions. `ReaderLearning` connects
quizzes, prerequisite links, mark-as-read and browser-progress reset to the
[learning API](api/learning.md); an explicit interaction creates the browser identity.

## Archive and reading

Kind/category/tag/query combine with AND. URLs retain `kind`, `category`, `tag`,
`q` and `cursor`; Back restores them. Clicking a selected tag clears it. Requests
are aborted and obsolete responses ignored; failures offer retry and empty
filtered results offer reset. Search matches SQLite published titles/summaries,
not Markdown bodies. Lists include publication date/year, kind, localized
category/tags, safe lock metadata and an optional reading estimate.

Migration 0007 stores an estimated duration when a revision is written, using
whitespace-separated Markdown words at 200 per minute, rounded up. Older
immutable revisions retain 0 (unknown), so discovery omits their estimate.
Locked discovery omits it too. Reading pages can derive it from the authorized
body. Discovery never reads files to calculate it. This is an estimate, not a
promise about reading speed.

Readers use the same sanitized Markdown/media pipeline as preview. The takeaway
is authored Markdown, never generated prose. Author/profile facts come from
applied settings. Guide dates reflect the last publication. No unpublished
content, illustrative UI-kit examples or credentials are seeded.

## Verification

Run `node scripts/project.mjs check`, `go -C backend test -race ./...`, and
`node scripts/project.mjs build`. After installing Playwright Chromium, run
`node scripts/smoke-production.mjs` and `node scripts/smoke-auth.mjs`.
The latter invokes `smoke-public.mjs` with temporary owner/content fixtures.

Go tests cover canonical kind/membership checks, Unicode slugs, published-only
metadata, locked/noindex behavior, private draft isolation and corrupted files.
The browser checks cover empty installations at 320/390/1280px in EN/AR/FR and
both themes; configured profile/readers/outlines; menu Escape/focus; query filters,
Back and delayed obsolete requests; retry and missing translations. Screenshots
are written to `frontend/test-results/` for local review. Virtual
WebAuthn remains simulation; verify on real target devices.

## Header

The public header reuses the Version B shared pill navigation and circular tools. Section links contain text only. The wordmark, monogram or uploaded image is a plain home link without a menu, chevron or pill background. Tools appear in search, language, theme order; a separate menu button provides phone navigation. The language button opens EN/AR/FR choices, focuses the first choice and supports Escape with focus return. Content language availability remains enforced. Authenticated owners can reach the dashboard from the footer.  for comparison sources and verification limits.

## Home and profile

Public pages reuse the shared kit for primary action/social/RSS links, Now rows, year-grouped writing and Everything, full-card learning links, rounded live search, portrait/experience About columns with email and links, author card, lesson sidebar and navigation. State remains server-derived; no locked bodies, private announcements or invented progress are rendered.

## Reader metadata

Writing shows category/tag filters without an additional search form; use the header Search button. Article and lesson headers use the category/type sticker, title, summary, portrait/name and publication date with the existing approximate reading time (saved Markdown words / 200, rounded up; no AI). Omit the Updated prefix and bottom language links; the header language menu retains explicit published-language availability. Progress cards have space above and below, including beside Mark as read. Configured Profile → Contact → Footer text replaces the default contact sentence; clear it to restore the default email-linked call to action. No saved profile values are changed.

## Footer destinations

Footer navigation links to localized Writing, Learning and Docs, then the first safe visible configured profile link for each GitHub, LinkedIn and RSS icon. Missing destinations are omitted; choosing RSS does not create a feed endpoint. Owner navigation remains restricted by session state. Primary Home/About styling follows Profile Main action; symbols do not select button color. [Owner setup](owner-tools.md#quizzes-and-lesson-gates).
