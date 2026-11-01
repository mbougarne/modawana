# Contributing

[العربية](CONTRIBUTING.ar.md)

Start with the [local setup](docs/getting-started.md), [architecture](docs/architecture.md),
and [contributor instructions](AGENTS.md). Keep changes focused and describe the
problem, resulting behavior, verification, and limitations in the pull request.
For substantial changes, discuss the scope in an issue first. Do not commit secrets,
production content, runtime data, generated builds, or private work notes.

## Checks

Use Go 1.27.3, Node 22.23.4, and npm 10.9.2:

```sh
node scripts/project.mjs install
node scripts/project.mjs format
node scripts/project.mjs check --race
node scripts/project.mjs build
(cd backend && go mod verify)
node scripts/smoke-production.mjs --http-only
```

`check` runs ESLint, Prettier, gofmt validation, documentation links/language coverage,
generated-file checks, TypeScript and frontend tests, Go tests, and go vet.
Use focused regression tests for authorization, persistence, concurrency and failure
behavior. Tests must own temporary data and must not target a live installation.
CI additionally checks dependencies, release packaging and restart behavior.
Browser suites are optional local diagnostics; CI does not install or run browsers.

```sh
npx --prefix frontend playwright install --with-deps chromium
node scripts/smoke-production.mjs
node scripts/smoke-auth.mjs
```

These require a production build and permission to bind loopback ports. Virtual
WebAuthn tests do not establish hardware compatibility. See [release checks](docs/release-readiness.md).

## Code and documentation

Keep imports before declarations and exports separate. Use braces for control flow.
Use shared UI components, token variables, logical CSS, and `t()` labels in EN/AR/FR.
Follow [design](docs/design.md) for token, label and palette generation.

Keep `/api/v1`, OpenAPI, Go validation, and TypeScript contracts aligned. Migrations
are append-only; never edit an applied migration or its checksum. Breaking API and
archive changes need explicit version/migration guidance and tests.

All new or materially changed project documentation must have an equivalent Arabic
and English version. Use linked pairs or complete bilingual sections. Describe current
behavior and procedures, not conversations or implementation diaries. Regenerate the
inventory with `node scripts/check-docs.mjs --write` and run the checker normally.

## Release packaging

After checks and builds:

```sh
(cd backend && go mod download all)
node scripts/release/notices.mjs
node scripts/release/package.mjs VERSION
node scripts/release/smoke-package.mjs
```

Use an unused version name. Packages contain native binaries for the build host,
frontend assets, operating guides, dependency notices and a SHA-256 inventory.
Test platform-specific binaries/images separately; a macOS binary is not a Linux
VPS binary. Packaging does not publish or deploy. See [licenses](docs/licenses.md)
and [security reporting](SECURITY.md).
