# ADR 0001: WebAssembly plugin host API

[العربية](0001-wasm-plugin-host.ar.md) · [Decisions](README.md) · [Architecture](../architecture.md)

- Status: accepted
- Date: 2026-11-01
- Decider: owner (Mourad Bougarne)

## Context

Modawana is one Go binary with one SQLite database, immutable Markdown
revisions, and access enforced in Go on every route
([architecture](../architecture.md)). The next phase turns it into a small
extensible platform without breaking those properties.

Today there is no extension mechanism. Integrations are hard-wired:

- MCP is a JSON-RPC handler in `backend/internal/http/mcp.go` with a
  hard-coded tool list that re-dispatches to the publisher HTTP routes.
- Social-card selection is `backend/internal/http/social.go`, duplicated in
  `frontend/src/pages/public/social.ts`.
- Notion delivery and the lesson scheduler live in
  `backend/internal/publisher` and run out of process through the publisher
  CLI and a systemd timer; the only extension-shaped interface in the
  codebase is `publisher.NotionDelivery`.
- The server has no job abstraction, only one hourly cleanup goroutine, and
  no registry of hooks, routes, or settings sections.

Planned first-party features (comments, store, social media, AI providers)
should not each add more hard-wired code. They should be plugins on a public
API that third parties can also use.

## Decision

