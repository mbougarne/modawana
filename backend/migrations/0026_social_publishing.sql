CREATE TABLE social_connections (
 id TEXT PRIMARY KEY,
 slot INTEGER NOT NULL UNIQUE CHECK(slot=1),
 instance TEXT NOT NULL,
 account_id TEXT NOT NULL,
 account TEXT NOT NULL,
 account_url TEXT NOT NULL,
 token BLOB NOT NULL
) STRICT;

CREATE TABLE social_oauth_flows (
 state_hash TEXT PRIMARY KEY,
 browser_hash TEXT NOT NULL,
 session_id TEXT NOT NULL,
 instance TEXT NOT NULL,
 locale TEXT NOT NULL REFERENCES locale_packs(code),
 verifier BLOB NOT NULL,
 expires_at INTEGER NOT NULL,
 claimed INTEGER NOT NULL DEFAULT 0 CHECK(claimed IN (0,1))
) STRICT;

CREATE TABLE social_posts (
 id TEXT PRIMARY KEY REFERENCES outbox_deliveries(id),
 connection_id TEXT NOT NULL,
 instance TEXT NOT NULL,
 account TEXT NOT NULL,
 content_key TEXT NOT NULL,
 locale TEXT NOT NULL REFERENCES locale_packs(code),
 revision_id TEXT NOT NULL,
 path TEXT NOT NULL,
 body TEXT NOT NULL,
 request_hash TEXT NOT NULL,
 created_at INTEGER NOT NULL,
 scheduled_at INTEGER NOT NULL,
 first_dispatch_at INTEGER NOT NULL DEFAULT 0,
 outcome TEXT NOT NULL DEFAULT '' CHECK(outcome IN ('','sent','blocked','cancelled','uncertain','failed')),
 reason TEXT NOT NULL DEFAULT '',
 remote_id TEXT NOT NULL DEFAULT '',
 remote_url TEXT NOT NULL DEFAULT ''
) STRICT;

CREATE INDEX social_post_history ON social_posts(created_at DESC,id DESC);
