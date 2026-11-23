CREATE TABLE outbox_events (
 id TEXT PRIMARY KEY,
 name TEXT NOT NULL,
 contract_version INTEGER NOT NULL CHECK(contract_version>0),
 source_id TEXT NOT NULL,
 source_version INTEGER NOT NULL CHECK(source_version>0),
 payload TEXT NOT NULL CHECK(json_valid(payload) AND length(payload)<=4096),
 created_at INTEGER NOT NULL,
 UNIQUE(name,source_id,source_version)
) STRICT;

CREATE TABLE outbox_subscriptions (
 plugin_id TEXT NOT NULL REFERENCES plugin_installs(id) ON DELETE CASCADE,
 name TEXT NOT NULL,
 contract_version INTEGER NOT NULL CHECK(contract_version>0),
 PRIMARY KEY(plugin_id,name,contract_version)
) STRICT;
CREATE INDEX outbox_subscription_topic ON outbox_subscriptions(name,contract_version,plugin_id);

CREATE TABLE outbox_deliveries (
 id TEXT PRIMARY KEY,
 event_id TEXT NOT NULL REFERENCES outbox_events(id),
 plugin_id TEXT NOT NULL,
 plugin_slug TEXT NOT NULL,
 state TEXT NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','running','succeeded','failed')),
 attempts INTEGER NOT NULL DEFAULT 0 CHECK(attempts BETWEEN 0 AND 5),
 available_at INTEGER NOT NULL,
 lease_until INTEGER NOT NULL DEFAULT 0,
 lease_token TEXT NOT NULL DEFAULT '',
 updated_at INTEGER NOT NULL,
 last_error TEXT NOT NULL DEFAULT '',
 UNIQUE(event_id,plugin_id),
 CHECK((state='running' AND lease_until>0 AND lease_token<>'') OR
       (state<>'running' AND lease_until=0 AND lease_token=''))
) STRICT;

CREATE INDEX outbox_delivery_ready ON outbox_deliveries(available_at,id) WHERE state='pending';
CREATE INDEX outbox_delivery_leases ON outbox_deliveries(lease_until,id) WHERE state='running';
CREATE INDEX outbox_delivery_history ON outbox_deliveries(updated_at DESC,id DESC);
