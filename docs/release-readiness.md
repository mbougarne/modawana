# Release readiness

[العربية](release-readiness.ar.md) · [Documentation](README.md)

## Before deploying an installation

- Run the [contributor checks](../CONTRIBUTING.md) for the exact revision being released.
- Verify HTTPS, the exact origin/RP ID, reverse-proxy trust, filesystem ownership,
  persistent storage, and a private runtime key using [deployment](deployment.md).
- Enroll and test an independent backup factor and offline recovery codes. Test a
  new device, phone-assisted login, and physical security key on the actual supported
  OS/browser/provider combinations. Virtual authenticators alone are insufficient.
- Check keyboard and screen-reader reading, forms and errors in EN/AR/FR on desktop
  and phone. Automated accessibility tests do not replace manual screen-reader review.
- Verify an encrypted off-server backup, separate key backup, and an isolated restore
  with the matching release. Confirm credential invalidation, re-enrollment and resume.
- If using scheduled delivery, test from the actual runner with the intended credentials:
  both destinations, one-destination failure, uncertain responses, and retries without
  duplicates. Verify continuity before enabling the timer; avoid overlapping schedules.

Live dual-destination delivery, hardware/provider compatibility, manual screen-reader
coverage, and operator-specific restore/deployment acceptance require environment
results. They are not established by this repository's automated checks. Record the
revision, date, environment, procedure and observed result for each installation.

## Before making the repository public

A clean current tree does not make its history private. Deleted documents, old
application content, personal deployment notes, commit authors/messages, other
branches/tags, pull requests, issues, Actions logs/artifacts and releases may still
be visible when repository visibility changes.

Review the full remote history and GitHub surfaces. Rotate any exposed credential
before considering history cleanup. If historical material must remain private,
publish a reviewed fresh snapshot in a separate public repository, or plan a
coordinated history rewrite. Neither is performed by the source cleanup PR.
Do not treat a pattern scan as proof that all secrets or personal information are absent.

## Repository documentation policy

Public guides describe the product and supported procedures. Historical tickets,
conversation records, deployment diaries and illustrative prototypes are omitted
from the current tree. The architecture guide replaces the former frozen planning
baseline as the maintained reference; technical security and persistence contracts
remain in the architecture, API and operational guides. No history rewrite or
repository visibility change is implied by this documentation reorganization.

Shared UI and token sources and reusable deployment examples remain tracked because
they are required for reproducible builds and self-hosting. Runtime keys, environments,
content, backups and private notes remain outside source control. MCP/OAuth and the
lesson runner remain optional; no account, token or enabled schedule is shipped.
