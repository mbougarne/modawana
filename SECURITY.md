# Security reporting

[العربية](SECURITY.ar.md)

This is a pre-production self-hosted application. The current checked revision is
the supported development target; there is no promise of older-branch maintenance
or a response-time SLA. See the [acceptance limits](docs/release-readiness.md).

Use the repository's GitHub **Security → Report a vulnerability** private reporting
entry if enabled. If unavailable, open an issue requesting a private contact without
including the exploit, credentials, owner data or private URLs. No monitored email
address is asserted here. Do not send sensitive reports to an unverified address.
Include affected revision/environment, minimal reproduction, impact and proposed
fix once a private channel is established. Coordinate disclosure with the maintainer.

If credentials may be exposed, revoke affected sessions/tokens/factors through a
trusted owner session or local VPS recovery, retain an independent tested factor,
and rotate the separate encryption key only through a documented migration/recovery
procedure. Restore invalidates credentials deliberately; never re-enable snapshot
credentials by editing SQLite. Preserve redacted evidence, not secrets.

CI runs npm audit, govulncheck, secret-pattern scanning, race tests and real-server
browser flows. These checks do not establish a comprehensive security audit. Real
hardware/provider access, off-server backup delivery and production configuration
require the operator evidence listed in the acceptance report.
