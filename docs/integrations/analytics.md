# Site analytics

[العربية](analytics.ar.md) · [Documentation](../README.md)

Analytics is an optional first-party extension. Collection starts off. Open
**Analytics** in the owner dashboard to enable local counts, select the visitor
choice policy, and set path exclusions and a retention period.

## Collection

| Event | Counted when |
| --- | --- |
| `page_view` | A public HTML page is served successfully in response to a GET. The count is attributed to the canonical path. |
| `lead_submitted` | A published public form accepts a new submission. An exact replay of the accepted submission leaves the count unchanged. |

The extension counts public activity, including repeat views. Private,
unlisted, and gated pages, admin and staff routes, redirects, HEAD requests,
and prefetches are excluded. Rules accept one localized canonical path per line.
An exact path such as `/en/about` excludes that page. A suffix of `/*` also
excludes descendants, as in `/en/learn/*`. Rules apply to new counts.

In **Count only after opt-in**, visitors can allow collection in the public
footer before their activity is counted. **Count unless declined** counts
eligible activity until the visitor opts out. Either setting honors
`Sec-GPC: 1` and `DNT: 1`. The first-party preference cookie lasts 180 days.
It contains the choice alone and can be changed through the footer. Lead form
consent and analytics preference are separate.

## Storage and reports

SQLite holds only UTC day, canonical public path, event name, and count.
No query strings, referrers, browser or account identifiers, IP addresses,
form inputs, or commerce records are sent to the collector. The extension
uses server-rendered preference controls and has no tracking script settings.

The owner can inspect totals, daily counts, and up to 30 top paths over the
retention period. Retention defaults to 30 days and can be set from 1 to 90
days, including the current UTC day. Expired rows are removed by the
background worker and when settings change. Turning off collection leaves
saved aggregates subject to their normal expiry.

A bounded buffer aggregates new counts in memory. The worker writes one
transaction per batch, typically every 30 seconds. A single pageview never
writes an analytics row. Reports show committed batches. Pending data can be
lost during a crash or discarded when settings change. Successful form
submissions are committed independently of analytics.

The Go adapter boundary accepts only bounded aggregate batches. This release
includes a local SQLite adapter. See the [OpenAPI schema](../api/v1.openapi.json)
for report, settings, and visitor-choice endpoints.
