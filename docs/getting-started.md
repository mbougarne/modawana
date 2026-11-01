# Local development

[العربية](getting-started.ar.md) · [Documentation](README.md)

Use the versions pinned in `.node-version`, `frontend/package.json`, and
`backend/go.mod`. Run all `scripts/project.mjs` commands from the repository root.
`install` runs `npm ci` in `frontend`; Go resolves modules when building or testing.

```sh
node scripts/project.mjs install
export BLOG_DATA_ROOT="$HOME/.local/share/modawana-dev"
node scripts/project.mjs dev
```

Webpack listens on `127.0.0.1:3000` and proxies API/media requests to Go on
`127.0.0.1:8082`. Open `http://localhost:3000/en`, `/ar`, or `/fr`. Ctrl+C stops
both development processes. Keep the data root absolute, private, and outside the
checkout. Without `BLOG_DATA_ROOT`, the launcher uses a checkout-specific temporary
directory which the operating system can clear. Use the same data root in every terminal.

## First owner

In a second terminal run `node scripts/project.mjs bootstrap`. Redeem the one-use,
ten-minute grant in the login page's local setup/recovery form using your private
email. Register a passkey and then sign in with it. No owner or content is seeded.
An existing owner signs in or recovers; bootstrap does not replace an account.

Chromium supports the development flow on HTTP `localhost`; browser behavior varies.
Keep the hostname consistent, since `localhost` and `127.0.0.1` are distinct origins.
If your browser/provider cannot enroll there, use trusted HTTPS below.

For automated-style local testing in Chrome, open DevTools → More tools → WebAuthn.
Enable the virtual environment and add a CTAP2 internal authenticator with resident
keys and user verification. Keep user verification and automatic presence enabled,
and keep DevTools open during enrollment and sign-in. This is a simulated factor;
it does not prove real phone/provider/security-key compatibility.

## Trusted local HTTPS

Stop HTTP development first. With `mkcert` installed, create a trusted localhost
certificate outside Git. Installing its local CA may need operating-system permission.

```sh
mkdir -p /tmp/modawana-local-tls
chmod 700 /tmp/modawana-local-tls
mkcert -install
mkcert -cert-file /tmp/modawana-local-tls/cert.pem -key-file /tmp/modawana-local-tls/key.pem localhost 127.0.0.1
export BLOG_DEV_TLS_CERT=/tmp/modawana-local-tls/cert.pem
export BLOG_DEV_TLS_KEY=/tmp/modawana-local-tls/key.pem
node scripts/project.mjs dev:https
```

Open `https://localhost:3000/en/login`. Use `node scripts/project.mjs bootstrap:https`
in the second terminal, with the same data root. The launcher validates the key and
certificate before starting; it does not install a CA or weaken production cookies.

## Recovery and common problems

- Lost a development factor, or created the account without finishing enrollment:
  run `node scripts/project.mjs recover` (`recover:https` for HTTPS), refresh login,
  and redeem the new grant with the **same email**. Recovery revokes sessions and
  pending flows while preserving content. Register and test the replacement factor.
- A grant expires after ten minutes and is single-use. Keep it out of URLs, logs,
  screenshots, chat, and shell arguments.
- A changed RP ID/domain does not migrate existing passkeys. Use the documented
  [recovery procedure](owner-security.md); do not relax allowed origins.
- Port already in use: stop the previous development process. `BLOG_LISTEN_ADDR`
  can select another backend IP/port; the development proxy follows it.

For production use the [deployment guide](deployment.md). Development TLS and
virtual authenticators are testing tools, not production security configuration.
