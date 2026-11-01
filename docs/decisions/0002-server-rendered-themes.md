# ADR 0002: Server-rendered public pages and theme packs

[العربية](0002-server-rendered-themes.ar.md) · [Decisions](README.md) · [Public pages](../public-pages.md)

- Status: accepted
- Date: 2026-11-01
- Decider: owner (Mourad Bougarne)

## Context

Public pages are a React SPA. The Go server already renders an HTML shell
with a Go `html/template` (`backend/internal/http/server.go`): escaped
metadata, Open Graph tags, `hreflang` alternatives, the brand palette CSS,
and a JSON boot blob — then an empty `<div id="root">`. The article body
still needs JavaScript ([public pages](../public-pages.md)).

Facts that shape this decision:

- `content.Service.ResolvePage` (`backend/internal/content/routes.go`) is
  already the single public contract shared by HTML delivery and SPA
  navigation. It returns a renderer-neutral `PageRoute` view model and
  decides 200/403-locked/404/503 before anything renders.
- Markdown becomes HTML only in the browser (`react-markdown`, `remark-gfm`,
  `remark-math`, `rehype-katex`, plus the custom media/link/code renderers
  specified in [the Markdown reader contract](../markdown-reader.md)). The
  backend uses goldmark only to validate and collect media references.
- The brand palette and the social-card logic are duplicated between Go and
  TypeScript; CSP is strict and nonce-based; every response is `no-store`
  because access is learner-aware.
- There are no feeds or sitemap today; both need server-rendered output.

A theme system (owner-selectable designs, later installable packs of
templates + CSS + assets) is not reachable from a Webpack-compiled SPA.

## Decision

Move public pages from the React SPA to Go server-rendered templates, one
page at a time, behind the same URLs and the same `ResolvePage` contract.
The React app remains the owner dashboard. A **theme** is a pack of Go
templates, CSS, and static assets rendering the public view models. Ship
three built-in themes compiled into the binary; the owner picks one in
Settings. Interactive learning features remain JavaScript islands mounted
into server-rendered pages. Access control, status codes, locked
projections, and `noindex` behavior do not change.

## Host/theme boundary

| Side | Owns |
| --- | --- |
| Server (Go) | Routing, access decisions, status codes, view-model assembly, Markdown→HTML rendering, sanitization, CSP nonces, caching headers, label/locale fallback |
| Theme | Layout and presentation: templates over the view models, CSS built on design-system tokens, static assets |

- Templates receive only safe public projections: `PageRoute`,
  `siteconfig.Public`, resolved labels, and the rendered body HTML. No
  database handles, no secrets, no draft or gated content — a template can
  never leak what the view model does not contain. The 403-locked view model
  stays the safe teaser projection.
- Themes contain **no JavaScript and no Go code**. Interactivity comes from
  host-owned, nonce-tagged island bundles; built-in templates are
  `embed`-compiled and third-party packs (a later stage) are validated,
  checksummed files under the data root, installed like plugins
  (ADR [0001](0001-wasm-plugin-host.md)).
- Theme CSS uses the design-system tokens and logical properties, so RTL
  Arabic, light/dark, and owner branding work in every theme.
  `design-system/tokens.json` remains the source of truth and
  `Palette.CSS()` keeps emitting owner colour overrides into the shell.
- The theme choice is a new field in the existing `site` configuration
  document, so it is versioned, previewable, restorable, and needs no new
  configuration kind (the `config_documents.kind` set is closed by a CHECK
  constraint).

## Markdown rendering parity (the main risk)

Server rendering requires a Go Markdown→HTML pipeline with exact parity to
the [Markdown reader contract](../markdown-reader.md): GFM, the safe-link
policy, media-ID images/video/captions/posters, PDF links, presentations,
code blocks (LTR inside Arabic pages), takeaways — and no raw HTML.

- Build it on goldmark (already a backend dependency for validation), with
  custom renderers for the media and policy extensions.
- Math: there is no Go KaTeX. Math stays a client-side enhancement — the
  server emits the TeX source in marked elements and includes the KaTeX
  island only on pages that contain math. Content without math (most of it)
  ships zero rendering JavaScript.
- Parity is enforced by shared fixtures: every construct in the contract gets
  a fixture rendered by both pipelines and compared in CI while both exist.
  The browser pipeline is removed per page as pages migrate, and entirely
  once no SPA public page remains.

## Interactivity as islands

