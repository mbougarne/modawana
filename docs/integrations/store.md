# Store catalog and inventory

[العربية](store.ar.md) · [Documentation](../README.md)

Store is an optional first-party module for one merchant selling physical
goods per installation. The catalog is disabled by default. The owner can
enable it in **Store** and select a three-letter currency before adding
the first SKU. Prices are integer minor units. For example, enter `12345`
for MAD 123.45, or `12345` for JPY 12345.

## Catalog and storefront

Use the catalog language selector to edit localizations. A stable product key
connects translations. Each localization has its own slug, title, description,
optional category, and up to eight uploaded images with localized alt text.
Use the same stable category key to connect category labels across languages.

Create the first active variant before publishing. Each SKU is unique across
the whole store, is normalized to uppercase, and has its own price and stock.
Variant labels may be translated. The SKU is shown when a localized label is
missing. Price, active status, and stock are shared across localizations and
take effect when saved. The last active variant of a published product cannot
be deactivated.

Save a localized draft, then publish it explicitly. Subsequent edits to the
localized text, URL, category assignment, or gallery remain in the draft
until publication. Go serves the catalog at `/{locale}/store` and products at
`/{locale}/store/{slug}`. The store, search, canonical metadata, and product image
access use the same publication and locale visibility rules. Store browsing
supports search and category filters. Public pages show availability while
exact stock quantities and adjustment history stay in the workspace.

## Inventory and access

Variants start with zero stock. Use **Record adjustment** to enter a signed
quantity and a reason. The same transaction changes stock and records the actor,
previous quantity, new quantity, and timestamp. Stock stays between 0 and
1,000,000,000. An exact retry returns the original receipt. Changing the inputs
for the same operation ID returns a conflict. The workspace shows the latest
50 adjustments for a SKU.

The installation Owner controls storefront activation and currency. The Owner
and delegated Store Managers can edit localizations and categories, manage store
images and variants, publish, and adjust inventory. Store Managers enter through
their usual staff workspace. Store permissions are rechecked in write
transactions. Store access does not grant access to the blog editor or its
private uploads.

This release serves a storefront and administers inventory. Checkout and payments
are separate roadmap work. See the [OpenAPI schema](../api/v1.openapi.json) for
catalog and inventory endpoints.
