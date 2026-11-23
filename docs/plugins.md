# WebAssembly plugins

[العربية](plugins.ar.md) · [Documentation](README.md) · [ADR 0001](decisions/0001-wasm-plugin-host.md)

The server can load owner-approved WebAssembly (WASI) plugins, per
[ADR 0001](decisions/0001-wasm-plugin-host.md). This page documents the
manifest, owner approval flow, and host APIs 1–3. Plugins can use Go compiled
with `GOOS=wasip1 GOARCH=wasm` or the AssemblyScript example below.

## Directory layout

A plugin is one directory under the data root, outside the checkout and
release directories:

```
$BLOG_DATA_ROOT/plugins/{slug}/
├── manifest.json   required, at most 64 KiB
├── plugin.wasm     the WASI module named by "module", at most 32 MiB
└── …               optional static assets (checksummed, not yet served)
```

The directory name is the plugin's identity and must match the manifest
`slug`. The operator places these files (for example over SSH); the server
never writes them and never deletes them. Symlinks and irregular files are
refused, as are more than 256 files or 64 MiB in total.

## Manifest

`manifest.json` is strict, versioned JSON: unknown fields are refused.

```json
{
  "schema_version": 1,
  "host_api_version": 1,
  "slug": "example",
  "name": "Example plugin",
  "version": "0.1.0",
  "description": "Optional, at most 500 characters.",
  "module": "plugin.wasm",
  "capabilities": ["hooks", "routes", "storage"],
  "hooks": [{ "name": "content.rendered_meta", "version": 1 }],
  "routes": [{ "method": "GET", "path": "/hello", "public": true }]
}
```

- `host_api_version` names the host API the plugin requires. This binary
  serves versions 1–3 and refuses newer requirements.
- All three APIs accept `hooks`, `routes`, and `storage`. APIs 2 and 3 add the
  capabilities documented below. A plugin declares each capability it uses.
- `hooks` subscribes to named hook points with a contract version
  (at most 16).
- `routes` declares handlers the host mounts under `/plugins/{slug}{path}`
  when `public`, and `/api/v1/plugins/{slug}{path}` (owner session required)
  otherwise (at most 16). Public routes are read-only: `GET` or `HEAD`.
  The first path segment may not be `install`, `enable`, `disable`, or
  `remove`.

## Owner approval flow

Nothing executes before approval. From the dashboard **Plugins** page
(`/{locale}/admin/plugins`), backed by owner-only API routes under
`/api/v1/plugins`:

1. A directory with a valid manifest appears as **Ready to install** with its
   manifest contents. **Approve and install** records the manifest and the
   SHA-256 checksum of every file in SQLite. The plugin stays disabled.
2. **Enable** verifies the files still match the approved checksums, refuses
   any host-function import whose capability the manifest does not declare,
   and only then registers its hooks, routes, jobs, and tools. **Disable** unloads
   it immediately; a disabled plugin never runs.
3. Changing any file on disk moves the plugin to **Files changed since
   approval**: it will not load again until the owner re-approves, and the
   page shows the pending manifest with any added capabilities highlighted.
4. **Remove** deletes the approval record and, through the schema, the
   plugin's stored data. Files under `plugins/` stay on disk for the operator.

Install, enable, disable, and remove require recent reauthentication, checked
again inside the mutation's transaction, exactly like other sensitive owner
operations. All records use optimistic versions (`expected_version`).

## Sandbox and limits

Modules are instantiated per invocation with wazero (pure Go, no cgo):

- No preopened directories, no sockets, no environment variables; the only
  effects go through the versioned `modawana_v1` host functions.
- Per invocation: 5 seconds, 64 MiB of linear memory, 64 KiB hook results,
  256 KiB route bodies, and a small per-plugin concurrency gate.
- Hook payloads and results are the same bounded JSON bytes the built-in
  registries exchange; hook failures are logged and dropped, never fatal to
  the request.
- Route responses are host-validated: status 200–599 and `application/json`
  or `text/plain` only.

