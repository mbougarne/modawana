CREATE TABLE staff_invites (
 id TEXT PRIMARY KEY,
 token_hash TEXT NOT NULL UNIQUE,
 owner_id TEXT NOT NULL REFERENCES owner(id),
 name TEXT NOT NULL,
 role TEXT NOT NULL CHECK(role IN ('owner','editor','store_manager')),
 expires_at TEXT NOT NULL,
 consumed_at TEXT,
 revoked_at TEXT
) STRICT;

CREATE TABLE staff_members (
 id TEXT PRIMARY KEY,
 owner_id TEXT NOT NULL REFERENCES owner(id),
 name TEXT NOT NULL,
 role TEXT NOT NULL CHECK(role IN ('owner','editor','store_manager')),
 credential_id TEXT NOT NULL UNIQUE,
 credential TEXT NOT NULL CHECK(json_valid(credential)),
 revoked_at TEXT
) STRICT;

CREATE TABLE staff_sessions (
 token_hash TEXT PRIMARY KEY,
 member_id TEXT NOT NULL REFERENCES staff_members(id),
 expires_at TEXT NOT NULL
) STRICT;
CREATE INDEX staff_sessions_member ON staff_sessions(member_id);
CREATE INDEX staff_sessions_expiry ON staff_sessions(expires_at);

CREATE TABLE staff_flows (
 token_hash TEXT PRIMARY KEY,
 invite_id TEXT REFERENCES staff_invites(id),
 ceremony TEXT NOT NULL CHECK(ceremony IN ('enroll','login')),
 challenge TEXT NOT NULL CHECK(json_valid(challenge)),
 expires_at TEXT NOT NULL,
 CHECK((ceremony='enroll' AND invite_id IS NOT NULL) OR (ceremony='login' AND invite_id IS NULL))
) STRICT;
CREATE INDEX staff_flows_expiry ON staff_flows(expires_at);

CREATE TABLE staff_revision_authorship (
 revision_id TEXT PRIMARY KEY REFERENCES revisions(id),
 member_id TEXT NOT NULL REFERENCES staff_members(id)
) STRICT;

ALTER TABLE outbox_events ADD COLUMN staff_actor_id TEXT REFERENCES staff_members(id);
