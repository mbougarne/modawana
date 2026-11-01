# Contributor instructions

[العربية](AGENTS.ar.md)

## Architecture

Read [architecture](docs/architecture.md) and the detailed
[configuration and learning contracts](docs/architecture-appendix-configurable-learning.md).
Use Go, React, TypeScript, Webpack, and one local SQLite database. Markdown revisions
and uploads are immutable persistent files outside the checkout and release directory.
Node is a development/build dependency. No public signup.

Keep application source in `backend/` and `frontend/`. Root `ui/src` is shared
React source imported as `@ui`; `design-system/` holds token sources and generators.
`deploy/` contains reusable deployment configuration, not installation secrets.

## Security and content

Enforce access in Go on every route, preview, media response, search result, and feed.
A published gated lesson is not automatically public. Never send a protected body
and hide it with CSS. Grade quizzes server-side and withhold answer keys before grading.
Preserve immutable revisions, optimistic versions, and exact idempotent replay.
Review translations and assessments before publication where required.

Read [owner security](docs/owner-security.md) and [passkey access](docs/passkey-access.md).
Support multiple credentials, an independent backup, and offline recovery codes.
Synced copies of one credential are not independent factors. No public MFA-disable
route or password-only shortcut. Retain the last usable factor until a replacement
is tested. Virtual authenticators do not prove phone/provider/security-key support.

## Frontend and documentation

Follow [design](docs/design.md) and [UI contracts](docs/ui-contracts.md).
`design-system/tokens.json` is the token source of truth. Regenerate with
`node design-system/scripts/build-tokens.mjs` and `node ui/scripts/build-tokens.mjs`;
do not hand-edit generated tokens. Keep both component CSS copies identical.
Use shared providers, `t()` for interface labels in EN/AR/FR, and logical CSS for RTL.
Do not seed demonstration identities or content.

Write new or materially changed documentation in English and Arabic together,
using linked pairs or complete bilingual sections. Keep commands and contracts
consistent. Documentation describes supported behavior and procedures; personal
instructions, conversation transcripts, and work logs do not belong in it.
Preserve original third-party license notices.

## Verification

Use focused tests for meaningful behavior changes. Run
`node scripts/project.mjs format`, `node scripts/project.mjs check`, and the build
when relevant. Imports precede declarations; declarations and exports are separated;
control flow uses braces. Never bypass style, language, or generated-file checks.
CI runs unit/API, race, security, packaging, and HTTP/restart checks; browser scripts
are optional local diagnostics. See [Contributing](CONTRIBUTING.md).

Keep credentials, runtime data, backups, generated artifacts, and local work notes
out of Git. Record tests actually run and remaining limitations in the PR.
Use [release readiness](docs/release-readiness.md) for deployment and public-release
checks; a passing build does not establish hardware or live integration acceptance.
