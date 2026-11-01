# Saved branding, profile and interface labels

[العربية](configuration.ar.md) · [API index](README.md) · [Schema](v1.openapi.json)

Owner pages are available at `/{en|ar|fr}/admin/settings`, `/admin/profile` and `/admin/labels`. A full owner session is required; recovery sessions and publisher tokens cannot edit them. Writes require the exact Origin and X-CSRF-Token. Public contact data is separate from private login and operator configuration. No personal facts are seeded.

## Revision workflow

For each of `/api/v1/settings`, `/api/v1/profile`, `/api/v1/labels`:

| Request | Result |
| --- | --- |
| GET base | Applied `{kind, version, revision_id, snapshot}`; defaults at version 0 before the first save |
| POST base/preview | Validate `{expected_version, snapshot}`; return normalized snapshot, `required_reviews` and `stale_reviews`; write nothing |
| PUT base | Validate the complete snapshot, expected version and reviews, then atomically insert immutable revision, media references, translation reviews, audit and applied pointer |
| GET base/history?cursor= | Up to 50 newest revision IDs/timestamps, plus `next_cursor` |
| POST base/restore | `{expected_version, revision_id}` returns a historical working copy; does not apply it |
| GET base/defaults | Return a default working copy at the current version; does not apply it |

Snapshots are bounded to 256 KiB; request bodies to 1 MiB. Unknown fields are rejected; site/profile snapshots require all top-level fields. Invalid values return 422; missing/negative apply versions return 422 and stale/concurrent saves return 409 (Preview requires a matching version). HTTP 409 preserves the local form until the owner chooses Reload. Preview/Cancel never changes live configuration. Reset and Restore also require explicit Save and apply. History references continue protecting files from deletion. New media stays private until an applied visible field references it; disabled banners and inactive image logos do not grant access.

Changed nonempty Arabic/French values require explicit confirmation of each `{path, locale, source_hash, value_hash}` from Preview. Hashes bind the exact value to effective English; stable row IDs survive reordering. An existing reviewed translation remains live when only English changes and is marked stale; it can be reviewed again. A restore is validated against current review rules. Profile/label fallback is permitted and marked with actual language/direction; published content never falls back.

## Field rules

Branding supports wordmark, one-to-three-letter/number monogram, image logo, favicon and allowlisted menu icons. Logo/favicon images are at most 1 MiB. Both colours use canonical `#RRGGBB`. Palette algorithm version 1 is shared between Go and TypeScript and verified by exact output vectors, including default and extreme colours. It checks text, links, buttons and focus against the relevant light/dark surfaces; adjusted primary shades are explained in the preview. Invalid contrast combinations are rejected. Neutrals remain code-owned. Go emits a nonce-protected initial stylesheet; the client applies the same palette and preserves reader theme choice.

Profile includes EN/AR/FR name (100 characters), headline (200), bio (320), sticker (40), image alt text (200), location (120), copyright (100), footer (200). Formatting uses the limited shared headline/bio grammar, never HTML. Portraits are square, at least 600 pixels, at most 2 MiB; banners at most 10 MiB with focal coordinates 0–100. Images require English alt text. Portrait and logo are independent.

There are at most three Now rows (label 80/text 160), ten links plus one main action (label 80/URL 2048), and twenty experience entries (role/organization 120/description 500). Row IDs are stable, bounded and unique. Months use YYYY-MM; date order is validated. Links allow HTTPS, local absolute paths and simple mailto addresses, rejecting controls, credentials and protocol-relative URLs. Public email is at most 254 characters. Home limits are 1–20. Empty/disabled sections stay hidden. The About picker lists documents published in the editing locale; public About resolution requires publication in the requested locale and never exposes a draft or falls back to another language.

## Labels

GET `/api/v1/labels/registry` returns code-owned section, description and EN/AR/FR defaults. Core keys and placeholder schemas cannot be renamed. Custom keys use two to four dot-separated lowercase segments, each starting with a letter and containing letters, digits or hyphens, at most 40 characters per segment and 160 total; at most 1000 override keys. Values are plain text, at most 1000 characters per form. Custom keys supply no new application behavior.

English is mandatory (effective shipped English is allowed for core keys). Each translation must preserve named `{placeholders}`. Arabic plurals require zero/one/two/few/many/other; English/French require one/other. Explicit empty AR/FR overrides use effective English. Reset removes an override to restore shipped values. Search matches keys/text; section and missing filters include counts. JSON export contains the working override map only. JSON import is bounded and server-validated into local Preview; publication still needs review and Save and apply.

GET `/api/v1/public/site?locale=en|ar|fr` returns only applied site/profile/labels, derived palette, version counters, effective localized profile metadata and available About URL. Hidden rows and inactive media are removed. GET `/api/v1/public/labels?locale=…` returns `{overrides, version}`. Neither response contains private email, sessions, file paths or operator secrets.

## Verification and generated contracts

Run `node scripts/project.mjs check`, `node scripts/project.mjs build`, `go test -race ./...` from backend, and `node scripts/smoke-auth.mjs` after installing Chromium. The smoke uses a real temporary Go server, HTTPS and owner sessions in EN/AR/FR. It covers Preview/Cancel/apply/conflicts, review/import, independent portrait/logo, private media, initial CSP styles and shared Markdown. Screenshots are written under frontend/test-results. Virtual authenticators do not prove hardware compatibility.