Build a WebAssembly plugin host inside the existing Go process, using
[wazero](https://wazero.io), a pure-Go, zero-dependency runtime (no cgo).
Plugins are WASI modules written in Go or TypeScript using toolchains this
project documents and tests; Rust is not a supported plugin language.
Plugins receive no ambient authority: every effect goes through versioned
host functions, gated by capabilities the plugin declares in a manifest and
the owner approves at install time.

Before any WebAssembly runs, the same contract is proven internally: the
hook, route, job, and settings registries are built first in plain Go, and
the existing first-party features (MCP tools, social cards, Notion delivery,
lesson scheduler) move onto them as built-in extensions. The Wasm host then
becomes a second, sandboxed way to implement the same contract.

## Host/plugin boundary

| Side | Owns |
| --- | --- |
| Host (Go) | HTTP, routing, authentication, authorization, SQLite, files, revisions, transactions, scheduling, secrets, validation, the manifest contract |
| Plugin (Wasm) | Pure logic: handle a hook payload, a routed request, a job tick, or a settings change, and call host functions for any effect |

- A plugin is a directory under the data root: `plugins/{slug}/` with
  `manifest.json`, one `.wasm` module, and optional static assets. Checksums
  of all files are recorded in SQLite at install.
- The host API is versioned (`host_api_version` in the manifest). The host
  refuses modules requiring a newer API and keeps old versions callable
  within a documented deprecation window.
- Plugins never see the database, the filesystem, the network, environment
  variables, or other plugins' data. WASI is instantiated with no
  preopened directories and no sockets.
- Hook payloads and host-function results are bounded, versioned JSON
  (or raw bytes for media), never Go pointers or SQL.
- Access control stays in Go. Hooks fire after the access decision for the
  request; a plugin cannot widen visibility, bypass a lesson gate, change a
  pass mark, or impersonate the owner. The capability table in the
  [configuration and learning appendix](../architecture-appendix-configurable-learning.md)
  continues to bind every caller, including plugins.

## Capability manifest

The manifest declares, and the owner approves, everything the plugin may do:

| Capability | Grants |
| --- | --- |
| `hooks` | Named hook points, each with a typed payload/result contract |
| `routes` | Handlers under `/plugins/{slug}/…` (public) and `/api/v1/plugins/{slug}/…` (owner/API), rate-limited and access-checked by the host |
| `storage` | Namespaced key–value and JSON document collections with checksummed plugin migrations |
| `settings` | A bounded settings schema the dashboard renders generically; values validated by the host |
| `jobs` | Scheduled jobs with bounded frequency, run by the host job runner |
| `http_fetch` | Outbound HTTPS to manifest-listed hosts only, with size/time limits |
| `secrets` | Named secrets entered by the owner, stored encrypted under the operator key, delivered to the plugin only at call time |
| `content_read` | Read access to published public projections (never gated bodies, drafts, answer keys, or credentials) |
| `mcp_tools` | Additional MCP tools, dispatched to the plugin's routes exactly as the built-in tools re-dispatch today |

Install and upgrade show the manifest diff; added capabilities require fresh
owner approval with recent reauthentication, like other sensitive settings.
Signature verification of third-party plugin archives is a later stage; until
then, installation is an explicit owner action with checksum pinning.

## Hook points (initial set)

| Hook | Fires | Contract |
| --- | --- | --- |
| `content.published` | After a publication transaction commits | Read-only event; used by delivery plugins (Notion, social media) |
| `content.rendered_meta` | While building page metadata | May contribute social-card/meta values; host sanitizes and bounds them |
| `page.public_fragment` | While rendering a server template region (after ADR [0002](0002-server-rendered-themes.md)) | Returns a sanitized HTML fragment for an explicitly themed slot (e.g. comments under an article) |
| `job.tick` | On the plugin's registered schedule | Bounded run time; the host records outcome and retries with backoff |
| `settings.changed` | After the owner applies the plugin's settings | Lets the plugin validate connectivity, warm caches |

Hooks are non-authoritative: a hook failure is logged and surfaced to the
owner, and never blocks core saving, publication, grading, or sign-in.
Delivery-style work triggered by hooks is queued through the job runner with
idempotency keys, matching the publisher's durable-delivery rules.

## Storage and migrations

- Plugin data lives in the same SQLite database in host-owned tables keyed by
  plugin ID (`plugin_kv`, `plugin_documents`, `plugin_migrations`,
  `plugin_installs`, `plugin_jobs`). Plugins get a storage API, never SQL.
- A plugin declares ordered, named migration steps (exported Wasm functions)
  with checksums. The host records each applied step in `plugin_migrations`
  and refuses checksum drift or downgrades — the same discipline as
  `backend/internal/db/migrate.go`, in a separate history so the core
  migration runner, its contiguous numbering, and its fresh-database check
  stay untouched.
- Collections declare bounded sizes and indexes in the manifest; the host
  enforces quotas per plugin so a plugin cannot grow SQLite unbounded.

## Settings and secrets

- The manifest's settings schema uses the same validation vocabulary as
  `backend/internal/siteconfig` (typed fields, bounded lengths, localized
  labels through the label registry). The dashboard renders it generically —
  the precedent is `LabelsForm`'s registry-driven rendering and the MCP tool
  `inputSchema` maps.
- Plugin settings are versioned snapshots with optimistic concurrency,
  history, and restore, like site/profile/labels configuration.
- Secrets (API keys for AI providers, store payment-provider keys) are never
  in snapshots, exports, logs, or the public payload. They are encrypted with
  an authenticated cipher under the operator-held key, like the TOTP seed.

## Backup and restore impact

- Plugin tables are in SQLite and plugin files are under the data root, so
  the existing coordinated backup covers both with no new mechanism.
- Portable export excludes plugin secrets and may include plugin data only
  when the plugin marks a collection exportable.
- Restore re-verifies plugin file checksums against the restored database.
  Following the restore contract, plugin secrets are invalidated with other
  credentials and must be re-entered; plugins whose files or checksums are
  missing come back disabled with a clear owner notice. The
  [recovery runbook](../operations/recovery.md) gains a plugin section.

## Migration path from the current code

1. Introduce in-process registries in new packages (`internal/extension`:
   hooks, routes, jobs, settings sections, MCP tools) with no behavior change.
2. Re-wire first-party features as built-in extensions of those registries:
   `mcpTools()` becomes registry-driven; social-card metadata moves behind
   `content.rendered_meta`; the server gets a job runner that replaces the
   ad-hoc cleanup goroutine; Notion delivery registers `content.published`
   plus a delivery job. The publisher CLI keeps working; the lesson scheduler
   stays in the publisher process until the `secrets` capability exists, then
   migrates.
3. Add the wazero host: manifest loading, capability gating, instance
   pooling, limits, and the storage API. Built-in extensions and Wasm plugins
   implement the same Go interface, so the host is a second implementation,
   not a rewrite.
4. Compile one first-party feature (social cards or Notion delivery) to Wasm
   as the proof, and ship the Go and TypeScript plugin toolchains with a
   template repository and conformance tests.

## Staged delivery plan

| Stage | Delivers | Proves |
| --- | --- | --- |
| 0 | `internal/extension` registries, job runner, built-in extensions for MCP tools and social cards | The contract fits the real features |
| 1 | wazero host, manifest + owner approval UI, KV/document storage, hooks, routes | Sandboxed third-party code paths |
| 2 | Settings schema rendering, jobs, `http_fetch`, `secrets`, plugin migrations | Full capability surface |
| 3 | Notion delivery and lesson scheduler as plugins; MCP tool exposure; TypeScript toolchain | First-party parity on the public API |
| 4 | Comments plugin (per the revised moderation policy), then store, social media, AI providers | The platform goal |

Each stage lands behind its own branch and PR with focused tests; a stage is
not a public compatibility promise until the host API is declared stable.

## Alternatives considered

- **Native Go plugins (`plugin` package or recompiling):** no sandbox, cgo
  and platform constraints, and third-party code would run with full process
  authority. Rejected.
- **Out-of-process plugins (subprocess RPC):** real isolation but a second
  runtime to deploy and supervise per plugin, against the one-binary,
  one-VPS operational model. Rejected for now; the host API is transport-
  agnostic JSON, so this remains possible later without changing plugins'
  logical contract.
- **Embedded scripting language (Lua, Starlark):** simpler host but a new
  language for plugin authors and a weaker capability story than WASI.
  Rejected: Go and TypeScript are the languages this project already uses.
- **Supporting Rust plugin toolchains:** technically easy (any Wasm runs)
  but each supported language is a documented, tested, maintained toolchain.
  Scope is deliberately Go + TypeScript.

## Consequences and risks

- Positive: integrations become uninstallable, auditable, owner-approved
  units; the core stays one binary and one database; first-party and
  third-party features share one API.
- Costs: Wasm invocation overhead (acceptable: hooks are per-event, not
  per-request-body byte); JSON (de)serialization at the boundary; a real
  compatibility surface to maintain once declared stable.
- Risks: capability creep (mitigated by the closed capability table and
  owner approval); plugin-induced SQLite growth (quotas); TypeScript
  toolchain maturity for WASI (mitigated by shipping and pinning the
  toolchain; Go alone is sufficient for stages 0–3).
