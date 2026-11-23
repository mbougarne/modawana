# Languages, forms, and search metadata

[العربية](cms-site-tools.ar.md) · [Documentation](README.md)

## Public languages

In **Settings → Public languages**, enable the languages visitors can use and
choose a visible default for the site root and unlocalized feed. Existing
installations default to English, Arabic and French in their existing order.
Spanish is available to editors and becomes public when enabled. Interface
labels without a Spanish value use English. Content still requires an explicit
translation and the normal publication review.

To ship another two-letter locale, add it to the built-in packs under
backend/internal/locale/packs.json and to SQLite with an append-only migration.
Update OpenAPI, run the label registry generator, and verify content, visibility,
links, feeds and migration from an existing site.

## Forms and leads

Open **Forms and leads** in the owner dashboard. Define fields and a retention
period from one to 365 days. To require consent, supply the text to display.
Submissions must explicitly agree and retain the exact wording. Enable the form,
add its current version as a **Lead form** block, and publish the page. The saved
page preview shows the form with submission disabled.

Each form save creates a new version. Update the block reference and republish
affected pages before receiving submissions for the new version. Disabling a form,
unpublishing the page or hiding its locale stops new submissions immediately.
The server checks the exact published page revision and block ID and limits
request size and rate. For seven days, exact retries produce one lead and one
notification in total.

The owner can review, tag, edit the status of, export or delete lead records.
The durable notification marks a new submission in the private inbox.
Expired leads disappear from the inbox and CSV immediately and are removed by
maintenance cleanup. The selected retention period applies to new submissions.
Database backups include form definitions and leads. Content portability exports
retain the page block reference alone.

## Search and sharing

Use **Search and sharing** in a draft to edit its search title, description,
managed social image, indexing preference and structured data. The form provides
a snippet preview. Publishing the saved revision updates the public metadata.
Empty overrides use the metadata derived from the content. Pages can emit WebPage
structured data and articles can emit Article structured data. Standalone product
pages support Product markup with a SKU and an optional brand. Index exclusion
adds a robots directive and removes the canonical URL from the sitemap.

Use **Public redirects** in site settings to map retired localized paths to
published public paths. Enter the decoded local path spelling, including the
language prefix and omitting queries and fragments. Reserved routes, duplicate
sources, live path collisions and chained redirects are rejected. The rule
operates while the target is publicly accessible and uses its canonical URL.
