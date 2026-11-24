# Social publishing with Mastodon

[العربية](social-publishing.ar.md) · [Documentation](../README.md)

Social Publishing is an optional first-party extension shipped with Modawana.
It owns the editor, schedule, and delivery history. Mastodon is its first
provider adapter and appears under **Social publishing** on the Plugins screen
when configured.

## Configure the connection

1. On a Mastodon 4.3 or later instance, register an OAuth application with only
   the `profile` and `write:statuses` scopes. Use the blog’s canonical HTTPS
   origin followed by `/api/v1/owner/social/callback` as the redirect URI.
2. Set `BLOG_MASTODON_ORIGIN` to the instance’s public HTTPS origin, such as
   `https://mastodon.example`. Set `BLOG_MASTODON_CLIENT_ID` to the application’s
   client ID. Write the client secret to a private regular file outside
   `BLOG_DATA_ROOT` and set `BLOG_MASTODON_CLIENT_SECRET_FILE` to its absolute
   path. The server process needs read access and the file must have no group
   or other permissions. Keep the existing `BLOG_AUTH_KEY_FILE` configured.
3. Restart the app. Open **Social publishing** in the owner dashboard, choose
   **Connect Mastodon**, and confirm the displayed account on Mastodon. Configure
   any proxy access logs to omit query strings for the callback.

The provider stays disabled when all three Mastodon settings are omitted.
Each installation can connect one account on its configured instance.
Connecting and disconnecting require recent owner authentication. Only
ciphertext for the access token is stored in SQLite. Disconnect clears the local
credential. Use Mastodon’s authorized applications settings to revoke the
provider grant. A restored backup requires a fresh connection.

For Docker Compose, pass the three settings under the `blog` service’s
`environment` in a private Compose override. Add a read-only bind mount for
the client secret, for example at `/run/secrets/mastodon-client-secret`, and use
that container path in `BLOG_MASTODON_CLIENT_SECRET_FILE`. Give UID 65532 read
access to the mounted file. Load the override alongside `deploy/compose.yml`.

## Preview and schedule

Choose a language and an eligible public revision. Enter the post text and
preview the exact text, destination, and canonical link. The text and link
together can contain up to 500 characters. Choose **Send now** or enter a local
date and time up to 90 days ahead. The app queues the post on Modawana and the
background worker uses Mastodon to send it as a public status.

A scheduled post keeps its exact published revision and connection. Modawana
checks anonymous access, the canonical link, the original account, and its
authorization just before delivery. Unpublishing the revision, restricting
access, changing its route, or replacing the connection can stop queued work.
You can cancel in Delivery history until dispatch starts.

## Delivery history

History lists the scheduled time, outcome, attempts, and Mastodon receipt when
available. After a transient failure, the worker replays the same request with
the same provider idempotency key, including after a restart. Modawana stops
replaying within 45 minutes of the first send attempt. If history shows
**Delivery unconfirmed**, check the account’s Mastodon timeline before creating
a new scheduled post.

The [OpenAPI schema](../api/v1.openapi.json) documents the owner endpoints.
