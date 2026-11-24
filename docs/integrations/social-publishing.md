# Social publishing

[العربية](social-publishing.ar.md) · [Documentation](../README.md)

Social Publishing is an optional first-party extension shipped with Modawana.
Configure any of the supported providers, then connect each destination
independently. Each scheduled post targets one connection and one published
revision.

## Configure provider apps

Register the blog’s canonical HTTPS origin followed by
`/api/v1/owner/social/callback` as the redirect URI in each developer app.
Keep `BLOG_AUTH_KEY_FILE` configured. Store each client secret in a private
regular file outside `BLOG_DATA_ROOT`. The server process needs read access and
the file needs no group or other permissions. Keep callback query strings out
of any proxy access logs.

For Facebook, X, Instagram, and LinkedIn, set the named environment prefix
followed by `CLIENT_ID` and `CLIENT_SECRET_FILE`. Set the listed additional
variables with the same prefix. Use the Graph API version enabled for the app
in `vNN.0` format and a supported LinkedIn Marketing API version in `YYYYMM`
format.

| Destination | Environment prefix and additional settings | Provider permissions |
| --- | --- | --- |
| Mastodon | `BLOG_MASTODON_ORIGIN`, `BLOG_MASTODON_CLIENT_ID`, `BLOG_MASTODON_CLIENT_SECRET_FILE`. Use a Mastodon 4.3 or later public HTTPS origin. | `profile`, `write:statuses` |
| Facebook Page | `BLOG_SOCIAL_FACEBOOK_`, followed by `PAGE_ID` and `API_VERSION` | `pages_show_list`, `pages_read_engagement`, `pages_manage_posts`. Grant publishing authority on the configured Page. |
| X account | `BLOG_SOCIAL_X_`. Configure a confidential OAuth 2.0 app with read and write access. X bills API requests separately. | `tweet.read`, `tweet.write`, `users.read`, `offline.access` |
| Instagram professional account | `BLOG_SOCIAL_INSTAGRAM_`, followed by `API_VERSION`. Use Instagram Login for a Business or Creator account. | `instagram_business_basic`, `instagram_business_content_publish` |
| LinkedIn personal profile | `BLOG_SOCIAL_LINKEDIN_PROFILE_`. Enable Sign In with LinkedIn using OpenID Connect and Share on LinkedIn. | `openid`, `profile`, `w_member_social` |
| LinkedIn company Page | `BLOG_SOCIAL_LINKEDIN_PAGE_`, followed by `ORGANIZATION_ID` (numeric) and `API_VERSION`. Use a Community Management app and authenticate as an approved Page admin with publishing authority. | `r_organization_admin`, `w_organization_social` |

Leave every setting for a provider empty to leave it disabled. For Docker
Compose, pass the environment settings to the `blog` service in a private
override. Bind each credential read-only outside the data directory, for
example at `/run/secrets/social-client-secret`, and use the container path in
its `CLIENT_SECRET_FILE` setting. Give UID 65532 read access to the mounted files.
Load the override alongside `deploy/compose.yml`.

Restart Modawana and open **Plugins → Social publishing**. Connect the desired
accounts and check the destination names. Connecting and disconnecting require
recent owner authentication. Only encrypted credentials are stored in SQLite.
Reconnect expired or revoked credentials. A restored backup requires fresh
connections. Disconnect clears the local credential. To revoke the provider
grant, use the provider’s authorized applications settings.

## Preview and schedule

Select the destination, language and published revision. Enter the post text
and preview the destination, text, and canonical link. Instagram requires an
approved public JPEG cover on that revision. The preview shows the image and
caption. Instagram displays an article URL in a caption as part of the text.

Choose **Send now** or schedule up to 90 days ahead. The Modawana worker sends
the post at the scheduled time. It rechecks the original account, the public
revision, its canonical URL, and the public cover immediately before sending.
You can cancel in Delivery history until the worker starts dispatch.

## Delivery history

History provides the destination, time, attempts, outcome and provider receipt
when available. A short outage before dispatch can trigger a retry. Mastodon
allows bounded replay with the same idempotency key for up to 45 minutes from
the first attempt. Instagram can confirm a completed publish through the media
container. When the outcome of a send remains uncertain, check the destination
account before creating a replacement post.

The [OpenAPI schema](../api/v1.openapi.json) documents the owner endpoints.
