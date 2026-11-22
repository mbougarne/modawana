# Public pages

[العربية](public-pages.ar.md) · [API](api/README.md)

The public site uses applied settings and published revisions. Go renders Home,
About, Writing, Search, collection indexes, outlines, and readers from the
selected built-in theme. Published text and navigation are available without
JavaScript. React powers the owner dashboard and interactive reader controls.
The same Go resolver supplies metadata, canonical URLs and published language
alternatives for every theme.

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

## Server-rendered public pages

The [built-in themes](decisions/0002-server-rendered-themes.md) use embedded Go
templates. The HTTP host supplies public view models for profile
details, writing and learning summaries, archive results, and article or About
Markdown sanitized by the Go renderer. About uses the published document in
the current language. Locked discovery results show a safe summary and lock
indicator without a link to the body.

Open **Themes → Public appearance** in Slate (or **Settings → Public appearance** in Classic) to choose **Classic** (the original layout),
**Folio** (serif headings and editorial lists), or **Studio** (a centered hero
and cards). **Preview theme** opens the homepage in a private tab using
published content and applied branding. Apply the settings revision to make
the theme active in all public languages. Restoring a revision and applying it
also restores its theme. Revisions that predate the picker use Classic.

Go serves all public routes, including localized 404 and 503 pages, using the
applied theme. The React application serves the owner and sign-in routes.

The server renders the complete reading and navigation layout. Search uses a GET
form, and archive filters and pagination use ordinary links. The `public-controls`
island debounces live searches, cancels obsolete requests, and progressively
enhances appearance, menus and About email copying. Articles retain native
menus and follow the stored or system appearance preference. Articles with math
load the manifest-listed KaTeX island; About, lessons, and documents add it when
needed. If the KaTeX bundle is unavailable, the underlying TeX stays readable.

Learning paths and guides render their current server-resolved outline. Open
members link to their readers. Locked and announced members appear as text.
A locked lesson returns HTTP 403 with a public summary, available prerequisite
links, and `noindex`. Its Markdown, media, and quiz questions stay behind the
same access decision as the public APIs.

The `reader-learning` island mounts the shared quiz and browser-progress
controls. It reads quiz state through the existing learning API. Grading and
mark-as-read use their existing server endpoints. Progress changes refresh
the outline and neighboring links while retaining the current quiz result.
Without the reader bundle, published text, outlines, and access notices remain readable.
The documentation index also links to published guides and allows a published
document to be selected with `?doc=…`.

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
The latter invokes the public and learning smoke tests with temporary fixtures.
It loads the themed indexes and a lesson with JavaScript disabled, submits a
Search form, and exercises quiz submission, lesson unlock, and progress reset.

Go tests cover canonical kind/membership checks, Unicode slugs, published-only
metadata, locked/noindex behavior, private draft isolation and corrupted files.
The browser checks cover empty installations at 320/390/1280px in EN/AR/FR and
both themes; configured profile/readers/outlines; menu Escape/focus; query filters,
Back and delayed obsolete requests; retry and missing translations. They also
load the server-rendered article directly at 320/390/1280px in EN/AR/FR and
both themes. Plain articles omit the SPA bundle and reader islands. The math
article fixture loads the KaTeX island. Screenshots
are written to `frontend/test-results/` for local review. Virtual
WebAuthn remains simulation; verify on real target devices.

## Header

The public header reuses the Version B shared pill navigation and circular tools. Section links contain text only. The wordmark, monogram or uploaded image is a plain home link without a menu, chevron or pill background. Tools appear in search, language, theme order; a separate menu button provides phone navigation. The language button opens EN/AR/FR choices, focuses the first choice and supports Escape with focus return. Content language availability remains enforced. Authenticated owners can reach the dashboard from the footer.  for comparison sources and verification limits.

## Home and profile

Public pages reuse the shared kit for primary action/social/RSS links, Now rows, year-grouped writing and Everything, full-card learning links, rounded live search, portrait/experience About columns with email and links, author card, lesson sidebar and navigation. State remains server-derived; no locked bodies, private announcements or invented progress are rendered.

## Reader metadata