Quiz player, progress, mark-as-read, and live search remain JavaScript,
initially the existing React features (`ReaderLearning`, `BrowserProgress`,
search) mounted as islands into server-rendered pages, calling the same
learning APIs with the same CSRF/origin rules. This avoids re-implementing
grading-adjacent UI during the migration. Slimming islands down later is an
optimization, not part of this decision. All island scripts are
manifest-listed, nonce-tagged assets; CSP stays strict with no inline
scripts or styles beyond the existing nonce'd brand block.

## Security model

- Access is decided in Go before any template runs; templates render the
  decision's safe projection. A theme cannot turn a locked lesson into a
  readable one because the body HTML for locked content never exists.
- `html/template` contextual escaping plus sanitized, host-rendered body
  HTML; themes interpolate data, they do not compose raw HTML.
- Caching stays `no-store` in this phase. Selective caching of anonymous
  public pages is a separate later decision; gated and learner-aware pages
  remain uncacheable by contract.
- Third-party theme packs (later): owner-installed with checksums recorded
  in SQLite, template parsing in a restricted function set, size limits, and
  no ability to add routes, scripts, or fetch remote resources.

## Storage and backup/restore impact

- Built-in themes are in the binary: no storage, backup, or restore impact.
- The selected theme is configuration: covered by existing config revisions,
  history, restore, and export.
- Later theme packs live under the data root like media and plugins, so the
  coordinated backup covers them; restore re-verifies checksums and falls
  back to a built-in theme with an owner notice if a pack is missing.

## Migration path from the current code

Page by page, each behind the same URL, with a per-route switch between
template rendering and the SPA shell (code-level allowlist plus an operator
override for rollback). `ResolvePage`, the API endpoints, and the SPA keep
working throughout, so every step is independently shippable and reversible.

1. **Renderer first:** the goldmark pipeline with the parity fixture suite —
   no route changes.
2. **Article page** (`/{locale}/articles/{slug}`): the highest-value page for
   readers without JavaScript and for indexing; exercises the renderer,
   social metadata (consolidating `social.ts` duplication into Go), and the
   first theme's layout.
3. **Home, about, writing, search:** profile- and list-driven pages; search
   and filters degrade to query-parameter navigation with an island for
   live search.
4. **Collections and outlines** (`/learn`, `/docs`, outlines): server-side
   locked/soon/open states straight from the existing decisions.
5. **Lesson pages:** last, with the learning islands; grading and progress
   behavior unchanged.
6. **Remove** the public SPA routes, `PublicPage.tsx` and the duplicated
   social/palette client code; the dashboard bundle remains.

Feeds and a gated-aware sitemap become straightforward after stage 4 and are
their own follow-up, not part of this record.

## Staged delivery plan

| Stage | Delivers |
| --- | --- |
| 1 | Go Markdown renderer + parity fixtures |
| 2 | Template infrastructure, per-route switch, article page in theme one |
| 3 | Home/about/writing/search; social-card consolidation |
| 4 | Collections, outlines, lesson pages with islands |
| 5 | Three built-in themes, theme picker in Settings, SPA public-route removal |
| 6 | (Later, with ADR 0001 mechanisms) installable theme packs |

Each stage is a branch and PR with focused Go tests plus the existing
browser checks at 320/390/1280px in EN/AR/FR and both appearance modes.

## Alternatives considered

- **Keep the SPA and add themes as CSS-only skins:** no template control, no
  JavaScript-free reading, no feeds/sitemap story, and theme authors would
  be constrained to restyling fixed markup. Rejected.
- **Node server-side rendering of the existing React pages:** real parity
  for free, but it adds a production Node service, which the architecture
  explicitly excludes. Rejected.
- **A Go HTML-over-the-wire approach (htmx-style) for interactivity:**
  attractive later, but replacing the quiz player's grading UX during the
  migration adds risk for no reader benefit. Islands first.
- **Big-bang rewrite of all public pages at once:** rejected; the per-page
  switch keeps every step reversible and reviewable.

## Consequences and risks

- Positive: readable without JavaScript, faster first paint, one place
  (Go) for metadata/social/palette logic, and a real theme platform that
  later composes with plugin page fragments (ADR 0001).
- Costs: a second rendering pipeline during the migration window, parity
  fixtures to maintain, and template-facing view models become a
  compatibility surface for theme authors.
- Risks: Markdown parity gaps (mitigated by the fixture suite and the
  per-route rollback switch); math rendering regressions on the few math
  pages (bounded by the island approach); theme-pack security is deferred
  to its own stage rather than solved implicitly.
