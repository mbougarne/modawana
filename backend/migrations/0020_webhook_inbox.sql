CREATE TABLE webhook_connections (
 id TEXT PRIMARY KEY,
 adapter TEXT NOT NULL,
 account TEXT NOT NULL,
 mode TEXT NOT NULL CHECK(mode IN ('sandbox','live')),
 enabled INTEGER NOT NULL CHECK(enabled IN (0,1))
) STRICT;

CREATE TABLE webhook_receipts (
 ordinal INTEGER PRIMARY KEY,
 connection_id TEXT NOT NULL REFERENCES webhook_connections(id),
 event_id TEXT NOT NULL,
 body_digest BLOB NOT NULL CHECK(length(body_digest)=32),
 subject TEXT NOT NULL,
 subject_version INTEGER NOT NULL CHECK(subject_version>0),
 subject_status TEXT NOT NULL CHECK(subject_status IN ('active','inactive')),
 state TEXT NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','applied','superseded','conflict')),
 received_at TEXT NOT NULL,
 UNIQUE(connection_id,event_id)
) STRICT;
CREATE INDEX webhook_receipts_pending ON webhook_receipts(ordinal) WHERE state='pending';

CREATE TABLE webhook_resource_status (
 connection_id TEXT NOT NULL REFERENCES webhook_connections(id),
 subject TEXT NOT NULL,
 version INTEGER NOT NULL CHECK(version>0),
 status TEXT NOT NULL CHECK(status IN ('active','inactive')),
 PRIMARY KEY(connection_id,subject)
) STRICT;