Writing shows category/tag filters without an additional search form; use the header Search button. Article and lesson headers use the category/type sticker, title, summary, portrait/name and publication date with the existing approximate reading time (saved Markdown words / 200, rounded up; no AI). Omit the Updated prefix and bottom language links; the header language menu retains explicit published-language availability. Progress cards have space above and below, including beside Mark as read. Configured Profile → Contact → Footer text replaces the default contact sentence; clear it to restore the default email-linked call to action. No saved profile values are changed.

## Footer destinations

Footer navigation links to localized Writing, Learning and Docs, then the first safe visible configured profile link for each GitHub, LinkedIn and RSS icon. The footer uses the built-in feed for the active language when no RSS destination is configured. GitHub and LinkedIn links are omitted when their destinations are missing. Owner navigation remains restricted by session state. Primary Home/About styling follows Profile Main action; symbols do not select button color. [Owner setup](owner-tools.md#quizzes-and-lesson-gates).

## RSS and sitemap

Subscribe at `/en/feed.xml`, `/ar/feed.xml`, or `/fr/feed.xml`. The shortcut
`/feed.xml` redirects to the default-language feed. Public pages advertise
their localized feed in the HTML head. Each RSS 2.0 channel lists up to 30
recent articles, documents, and lessons readable without signing in. Entries
include the published title, summary, canonical link, and latest publication
date. An entry keeps its ID across updates to the same language version.

`/sitemap.xml` lists canonical Home, About, Writing, Learn, and Docs pages in
all supported languages, plus anonymously readable content and collection
outlines with at least one readable published member. `/robots.txt` advertises
the sitemap. Public URLs use `BLOG_BASE_URL`, including encoded slugs. The
sitemap follows the protocol's single-file limits and returns 503 if the
generated document exceeds those limits. [Sitemaps protocol](https://www.sitemaps.org/protocol.html)

Feeds and the sitemap use anonymous access even when the request carries an
owner session or a learning identity. Locked lessons, planned entries, and
drafts are excluded. Feed summaries come from published revisions and are
escaped as text. Both XML endpoints support GET and HEAD and use `no-store`
so publication and access changes appear on subsequent requests.

## Installable theme packs

Stage a pack as `themes/pack-editorial/theme.json` under the external data root.
The pack ID is its directory name. It starts with `pack-`, followed by a
lowercase letter and up to 30 lowercase letters, digits, or hyphens. A pack has
one JSON file. The service user needs read access to that file and its directory.

```json
{
  "api_version": 1,
  "name": "Editorial",
  "layout": "{{template \"header\" .}} {{template \"content\" .}} {{template \"footer\" .}}",
  "css": ".vb-shell { max-inline-size: 72rem; } .vb-title { letter-spacing: 0.02em; }"
}
```

The layout arranges the host's `header`, `content`, and `footer` partials, each
exactly once. An optional base64 `banner_png` field adds a decorative image and
requires one `{{template "banner" .}}` slot. The PNG is limited to 96 KiB and
1600 × 800 pixels. The complete JSON is limited to 192 KiB. The host supplies
the document head, locale, reader content, and interactive controls.

The optional CSS accepts comma-separated tag or class selectors with descendant
combinators, a [list of presentation properties](../backend/internal/theme/pack_css.go),
and up to 24 KiB of source. Every selector is scoped to the pack. CSS functions
are limited to design tokens, arithmetic, colours, and grid sizing. The pack
format accepts layout slots, scoped CSS, and the optional local PNG. The host
handles scripts, routes, and access decisions.

In **Themes → Public appearance** in Slate (or **Settings → Public appearance** in Classic), review the candidate name and checksum,
then choose **Approve pack**. Use a fresh owner session to approve or remove a
pack. Approved packs appear in the public theme picker and support the same
private preview and settings revision workflow as built-in themes. The picker
remembers the pack ID across revisions.

Editing a pack requires a new approval. The site uses Classic while the active
pack is missing, changed, or invalid, and Settings shows the fallback. Approving
an update to the selected pack makes the update live. **Remove approval** keeps
the staged file and uses Classic if the pack was selected. Coordinated backups
retain approved matching pack files and restore verifies their checksums again.
Content transfer carries the settings reference. Stage and approve the pack at
the destination before applying that revision.