## Storage

The `storage` capability provides a namespaced key–value store and JSON
document collections in host-owned SQLite tables (`plugin_kv`,
`plugin_documents`), keyed by the install record so plugins can never read
each other's data. Stage 1 quotas per plugin: 1024 keys and 1 MiB of
key–value data (values at most 64 KiB); 4096 documents and 4 MiB of document
data (bodies at most 256 KiB of valid JSON). API 2 plugins can migrate this data using the same quotas.

## Invocation contract

The module runs as a WASI command: one JSON envelope on stdin, the result on
stdout, exit code 0. A hook envelope is
`{"api":1,"kind":"hook","hook":"…","hook_version":1,"payload":…}` and stdout
is the raw hook result. A route envelope is
`{"api":1,"kind":"route","method":"GET","path":"/hello","query":{…},"owner":false,"body":"…"}`
and stdout is `{"status":200,"content_type":"application/json","body":"…"}`.

The first-party example plugin in
[`backend/internal/plugin/testdata/example`](../backend/internal/plugin/testdata/example/main.go)
is the reference implementation; the integration tests compile and run it.
Build it with:

```sh
node scripts/build-example-plugin.mjs
```

## Stage 2 capabilities

Set `host_api_version` to 2 to use these declarations. The import module name
remains `modawana_v1` so modules can reuse the original storage functions.
The owner approves the checksum of the displayed candidate file set through
the Plugins page. The page shows declared HTTPS hosts and scheduled jobs before
approval. Existing API 1 modules still load.

| Capability | Manifest field | Meaning |
| --- | --- | --- |
| `settings` | `settings` | Up to 24 fields with `name`, localized `label`, `type` and `default` |
| `jobs` | `jobs` | Up to 8 jobs with `name` and `every_seconds`, from 60 to 86400 |
| `http_fetch` | `https_hosts` | Up to 8 exact lowercase ASCII DNS hostnames |
| `secrets` | `secrets` | Up to 8 named secrets with localized `label` |
| `storage` | Optional `migrations` | Up to 32 ordered steps with `name` and lowercase SHA-256 `checksum` |

Names are lowercase ASCII and start with a letter. They can contain digits,
underscores and hyphens, up to 48 characters. Labels require an English value.
French and Arabic labels fall back to English. Settings types are `string`
(up to 1024 bytes), `boolean`, and `integer` (signed 32-bit). Every field needs
a default of that type.

The dashboard generates the settings form from the approved manifest. It
retains up to 50 versions and restores a selected version as a new save.
Restoring applies the current schema and defaults. Settings are available as
the `settings` object in hook, route, and job inputs. Keep credentials in
named secrets. The UI only reports whether a declared secret is present.

### Jobs and migrations

The host sends jobs `{"api":2,"kind":"job","name":"sync","settings":{…}}`.
Jobs are scheduled on enable, canceled on disable or remove, and paused
during restore maintenance. The first run follows one full interval from
enable or restart. The host bounds each run to five seconds and records
outcomes and backoff in the server logs.

The host invokes a new migration with
`{"api":2,"kind":"migration","name":"initialize"}`. The command uses the
existing storage functions against a single transaction and exits zero on
success. A migration batch has a 15-second limit. A failed migration rolls back storage and enable or upgrade changes.
Use a stable checksum for each step. The host matches the applied prefix
against the declared names and checksums and rejects reordered, changed, or
removed steps. The approved plugin file set separately pins the module bytes.

### HTTPS and secrets

`secret_get(name_ptr, name_len, buffer_ptr, buffer_capacity)` reads a declared
secret into the caller's guest memory on demand. The operator key encrypts
the value at rest with a key derived specifically for plugins. Updating or
removing secrets requires recent owner authentication.

