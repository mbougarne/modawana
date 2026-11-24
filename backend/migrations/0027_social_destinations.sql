CREATE TABLE social_provider_connections (
 id TEXT PRIMARY KEY,
 provider TEXT NOT NULL UNIQUE,
 instance TEXT NOT NULL,
 account_id TEXT NOT NULL,
 account TEXT NOT NULL,
 account_url TEXT NOT NULL,
 token BLOB NOT NULL
) STRICT;

INSERT INTO social_provider_connections(id,provider,instance,account_id,account,account_url,token)
 SELECT id,'mastodon',instance,account_id,account,account_url,token FROM social_connections;
DROP TABLE social_connections;
ALTER TABLE social_provider_connections RENAME TO social_connections;

ALTER TABLE social_oauth_flows ADD COLUMN provider TEXT NOT NULL DEFAULT 'mastodon';
ALTER TABLE social_posts ADD COLUMN provider TEXT NOT NULL DEFAULT 'mastodon';
ALTER TABLE social_posts ADD COLUMN image TEXT NOT NULL DEFAULT '';
ALTER TABLE social_posts ADD COLUMN container TEXT NOT NULL DEFAULT '';