After editing label defaults, run `node scripts/build-label-registry.mjs`. After changing palette logic or design tokens, run `node scripts/build-palette-vectors.mjs`; review the algorithm version and both implementations before accepting changed vectors. Both generators support `--check` and run in CI. Never hand-edit generated registries/vectors. Migration 0006 adds immutable review/audit records and public configuration-media flags; follow the stopped-writer backup procedure before upgrading an existing database.

## Labels presentation

The Labels screen shows Version B translation columns (key, English, French, Arabic), stacked into cards below 900px. Completeness chips count effective missing values; explicit empty overrides still request English fallback. Unsaved overrides are marked against the saved snapshot. Thirty rows are shown per page; search, section and missing-only filters search the full registry. Per-cell options retain fallback/reset, and plural controls retain all six Arabic forms. JSON transfer and custom-label controls are expandable. The save bar stays visible; Preview/Cancel, optimistic versions, history and exact translation reviews retain the same API contracts.

## Using label fallback and JSON transfer

In a French or Arabic cell, **Use Effective English** sets that locale's override
to an explicit empty string. The interface then uses the current English override,
or the shipped English text if there is no English override. Later English edits
are followed automatically; no translated copy is created. For example, English
`Journal` plus French `""` displays `Journal` in French. **Reset locale override**
removes that setting and restores the shipped translation (or normal English
fallback if no translation exists). Unchecking the fallback option also resets
that locale; it does not recover a previously typed translation. Preview and Save
and apply are still required. This affects interface labels, never article bodies.

To transfer labels:

1. Open `/{locale}/admin/labels` and expand **Import and export labels**.
2. **Export working copy** downloads `interface-labels.json`, including unsaved
   overrides. It contains overrides only, rather than the complete default registry.
3. On the destination, choose or drop that JSON file in **Import JSON into preview**
   (maximum 256 KiB). The dashed upload area uses the same Media design as the owner
   editor. Invalid JSON, invalid keys/placeholders or incomplete plural forms fail
   validation. Nothing is published by selecting a file.
4. Inspect the preview and confirm any required French/Arabic translation review,
   then **Save and apply**. **Cancel** restores the saved copy.

Example file (a plain override map, without a `snapshot` wrapper):

```json
{
  "nav.articles": { "en": "Journal", "fr": "", "ar": "مقالات" }
}
```

Import replaces the complete working override map; it does not merge with existing
overrides. Keys omitted from the file revert to shipped defaults after applying.
Export your existing overrides first if you want to retain them, edit that file,
then import it. `{}` resets all overrides after explicit application. Use the
Labels JSON controls for this workflow, separately from the site archive/Notion
Markdown import in Transfer. Server validation, revision conflicts and translation
review rules apply equally to manual edits and JSON imports.

The Labels header provides a language popup, View site, theme and sign-out
controls; sign-out is last. The heading follows the Labels reference's display
font and 40px/46px sizing; its description uses the shared page-head type.
Branding/profile image inputs and the editor Media picker share the dashed upload
area, with their existing accepted file types, private-media handling and purpose
limits retained.

Settings, Profile and Labels now use the same owner workspace header and control
styling as Posts, organization, Media, Security and Transfer. Each retains
its own API revision/review rules. Save bars remain visible with reserved page
space. Language changes preserve the current configuration screen; dirty-form
navigation continues using the existing unsaved-change warning.

Settings and Profile now show their working controls beside a live preview on
screens wider than 1000px; phones/tablets stack the preview below. The desktop
preview is sticky, scrollable and keyboard-focusable. Settings previews working
logo/colours against the current public profile, with an independent Light/Dark
choice; Profile previews its working localized values with EN/FR/AR controls.
Previewing still writes nothing and does not apply draft colours globally. Image
fields now expose a Choose image button, retaining purpose-specific limits and
server checks.

Settings brand colours now use the reference picker/hex row plus five preset
swatches per role. Primary choices: Majorelle, Terracotta, Teal, Violet, Ink.
Secondary choices: Sun, Coral, Mint, Rose, Lime. Values come from tokens.json,
not component literals. Picker/hex/presets synchronize the working snapshot;
invalid hex stays visible for correction and is rejected by the existing server
validation before applying. Palette derivation, contrast checks and review/save
semantics are unchanged. Settings field/menu gaps, shared image upload spacing
and the history heading/action gap are explicit logical token spacing.

## Profile validation feedback

Profile prose and sections may be empty. AR/FR text requires its English source.
Selected portraits/enabled banners require English alt text; added links require
a label and safe URL; added experience rows require valid start/end months. Remove
unused rows rather than filling every optional section. Public email and About
selection may be cleared. Media dimensions/size and translation review still apply.

Authenticated profile Preview/Apply validation may return HTTP 422 with
`{error:{code:"invalid_fields",message,request_id,fields:[{path,code}]}}`.
Paths identify the field, language and zero-based row index (e.g.
`portrait_alt.en`, `links.0.url`); reason codes are stable UI lookup keys such as
`english_source`, `image_alt`, `url`, `month` and `date_order`. No submitted values are
echoed. The owner form explains errors and links to the fields, preserving edits.
Failed Preview/Apply leaves the applied snapshot and version unchanged.

## Actionable reasons across admin forms

Settings also supplies `error.fields` for wordmark/monogram, logo mode/image, colours,
contrast, menu icons and media references. Profile errors show concrete text/count/
range limits. Missing exact review uses `reviews` / `translation_review`, explaining
Preview and confirmation. The form distinguishes validation from session/confirmation,
version conflicts, rate limits, connection failures and server failures. No raw server
error text or submitted values are displayed.
