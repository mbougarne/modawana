# Frontend

[العربية](README.ar.md) · [Project](../README.md)

React, TypeScript and Webpack power public readers and the private dashboard.
Shared source is imported from root `ui/src` through `@ui`, with one React runtime
and one npm lockfile. English, Arabic RTL and French share the same components.

Run `node scripts/project.mjs dev` from the root for the Go/Webpack development
servers. See [local setup](../docs/getting-started.md). Production bundles have hashed
assets and a manifest and are served by Go on the same origin as the API.

```sh
npm --prefix frontend run check
node scripts/project.mjs build
```

The checks type-check the application/shared UI and run DOM/contract tests. Optional
browser diagnostics and their limits are described in [Contributing](../CONTRIBUTING.md).
Read [design](../docs/design.md), [UI contracts](../docs/ui-contracts.md),
[public pages](../docs/public-pages.md), and [owner tools](../docs/owner-tools.md).
Never embed runtime secrets in browser assets or treat hidden UI as authorization.
