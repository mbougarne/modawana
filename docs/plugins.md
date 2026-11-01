# WebAssembly plugins

[العربية](plugins.ar.md) · [Documentation](README.md) · [ADR 0001](decisions/0001-wasm-plugin-host.md)

The server can load owner-approved WebAssembly (WASI) plugins, per
[ADR 0001](decisions/0001-wasm-plugin-host.md). This page documents the stage 1
surface: the plugin directory layout, the manifest, the owner approval flow,
and the sandbox guarantees. Plugins written for stage 1 use Go compiled with
`GOOS=wasip1 GOARCH=wasm`; the TypeScript toolchain is a later stage.

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
  serves version 1 and refuses newer requirements.
- `capabilities` may declare `hooks`, `routes`, and `storage` in stage 1.
  Every other ADR capability (settings, jobs, http_fetch, secrets,
  content_read, mcp_tools) is refused until the stage that delivers it.
  A capability must be declared to be used and used to be declared.
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
   and only then registers the plugin's hooks and routes. **Disable** unloads
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
data (bodies at most 256 KiB of valid JSON). Plugin-declared migrations
arrive in stage 2.

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

## Stage 1 limitations

- Capabilities beyond `hooks`, `routes`, and `storage` are refused.
- Public plugin routes are `GET`/`HEAD` only; static assets are checksummed
  but not served.
- No plugin-declared migrations, no settings schema rendering, no jobs, no
  outbound HTTP, no secrets, no MCP tool exposure.
- No signature verification: installation is an explicit owner action with
  checksum pinning, and only the operator can place plugin files.
