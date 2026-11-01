# Owner content portability

[العربية](portability.ar.md) · [API index](README.md)

Open **Import and export** in the owner navigation. Both operations require a
complete owner session, exact Origin/CSRF and authentication within five minutes.
Scoped publishing tokens cannot export private history/answer keys or import an
installation. Imports recheck the owner session inside the commit transaction.

- `POST /api/v1/portability/export`: downloads `application/zip`, no request body.
- `POST /api/v1/portability/import`: raw ZIP body; returns revision/media/collection
  counts and `review_required:true`. Only an empty content/configuration target is
  supported. The existing owner/security setup is retained. Nonempty targets return
  409; malformed bundles return 422 or the appropriate domain validation error.
- One transfer runs at a time (429 when occupied); HTTP deadlines are five minutes.
  Browser uploads are capped at 256 MiB. Interrupted downloads must be retried.

Local operator equivalents (absolute archive paths; output must not exist):

```sh
build/blog export --file /private/content.zip
build/blog import --file /private/content.zip
```

The archive contains schema-1 `manifest.json`, checksums and byte sizes, `data.json`,
selected Markdown files (the exporter selects **all retained revisions**) and
referenced upload files. Current revision pointers and source-translation links
are preserved through ID remapping. Quizzes include private answer keys, so treat
this as private owner data. Applied configuration/label overrides, taxonomy,
collection locales, draft plans and intended gates are included. Credentials,
sessions, learner identities/progress, tokens, operator settings, security logs and
configuration reviews are excluded. This is not a disaster-recovery backup.

Limits: 20,000 archive entries, 2 GiB total extracted bytes, 256 MiB per file,
8 MiB manifest/data JSON, 2,000 content revisions, 10,000 media entries, 1,000
collections and 3,000 localized taxonomy rows. Existing content/media validation
also applies. Unsupported schemas, duplicate entries, symlinks, unsafe paths,
missing references, invalid quizzes and checksum mismatches are rejected. Sizes
are checked while streaming; archives are extracted only into a private temporary
area. No archive SQL is executed.

Import rebuilds a separate staging installation through the normal validators.
All persisted IDs and file paths are generated again; source revision, quiz and
media references are remapped. Only after full validation are files installed and
metadata committed together into the still-empty destination. A failed commit may
leave unreferenced generated files; bounded maintenance collects old orphans.
No imported article/lesson/document is published, no collection plan is active,
and no translation or assessment review is copied. Imported collection visibility
starts off. Review quizzes and gates, publish intended revisions, then apply plans.
Configuration is retained as unapplied history: open Settings/Profile/Labels,
restore that revision into the editor, review translations and apply explicitly.
The About document must be published before applying a profile that references it.

## Notion Markdown exports

The **Import from Notion** section accepts Notion's unmodified **Markdown & CSV**
ZIP, including subpages. It is separate from the blog archive importer above and
can add drafts to an existing installation without changing its configuration.
Choose the content language (EN/AR/FR; no automatic translation), then **Articles**
or **Ordered learning path**. Preview the pages before selecting **Import these
drafts**. The result links to Content for review and publication.

- `POST /api/v1/portability/notion/preview?locale=en&kind=article`
- `POST /api/v1/portability/notion/import?locale=en&kind=learning`

Both use a raw ZIP body and the same owner, CSRF, recent-authentication, upload
size, concurrency and deadline boundaries as native portability. Only `locale`
and `kind` query parameters are accepted, once each. Preview returns `pages`,
`issues`, referenced `media`, `ignored` files, `skipped` existing pages and the
proposed `collection_title`. Import returns the usual counts, `review_required`
and `skipped`. A 409 means a conflicting page/route/collection appeared or an
existing learning path would have to be changed; nothing is overwritten.

Page titles come from the initial H1 (removed from the body to avoid a duplicate
reader title), otherwise the filename without Notion's ID. Stable identities use
the Notion page ID plus selected language, falling back to the archive-relative
path when no ID exists. Reimport skips existing identities and preserves their
manual edits. Internal Markdown links are rewritten to future site URLs and
referenced attachments to private managed media; code samples remain unchanged.
No remote images or other remote resources are fetched. CSV files and unrelated
assets are not turned into articles or database tables.

Learning mode creates one hidden collection and a draft plan. A single top-level
overview followed by nested pages stays an article; the children become lessons.
Other exports become one collection of lessons. Numbered siblings sort naturally
(`20`, `20A`, `20B`, `21`), independent of ZIP entry order. No quiz, answer keys,
completion requirements or gates are inferred from lesson prose. Publish selected
drafts, make the collection visible and explicitly apply its plan after review.
This is a one-time migration importer, not ongoing Notion synchronization; it
does not extend or replace an existing learning path's plan.

Limits: 20,000 ZIP entries, 128 MiB total declared uncompressed bytes, 50 MiB per
file, 1 MiB per Markdown page, 2,000 article pages or 500 pages in learning mode.
Existing media codec/size/pixel validators still apply. Absolute paths, traversal,
backslashes, duplicate entries/page identities, symlinks, malformed UTF-8 and
corrupt entries are rejected. Unsupported HTML (including Notion callout HTML),
remote images, invalid media and unresolved local links block import and are
listed in preview rather than silently discarded. Supported media are JPEG,
PNG, WebP, validated MP4/WebM, PDF and VTT under the normal upload rules.

All media and drafts validate in an isolated staging database before one atomic
metadata commit. Owner authorization and key/route collisions are rechecked in
that transaction. File installation precedes commit; a failed commit can leave
unreferenced files for normal maintenance, never a partially imported public page.
Existing owner/security, content, branding, labels, learner progress and active
plans are preserved. Imported drafts/media remain private until normal reviewed
publication permits access.
