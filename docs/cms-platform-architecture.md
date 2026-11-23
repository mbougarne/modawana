# CMS platform and managed hosting architecture

[العربية](cms-platform-architecture.ar.md) · [Documentation](README.md) · [Current architecture](architecture.md)

**Status** · Proposed. The current-state review covers `main` at `d971624` (merged PR #17).

Modawana can grow into a small multilingual CMS for editorial sites, landing
pages, lead capture, and online stores. Keep the MIT-licensed application and
first-party modules usable on a self-hosted instance. Sell managed operation
of that same application, including domain setup, upgrades, backups, and
support. Build on the existing Go server, React dashboard, SQLite database,
and versioned content.

## What exists today

| Area | Shipped | Gap |
| --- | --- | --- |
| Publishing | Markdown content with immutable per-locale revisions, translation review, scoped publishing, media, search, and learning paths. Standalone pages have Standard and Landing templates. Header and footer menus can link to stable page IDs with a published, visible translation. Owners can hide public languages. | Visual builder, reusable compositions, configurable front page, route redirects, and an extension block registry. |
| Appearance | Classic, Folio, and Studio public themes and restricted, checksummed public theme packs. Classic and Slate dashboard appearance is a browser preference. | More public layout and styling controls, admin navigation and widget customization, site defaults, and portable appearance presets. |
| Extensibility | Wasm host APIs 1–3 with operator-staged files, owner approval, namespaced storage, settings, secrets, interval jobs, restricted HTTPS, public GET/HEAD routes, and read-only MCP tools. The implemented metadata hook supports native and Wasm social-card selection. | Durable events and jobs, typed contributions to the builder and dashboard, verified callback ingress, signed distribution, and an extension health view. |
| Discovery and integrations | Go-rendered canonical/alternate links, baseline metadata and social cards, robots, sitemap, RSS, and separately scoped Notion delivery and optional AI lesson generation. | Editable SEO, social publishing, traffic analytics, form submissions and lead handling, and general editorial AI. |
| Commerce | No catalog, checkout, order, fulfillment, or payments implementation in this checkout. | First-party Store module, PayPal provider adapter, and a merchant workflow from listing through refund. |
| Operation | One owner, one runtime per site, one local SQLite database, immutable files, a coordinated backup and restore flow, and self-hosting configuration. | Invite-only staff, repeatable site provisioning, domain lifecycle, fleet upgrades, managed billing, and site offboarding. |

[Owner tools](owner-tools.md) · [Public pages](public-pages.md) · [Plugin APIs](plugins.md) · [Dashboard appearance](ui-contracts.md#admin-appearance) · [Deployment](deployment.md) · [License](../LICENSE)

## Platform boundaries

| Layer | Responsibility |
| --- | --- |
| Core | Identities, capabilities, content and revision lifecycle, routes and access policy, rendering, menus, media, publishing, backups, and the infrastructure for extensions. Canonical URLs, basic metadata, and public feeds and sitemaps remain core. |
| Optional first-party modules | Forms and Leads, SEO Tools, Social Publishing, Analytics, AI Editorial, Store, and PayPal. Use reviewed Go registrants and host-rendered React screens where the feature needs private data, multi-record transactions, or a verified callback. Ship these modules in the same release and let the owner enable them independently where dependencies permit. |
| Third-party extensions | Retain Wasm APIs 1–3 and public theme-pack API 1. Expand them through versioned schemas and host-rendered contributions. Capabilities and approvals confer only their documented effects. |
| Managed service | Provision and operate isolated site instances, manage domains and subscriptions, track release and backup health, and support customer exit. Merchant business data and credentials stay in the site. |

The site data plane remains one Go process using one local SQLite database and a
private data root. React and Webpack continue to provide the dashboard and built
interactive assets. Core and first-party features use host-owned additive
migrations. Extension modules use the existing capability and migration
contracts. Privileged Go modules are part of a reviewed release. Packages users
install at runtime remain sandboxed Wasm or declarative theme packs.

## CMS, themes, and dashboard

### Page builder and navigation

- Extend standalone pages first. Store a format discriminator and a bounded,
  schema-versioned block document in an immutable revision artifact, covered by
  the existing checksum, backup, optimistic save, preview, review, and
  publication rules. Each block has a stable ID, namespaced type, schema version,
  and validated properties. Keep existing Markdown revisions and Standard and
  Landing templates readable. An editor explicitly creates a new composition
  revision when converting a page. Existing content IDs, public URLs, and menu
  targets remain valid.
- Start with Markdown or rich text, headings, images, buttons, columns, hero,
  call to action, and content lists. Use templates as starting snapshots and
  copy reusable sections by value. A shared section that can update multiple
  already published pages needs its own versioning and approval contract.
  Localize page text per revision and retain explicit language availability.
- Render block documents in Go with host templates and sanitized Markdown.
  Preview uses the same renderer behind an authorized, noindex preview route.
  Reference media, forms, and products by typed IDs and resolve each under its
  own access rules. Use only host-owned interactive components. Validate
  required dependencies on publish and show owners affected pages before
  disabling an extension. Preserve unknown block data for editing and export.
  A missing extension renders a safe localized fallback. Checkout and forms
  fail closed when their backing service is unavailable.
- Keep `/{locale}/pages/{slug}` as the default. Add a per-locale front-page
  selection, menu nesting and placements, and validated local redirects.
  Use one route resolver to reserve built-in, admin, API, and store routes and
  reject collisions or redirect loops. Canonical links, search, sitemap,
  language alternatives, and menus use that same resolution. Keep the current
  landing page and all existing links by default.
- Keep EN/AR/FR and their visibility controls as the initial supported
  configuration. Add more locales through a shared registry for locale codes,
  direction, interface label packs, and public visibility. Validate that a
  site's chosen default is visible. Keep the interface label fallback separate
  from published content, which requires an explicit translation.

### Public themes and admin customization

- Extend public packs with versioned layouts for home, standalone pages, posts,
  and storefront views, plus allowlisted colour, type, spacing and component
  tokens and checksum-pinned local assets. Keep host-owned document heads,
  access notices, forms, and checkout behavior. Preview under applied content
  and proposed appearance, then apply the versioned site setting. Preserve
  the existing built-in fallback when an active pack cannot be verified.
- Give owners control of the dashboard through an explicit registry of sections,
  screens, actions, and widgets. Allow renaming, grouping, reordering, and hiding
  optional items, choosing a landing screen, and saving widget layouts and
  admin appearance presets. Save site-wide defaults separately from each
  editor's preferences. Make account, security, and reset controls reachable.
  Use the shared EN/AR/FR labels and RTL layouts for the dashboard and
  extension-contributed screens.
- Add invite-only staff after introducing capability checks for resources and
  actions on the Go side. Start with Owner, Editor, and Store Manager presets
  and constrain sensitive actions such as granting permissions, enabling
  plugins, editing provider secrets, and refunding orders. Preserve the
  original owner, attribution, passkeys, backup factors, and recovery. Recheck
  grants on every API action and when a queued user action is executed. Layout
  preferences have no effect on permissions.

## Extension platform

- Extend the current registry with typed, namespaced contributions for admin
  navigation, schema-driven settings, widgets, builder blocks, public
  projections, and provider adapters. Give Wasm the smallest versioned
  contracts needed by real examples. Use host renderers and typed form
  actions for third-party UI, with explicit schemas and capabilities. Keep
  arbitrary browser scripts, raw HTML, and plugin-defined public writes
  outside the first extension API. Forms and PayPal use dedicated Go routes
  with separate authorization and validation rules.
- Add a SQLite transactional outbox for events such as `content.published`,
  `form.submitted`, and `order.paid`. Write each event in the same transaction
  as its domain mutation. Give it a stable ID, contract version, exact source
  version, and minimal data. Subscribers get durable delivery state with
  leases, bounded retries, backoff, idempotency and owner-visible failures.
  A successful enqueue accompanies a state change. External delivery runs
  after commit and may be repeated on recovery. Synchronous read-only render
  hooks remain bounded and optional. They perform no network work.
- Add a host-verified webhook inbox for specific provider routes. Check
  signatures, target connection, mode and identity before accepting an event.
  Deduplicate by connection and provider event ID. Record the verified event
  durably before acknowledgement and process it idempotently. Credential
  rotation, revocation, and mode changes have explicit handling for in-flight
  operations.
- Distribute signed and checksum-pinned release bundles through a curated
  catalog. Declare package and API compatibility, capabilities, dependencies,
  and migrations. Show capability diffs and validate a candidate before
  activation. Preserve checksum-approved local installs for self-hosting.
  Disable stops new work, while domain-specific obligations may require
  reconciliation. Export data before destructive removal. Reject incompatible
  rollback and use a tested forward fix or a matching pre-upgrade snapshot.
  Expose installed versions, permissions, pending work, and errors in the
  dashboard without disclosing secrets or payloads.

## First-party publishing and growth modules

| Module | First release | Subsequent scope |
| --- | --- | --- |
| Forms and Leads | Versioned form definitions and a builder block. Bind public submissions to a currently published block and its form version. Validate fields, enforce size/rate limits and spam controls, and persist submissions before queueing notifications. Offer an inbox, status, tags, a consent record where configured, CSV export, and retention/deletion controls. | CRM adapters, automations, multi-step forms, and an optional campaign module. |
| SEO Tools | Versioned per-locale title, description, social image, snippet preview, and indexability controls. Add validated redirects through the core resolver and typed structured data for content and products. Private and hidden content stays excluded. Search may show safe locked summaries. Feeds and sitemaps exclude gated lessons. | Redirect reporting, broken links, and optional search performance integrations. |
| Social Publishing | Connect one provider first. Draft or schedule a post for a specific destination and published revision, with an explicit preview. Use scoped OAuth, encrypted credentials, a durable delivery record, and an owner-visible retry status. Recheck publication and connection state before sending. | More destinations, per-channel copy, and link campaign parameters. Previously delivered posts require a separate provider action. |
| Analytics | Optional first-party aggregate counts for public traffic and conversions. Define event names and data retention. Keep source queries, identities, and form or checkout details out of analytics payloads. | Plausible as the first external connector, followed by consent-aware GA4 and advertising adapters with dedicated script and connection policies. |
| AI Editorial | Optional provider adapter with owner-configured credentials, quotas, and explicit context selection. Generate reviewable suggestions for alt text, summaries, SEO, social copy, and translations. Apply a suggestion through the normal draft and translation-review flow. | Additional providers and cost/usage reporting. Access to private inputs requires a specific permission and an explicit action. Leads, order data, credentials, and automatic publication are outside the initial contract. |

Further first-party modules can add moderated guest comments and campaign
management using the same submission and delivery primitives. Scheduled
editorial publication can reuse the durable job runner and publish an exact
reviewed revision through the core publication service.

Forms and checkout use separate public write contracts. Each binds to the
appropriate published resource or guest session, validates server-side, limits
abuse, and returns only a minimal acknowledgement. Lead and order records are
private and use capability-controlled screens and exports.

## Store and PayPal

### Product and operations scope

| Area | First merchant release | Follow-on modules and capabilities |
| --- | --- | --- |
| Catalog | Localized product pages, categories and media, variants and SKUs, a price and stock rules per variant, and draft/published state. | Bulk import and editing, bundles, merchandising recommendations, and additional price lists. |
| Checkout | Guest cart and checkout, secure order lookup, one configured currency per store, shipping zones and rates, configured tax rules with visible totals, and basic coupon codes. | Customer accounts, additional currencies, gift cards, external tax/carrier adapters, and abandoned-cart workflows. |
| Orders | Customer and address snapshots, order and payment history, stock adjustments, fulfillment and tracking, queued order and shipment notifications, receipts, cancellations, full and partial refunds, and a merchant export. | Digital delivery, returns portal, invoices via an appropriate integration, subscriptions, and POS/accounting connectors. |

Start with physical goods sold by one merchant per site. Keep Store and PayPal
independently named and configurable. Store owns catalog,
inventory, cart, order, refunds, and fulfillment. PayPal supplies an adapter
for creating and capturing a payment, status lookup, refund requests, and
verified provider events. Store connects product discovery to the core route
and metadata resolver. The merchant connects its own
provider account. Hosted plan billing uses a separate account and ledger.

### Transaction and recovery contract

1. Use currency-aware integer minor units. Recalculate totals in Go from
   authoritative prices, discounts, shipping, tax, and stock. Record
   immutable line and total snapshots, including tax and shipping
   calculations. Reserve stock and create the local order and payment
   operation transactionally. Reject reuse of an idempotency key with changed
   inputs.
2. Use PayPal Orders v2 for server-side creation and capture, using a
   provider-hosted approval step. Persist the stable provider request ID
   before making a call for an operation that supports it. Keep provider
   secrets on the server and leave card data with the provider. A payer
   return can resume the flow. Payment completion
   requires verified provider evidence that matches the connection, merchant,
   currency, amount, and order/capture identifiers.
3. Model order, payment, refund, and fulfillment states separately. Update the
   payment ledger, reservation or sold quantity, and outbox together. Handle
   duplicates, out-of-order callbacks, and a timeout after provider success
   through verification and reconciliation. Expire unpaid reservations when
   the attempt is definitively abandoned. Hold and flag uncertain captures
   for review. A late payment
   after a confirmed release enters an exception workflow for availability
   review or refund. Restocking is an explicit operation independent of
   a refund.
4. Pausing a store or disconnecting a provider closes new checkout. Keep
   access to order history and provide a reconciliation workflow for pending
   captures, disputes, and refunds. Retain financial operation and provider
   event IDs in the domain backup even when general API idempotency records
   are reset. Restore into maintenance with external deliveries and new
   payment actions paused. Reconnect the merchant and reconcile provider
   activity from the backup cutoff onward with the restored ledger. Correlate
   records using opaque local operation references when the provider supports
   them. Hold unmatched captures and refunds for merchant review, including
   operations absent from the restored database. Pause automatic replay if the
   local record or provider idempotency window is uncertain.
5. Verify the end-to-end purchase, cancellation, refund, duplicate callback,
   last-unit concurrency, and interrupted-restore flows in sandbox. Confirm
   live capabilities for each merchant's account, country, and currency
   before enabling production checkout. Validate merchant-specific tax,
   shipping and invoice settings for the intended markets during onboarding.

[PayPal Orders v2](https://developer.paypal.com/api/orders/v2/) · [PayPal request idempotency](https://developer.paypal.com/api/rest/requests/#paypal-request-id) · [PayPal webhook verification](https://developer.paypal.com/api/rest/webhooks/rest/) · [PayPal refunds](https://developer.paypal.com/api/payments/v2/captures-refund)

## Open-source distribution and managed hosting

- Publish the existing MIT application and documented first-party modules in
  reproducible, versioned builds. Provide the plugin/theme contract, example
  packages, upgrade matrix, and self-hosted operational guides. Complete
  the repository history and GitHub surface review before changing visibility.
- Run one application container, private local volume, SQLite database, and
  encryption key per hosted site. A verified domain map at the TLS gateway
  routes only known hosts to their site. Use per-site identity and resource
  quotas, network isolation, constrained outbound access, and no shared
  writable mounts. Scale by moving or placing sites on additional hosts
  with one active writer per site.
- For the pilot, pick the canonical HTTPS hostname and passkey RP ID before
  owner enrollment. Redirect aliases to that host. Verify customer domain
  control before serving traffic or issuing certificates. A custom-domain
  move needs explicit URL and passkey re-enrollment, session, webhook, and
  redirect steps. A stable admin origin with separate public domains is a
  later change to the current same-origin contract.
- Run a separate control plane for site identity and lifecycle, placement,
  domain state, image pins, quotas, health, backup references, and hosting
  subscriptions. A billing suspension can be reversed without deleting site
  data. Keep page bodies, customer records, and each merchant's
  payment credentials in the site runtime. Grant support access only
  through a scoped, auditable procedure. Deliver owner bootstrap or
  recovery grants through a dedicated one-time channel and keep them out
  of logs and tickets.
- Make provisioning and upgrades safe to retry using stable site and operation
  IDs. Use pinned images, a canary cohort, an old-image snapshot before migration,
  a stop-before-start upgrade, readiness checks and explicit rollback
  procedures. Keep encrypted off-host backups per site with separate key
  custody and alert on stale or missing backups. Test isolated restores
  without provider credentials or access to other sites and
  measure backup time, disk staging needs, data-loss window, and recovery
  time. Set pilot quotas beneath the capacity of the current backup format
  (2 GiB total, 256 MiB per file, 20,000 files) or ship and exercise a new
  verified format before raising stored-data limits.
- Offer offboarding with a documented content export and a separate
  coordinated operational backup. Define portable orders, products, forms,
  and lead exports with explicit access and revision semantics. Re-enter
  provider credentials and re-enroll owner/staff factors at the destination.
  Validate export, restore, and egress isolation with multiple sites before
  accepting paying customers.

[Deployment](deployment.md) · [Restore contract](operations/recovery.md) · [Release readiness](release-readiness.md) · [WebAuthn](https://www.w3.org/TR/webauthn-3/)

## Delivery sequence

Each phase can ship behind its own activation boundary. Phase 1 is the next
implementation. Managed CMS provisioning can be exercised in parallel once the
core extensions and builder are stable. Store hosting depends on the merchant
release gate.

| Phase | Deliverable | Acceptance |
| --- | --- | --- |
| 1. Page builder foundation | Revision-bound blocks, shared Go preview and rendering, one landing-page preset, editor route. | Edit and publish a multilingual landing page. Every legacy page and link still works. Preview matches the published renderer. Restore a revision and verify export and recovery. |
| 2. Extension lifecycle | Typed contribution registry, durable outbox, inbox contract, first reference contribution, and owner diagnostics. | Existing Wasm APIs still load. Retrying after process exit does not repeat the domain effect. Capability changes require review. |
| 3. Appearance and dashboard | Public theme slots and presets, front-page/menu controls, configurable and resettable dashboard. Add invite-only roles and support for additional locales as separate slices. | Themes and layouts render in EN/AR/FR. Changing navigation has no effect on route/API permissions. Built-in fallback and base owner recovery work. |
| 4. Forms and SEO | Forms and Leads, SEO Tools, redirects, and search/sitemap integration. | A published landing page captures a lead once, queues delivery, and exposes it only to authorized staff. Search and discovery preserve the existing visibility and learning-gate rules. |
| 5. Growth integrations | Analytics, one social provider, and AI Editorial. | A scheduled social job validates the exact published revision before sending. Analytics excludes protected details and respects configured consent. AI suggestions enter the existing draft workflow. |
| 6. Store and PayPal | Catalog, checkout, merchant order operations, provider integration, and recovery. | Complete a real sandbox merchant flow with stock races, duplicate events, partial refund, and restore reconciliation. Confirm live merchant readiness separately. |
| 7. Managed service | Provisioning pilot, curated catalog, rollout and restore automation, host billing, and export/offboarding. | Provision, upgrade, restore, and offboard independent sites. Demonstrate no cross-site data, credential, session, or plugin leakage. Validate a real restore and support workflow for the pilot. |

Provider order after PayPal, the first social destination, and market-specific
store options can be selected when their corresponding phase starts.

### First implementation

1. Add the revision format and block validation in Go. Keep old pages readable
   and cover save, publish, restore, content export/import, and recovery
   archives with fixtures.
2. Render a small block set with the same server code in preview and public
   pages. Preserve routes, canonical metadata, and media access for existing
   pages.
3. Add the React editor with keyboard-accessible block ordering, a Landing
   preset, and explicit conversion of an existing Markdown page into a new
   draft. Verify save conflicts, translation review, EN/AR/FR rendering,
   revision restore, and menu links before enabling it by default.