`http_fetch(request_ptr, request_len, buffer_ptr, buffer_capacity)` accepts
a JSON object with `url`, `method`, optional `headers`, and optional base64
`body`. The result is JSON with `status` and base64 `body`. Supported methods
are GET, HEAD, POST, PUT, PATCH and DELETE. Accepted headers are Accept,
Content-Type, Authorization, X-API-Key, and Idempotency-Key. The host dials
verified public addresses using TLS and the approved hostname. Redirects are returned without following them. The host refuses
proxies, private addresses, and non-default ports. Up to four
requests run per invocation, with a three-second timeout and a 64 KiB response
body limit. Retry a short response buffer with the same request bytes to read
the buffered response without issuing the request again.

Both functions return a length or the existing negative host status codes.
They are available to scheduled jobs and to owner write routes that passed
the Origin and CSRF checks. Public routes, read-only routes, hooks and
migrations receive neither capability during their invocation. MCP tools are
read-only and also receive neither capability.

### Owner API and recovery

- GET `/api/v1/plugin-configuration/{slug}` returns `version`, effective
  `values`, settings `history` and a map of secret names to presence flags.
- PUT the same route with `expected_version` and complete `values`, or
  `expected_version` and `restore_version`.
- PUT `/api/v1/plugin-configuration/{slug}/secrets/{name}` with
  `expected_version` and `value`. Use a string to set it or null to remove it.

Coordinated backups include approved files that match their recorded
checksums, plus plugin data and settings history. They omit plugin secret
values. Restore verifies the files and disables plugins with missing files.
Re-enter their secrets before resuming integrations that need credentials.

Plugin-managed content reads, public static assets, and archive signatures
remain outside these API versions.

## Stage 3 MCP tools

Set `host_api_version` to 3 and declare the `mcp_tools` capability. The
`mcp_tools` array describes up to eight tools. See the
[example manifest](../examples/plugins/typescript/manifest.json).

- Each tool has a local `name`, a `description`, a `properties` object, and
  optional `required` property names. The properties can be string, integer
  (signed 32-bit), number, or boolean. A tool can declare up to 16 properties.
- The MCP name is `{slug}__{name}`. Only authenticated credentials with
  the `plugin_tools` publisher permission can discover or call these tools.
  Issue a token with just this permission when it needs only plugin tools.
  The grant covers all enabled plugin tools. Standard publisher tools keep
  enforcing their own scopes and content restrictions.
- The command receives an envelope such as
  `{"api":3,"kind":"mcp","tool":"echo","arguments":{"message":"Hello"}}`.
  Arguments must match the declared properties. The host rejects unknown
  fields, nulls, and payloads above 16 KiB. String arguments can have up to
  4096 characters. The guest receives no HTTP request or credential.
- Return UTF-8 text on stdout up to 64 KiB. The host wraps it as MCP text
  content. Traps, timeouts, and attempted writes or network access return a
  tool error. A tool may read its own plugin's storage. It cannot change
  storage, read secrets, or fetch HTTPS resources.

### AssemblyScript example

The sample uses AssemblyScript, a subset of TypeScript compiled to Wasm.
It uses a small WASI stdin/stdout adapter. It does not require a JavaScript
runtime in the server.

```sh
npm ci --prefix examples/plugins/typescript
npm run build --prefix examples/plugins/typescript
```

Copy `build/plugins/typescript-example/` to the server's plugin directory,
approve and enable it, and call `typescript-example__echo` with a token
that has `plugin_tools`. CI builds and runs this module through the host.

### First-party extensions

The [social-card metadata selector](../backend/cmd/plugin-social-cards/main.go)
is also available as a Wasm module. It implements the same hook with the
same selector as the built-in extension.

```sh
mkdir -p build/plugins/social-cards-wasm
(cd backend && GOOS=wasip1 GOARCH=wasm go build -o ../build/plugins/social-cards-wasm/plugin.wasm ./cmd/plugin-social-cards)
cp backend/cmd/plugin-social-cards/manifest.json build/plugins/social-cards-wasm/
```

Approve and enable that directory to run the Wasm selector on the public
metadata hook. The host still controls public media access and result sanitization.

