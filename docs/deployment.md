# Deployment

[العربية](deployment.ar.md)

Build/test with Go **1.27.3**, Node **22.23.4**, npm **10.9.2** and Docker Engine /
Compose v2 on Linux. One Go process serves API and Webpack assets; production has
no Node, Redis or separate database. SQLite and immutable files need a local
persistent filesystem, not NFS or a release directory. Never scale the app to two
instances. Runtime lease rejects a second server on the same data root.

Copy `.env.example` to a private file outside Git, replacing every placeholder.
The Go binary reads environment variables, not dotenv files. For native commands
load a trusted shell-compatible file using `set -a; . /private/blog.env; set +a`.
Use an exact real HTTPS domain/RP ID. Changing domains requires passkey re-enrollment;
provider credentials do not automatically transfer. `.invalid` placeholders are
rejected in production. Allocate private data/backups directories and a separate
32-byte binary key, all accessible only to UID/GID 65532 and the operator:

```sh
sudo install -d -m 700 -o 65532 -g 65532 /srv/personal-blog/data /srv/personal-blog/backups /srv/personal-blog/secrets
sudo sh -c 'umask 077; test ! -e /srv/personal-blog/secrets/auth-key && openssl rand 32 > /srv/personal-blog/secrets/auth-key'
sudo chown 65532:65532 /srv/personal-blog/secrets/auth-key
# Run from this repository root, with your private values in /private/blog.env.
docker compose --env-file /private/blog.env -f deploy/compose.yml build
# Keep the first owner grant private; no owner/content is seeded.
docker compose --env-file /private/blog.env -f deploy/compose.yml run --rm blog bootstrap
docker compose --env-file /private/blog.env -f deploy/compose.yml up -d
docker compose --env-file /private/blog.env -f deploy/compose.yml exec -T blog /app/blog ready
```

Configure host Caddy with the supplied Caddyfile and actual `BLOG_HOSTNAME`, DNS and
ports 80/443. Only loopback port 8080 is exposed by Compose. Set trusted proxy CIDRs
to actual observed proxy peers; never trust arbitrary forwarded headers. Production
startup warns when no trusted proxy is configured; behind Docker, trust only the
observed proxy peer (which can be the bridge gateway), not every client. Keep logs
free of query strings, cookies, credentials and bodies. Complete owner setup at
`/en/login`, add/test an independent backup and offline recovery codes. Configure
name, branding, profile and translations in the dashboard; there is no source edit
or production content seed. Read [owner security](owner-security.md).

Before an upgrade: verify a current encrypted off-server backup and separate key
backup, run candidate CI/container checks, stop the old server, and back up with the
**old image before any candidate command opens the database**. A native command or
Compose run uses the selected image and can migrate at startup. Example with the
old service still running: `docker compose --env-file /private/blog.env -f deploy/compose.yml exec -T blog /app/blog backup --file /backups/pre-upgrade.zip`.
Encrypt/transfer it, stop the service, select the tested new image, then `up -d` and
check readiness, login, revisions and upload delivery. Release files/images and
persistent data stay separate. Never run an incompatible old binary on migrated
SQLite. Prefer a forward fix; rollback requires the matching old image plus its
pre-upgrade backup restored to a new directory using the recovery runbook. Restoring
invalidates credentials and learning progress. Keep the failed data for diagnosis.

[Recovery and retention](operations/recovery.md) includes offline restore,
re-enrollment, 24-hour RPO and seven-daily/four-weekly backups. Health 200/readiness
503 after restore is intentional until tested re-enrollment and `blog resume`.
For startup failure inspect redacted logs, private ownership, exact origin/RP ID,
key length, build manifest and schema compatibility; never delete SQLite or bypass
migration validation. See [verification](release-readiness.md) for
actual environment coverage. A successful image build is not a production rollout.
