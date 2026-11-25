# Self-hosted releases and extension catalog

[العربية](self-hosted-release.ar.md) · [Documentation](README.md)

## Native packages

Build an MIT release from a clean checkout of the versioned source. Use the Go
and Node versions pinned in the repository and the npm version in [deployment](deployment.md).
Use the same OS, architecture and build environment for independent comparisons.

```sh
node scripts/project.mjs install
node scripts/project.mjs check
node scripts/project.mjs build
go -C backend mod download all
node scripts/release/notices.mjs
node scripts/release/package.mjs vX.Y.Z
node scripts/release/check-reproducible.mjs vX.Y.Z
```

Publish the archive and its `.sha256` together. Compare SHA-256 with the value
obtained from the trusted release channel before extracting. On Linux, run
`sha256sum -c vX.Y.Z.tar.gz.sha256` next to the archive. On macOS use
`shasum -a 256 -c vX.Y.Z.tar.gz.sha256`. `release.json` identifies the source
commit, dirty checkout status, target, toolchains and every shipped file hash.
Stable archive order, permissions, timestamps and ownership make repacking
byte-for-byte reproducible with the same build inputs. For an independent build,
repeat the commands from a second clean checkout of the same commit and compare
the archive SHA-256. Ship clean-source builds with the corresponding source tag.

Extract into a versioned directory and set the private environment described in
[deployment](deployment.md). Point `BLOG_FRONTEND_DIST` at the extracted
`frontend` directory and `BLOG_DATA_ROOT` at a separate private persistent
directory. Start the extracted `blog` binary. On a clean installation, run
`blog bootstrap` with the same environment and complete owner enrollment.
For an upgrade, take and verify the backup using the old binary, stop it, and
start the candidate from its own directory with the same data root. Confirm
`blog ready`, owner sign-in, content, media and enabled extensions. For
incompatible migrations use the old binary and its pre-upgrade backup, restored
into a fresh directory following [recovery](operations/recovery.md).
See [release readiness](release-readiness.md) for the public release review.

| Component | Compatibility |
| --- | --- |
| Native archive | Match target OS and CPU in `release.json`. Linux x86-64 is exercised in CI. |
| Runtime | Go binaries, Webpack assets, local SQLite and immutable files. Node is only needed for building or catalog publishing. |
| First-party modules | Social publishing, analytics, editorial AI, store and PayPal are built into the host. Optional providers need installation-specific credentials. |
| WASI extensions | Plugin manifest schema 1, host API versions 1 through 5. The package ships a compiled sample and a TypeScript source example. |
| Themes | Public pack API 1 (host partials and scoped CSS) and API 2 (host layouts and tokens). The package ships a sample API 2 pack. |
| Catalog | Index schema 1, detached Ed25519 signature, SHA-256 package digests. Local checksum approval is also supported. |

## Publish the curated catalog

The release includes `examples/catalog-spec.json` for the first-party sample
plugin and theme. Include only reviewed extensions and use a new version for
changed bytes. A plugin bundle is a ZIP of regular files, headed by
`manifest.json` and its WASM module at the ZIP root. A theme bundle is its
original `theme.json`. From the extracted release directory, prepare the ZIP
and sign the index with an offline Ed25519 key. Choose a future UTC expiry.

```sh
(cd examples/plugins/example && zip -X ../../plugin-example.zip manifest.json plugin.wasm)
node scripts/release/catalog.mjs index examples/catalog-spec.json catalog.json "$CATALOG_EXPIRY"
node scripts/release/catalog.mjs sign catalog.json /private/catalog-signing.pem release-1 catalog.sig.json
node scripts/release/catalog.mjs keyring /private/catalog-public.pem release-1 trusted-keys.json
```

Publish the catalog, its detached signature and all referenced bundles.
Distribute the public key independently through a trusted channel. Compare its
fingerprint with the curator's authenticated value before adding it to your
local keyring. Keep the signing key offline and outside Git. A keyring accepts
multiple key IDs. To rotate a key, authenticate the new public key, add it to
`keys`, and publish a new signature with its ID. To revoke a key, add its ID to
`revoked` and remove it from `keys`. A revoked ID is rejected even if its key
remains present. Replace cached catalogs and review previously installed versions
from that key. Revocation affects subsequent verification. Use the dashboard to
disable affected plugins or switch away from an affected theme.

## Preview, approve and recover

On the self-host, save the catalog, signature, trusted keyring and exact bundle
locally. Back up the installation before upgrading an extension. Stop the
service and use its normal runtime user, environment and `BLOG_DATA_ROOT`.

```sh
./blog extension preview --catalog catalog.json --signature catalog.sig.json \
  --keyring trusted-keys.json --bundle examples/plugin-example.zip \
  --kind plugin --id example --version 0.1.0
./blog extension install --catalog catalog.json --signature catalog.sig.json \
  --keyring trusted-keys.json --bundle examples/plugin-example.zip \
  --kind plugin --id example --version 0.1.0 --approve "$REVIEW_SHA256"
```

Read the previous and proposed manifests, especially capabilities, routes,
HTTPS hosts and migrations. `applied_migrations` counts the already-applied
prefix of the candidate's migration list. Set `REVIEW_SHA256` to the printed
`review_sha256` only after reviewing. Install verifies the current trust and
catalog again, checks the pinned bundle, and binds approval to the same
installation revision. For a theme, pass `--kind theme --id pack-example
--version 1.0.0 --bundle examples/themes/pack-example/theme.json`.
Retain the installation output in your private release records so you can trace
installed packages to the signing key during a revocation.

A newly approved plugin stays disabled until enabled in the dashboard. An
enabled plugin upgrade runs pending migrations in the approval transaction.
An incompatible package, stale review or migration failure leaves the approved
version and its data intact. Approved versions live in checksum-addressed
directories and are rechecked at runtime. Start the server to use the approved
version. Select a new theme in the dashboard as usual. To install a local plugin
or theme, use the existing dashboard checksum approval flow described in
[plugins](plugins.md) and [owner tools](owner-tools.md).

Disable a plugin from **Plugins** to stop its routes and jobs while keeping its
data. Export extension data with `blog backup --file /absolute/pre-upgrade.zip`.
The backup includes approved matching files, plugin storage and settings and
theme approvals. Keep it encrypted. Plugin secrets need separate reprovisioning.
To recover after an applied migration, restore the pre-upgrade backup into a
fresh data directory using the matching Modawana release and follow the
[re-enrollment and resume steps](operations/recovery.md).