[Notion delivery and lesson scheduling](integrations/remote-publishing.md#in-process-extensions)
are opt-in native extensions. They reuse the publisher's scoped API,
durable state, and longer external request budgets.

### Host-rendered contributions (API 4)

Declare the `contributions` capability and up to 16 entries in
`contributions`. Each entry has `kind`, a lowercase `key`, `version` (currently
1), a localized `label` with an English value, and a host-defined `target`.
French and Arabic labels are optional. The host forms the contribution ID
from the approved plugin slug and key.

| Kind | Target and behavior |
| --- | --- |
| `builder_block` | `text_panel` adds a block to the standalone page editor. The block accepts an optional heading up to 200 characters and required text up to 500 characters. Go escapes both as plain text in preview and on published pages. |
| `admin_navigation` | `overview` or `settings` links to the matching section of the owner’s Plugins screen. |
| `settings` | `configuration` labels the host’s existing settings form. It requires declared plugin settings, as does the `settings` navigation target. |
| `widget` | `extensions` shows localized plain `text` on the owner’s Plugins screen. |
| `public_projection` | `footer` adds localized plain `text` to the host-rendered public footer. |
| `provider_adapter` | Registers a descriptor for `payments`, `analytics`, `social`, `notifications`, or `editorial_ai`. Provider execution is defined by future integrations. |

As with other capabilities, the owner approves the exact manifest and module
checksums. A changed file set requires approval before it can replace the running
version. APIs 1–3 remain supported. The Plugins screen shows the installed
version, approvals, and current scheduler state, including the next scheduled run
and consecutive failure count. Scheduler status reflects the current server process.

When reviewing a disable, **Affected pages** scans current draft and published
page revisions in batches. Disabling removes the contributions from the host
registry. Saved blocks stay in their revisions, display an unavailable message,
and require an active matching contract to publish again. Re-enabling a matching
contract restores rendering.

For a reference plugin, run:

```sh
npm ci --prefix examples/plugins/typescript
npm run build:contributions --prefix examples/plugins/typescript
```

Copy `build/plugins/editorial-notes/` into the server's plugin directory, approve it,
and enable it. Add an **Editorial note** block on any standalone page, enter text,
then save and preview it. The example also supplies an owner navigation link, a
settings section, and a widget.

### Durable publication events (API 5)

To receive new publications, an approved and enabled plugin declares
`"capabilities": ["events"]` and
`"events": [{"name":"content.published","version":1}]` with host API 5.
APIs 1–4 can continue to use their existing hook and job contracts. The event
and a job for each enabled subscriber commit in the same SQLite transaction
as the publication. This includes editor, bulk, and publisher API publication.
Enabling a subscription starts delivery from subsequent publications.

The Wasm stdin envelope contains `api`, `kind: "event"`, `operation_id`,
`event`, and optional configured `settings`. The event holds `id`, `name`,
`version`, `source_id` (content locale version ID), `source_version` (its
committed optimistic version), and `data` with `content_id`, `locale`, and
the exact published `revision_id`. The envelope contains IDs and metadata.

Return `{}` to acknowledge the event. With the approved `storage` capability,
the plugin can return `{"set":{"key":"receipt","value":"text"}}`. The host
commits that update to the plugin's own key-value namespace together with the
successful delivery. The event invocation runs after commit with read-only
storage and no network or secret access. For a future provider adapter, use
the same `operation_id` with provider-supported idempotency, or reconcile that
operation before sending it again. Provider delivery is at least once.

In **Plugins → Event deliveries**, an owner can inspect statuses, attempt counts,
the next scheduled attempt, and sanitized failures. An attempt holds a
one-minute lease. A stopped worker's outstanding job can be reclaimed after
the lease expires, with the same operation ID. Five failed or interrupted
attempts exhaust the budget. Retry renews that budget on the same job.
A later publication can deliver before a job waiting in backoff.
Disabling a subscription pauses its outstanding work. Reinstalling a removed
plugin creates a new identity. Delivery history is retained in SQLite and
included in coordinated backups. Ordinary interval jobs keep their existing
scheduler.
