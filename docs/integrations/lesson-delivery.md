# Blog and Notion delivery

[العربية](lesson-delivery.ar.md) · [Publisher](../api/publisher.md)

The Go publisher can deliver one lesson to a blog draft and a separate Notion page.
Each destination is checkpointed independently. A successful delivery is not repeated
because the other destination failed. There is no two-way synchronization of edits,
quizzes, or learner progress.

```sh
build/publisher deliver --input delivery.json --state-dir /var/lib/lesson-runner
```

Supply `BLOG_URL`, `BLOG_PUBLISHER_TOKEN`, and `NOTION_API_KEY` through the runner's
private environment. `BLOG_PUBLISHER_CA` optionally names a trusted CA file. The
Notion integration needs access to the configured parent page. A connected chat's
Notion access does not supply REST credentials to this process.

The manifest contains `key`, `locale`, `source_revision`, `notion_parent`,
`auto_publish` (start with false), and `draft` (the publisher DraftInput, including
Markdown). Use stable keys/revisions and an existing collection/position.
Translations remain drafts. Original-language auto-publication requires an explicitly
configured collection and a suitably scoped token; it cannot review assessments,
review translations, or activate gates.

Keep the state directory private, backed up and outside Git/release directories.
The ledger records IDs, versions, hashes and outcomes, not credentials or lesson
bodies. A per-key exclusive lock prevents overlapping workers and survives a crash.
Confirm no worker is running and inspect both destinations before removing a stale lock.

After interruption, reuse the exact manifest. The runner rejects changed sources,
destinations or hashes rather than overwriting independent edits. Blog requests
reuse stable idempotency keys. Notion pages use a stable `[key/locale]` title suffix
and source revision/hash marker; reconciliation paginates the parent's children.
Multiple matches or mismatched content stop delivery. After an uncertain create,
only reconciliation is allowed: no visible match is not proof creation failed.
Payloads larger than 500000 encoded JSON bytes are refused.

Verify a real two-destination delivery, a one-destination failure and duplicate-free
retry from the actual runner before enabling a schedule. See
[scheduled generation](remote-publishing.md) and [release checks](../release-readiness.md).
