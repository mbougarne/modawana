ALTER TABLE owner ADD COLUMN login_email TEXT CHECK(length(login_email)<=254);
ALTER TABLE owner ADD COLUMN totp_cipher BLOB;
ALTER TABLE owner ADD COLUMN totp_step INTEGER NOT NULL DEFAULT -1;
ALTER TABLE sessions ADD COLUMN last_seen_at TEXT;
ALTER TABLE sessions ADD COLUMN credential_id TEXT;
CREATE TABLE auth_grants (
 token_hash TEXT PRIMARY KEY NOT NULL,
 purpose TEXT NOT NULL CHECK(purpose IN ('bootstrap','recovery')),
 expires_at TEXT NOT NULL,
 consumed_at TEXT
) STRICT;
CREATE TABLE auth_flows (
 token_hash TEXT PRIMARY KEY NOT NULL,
 kind TEXT NOT NULL CHECK(kind IN ('anonymous','bootstrap','recovery','password','managed')),
 owner_id TEXT REFERENCES owner(id),
 auth_version INTEGER,
 expires_at TEXT NOT NULL,
 challenge TEXT,
 ceremony TEXT CHECK(ceremony IN ('register','login','reauth','test')),
 session_hash TEXT,
 pending_password TEXT,
 pending_totp BLOB,
 consumed_at TEXT
) STRICT;
CREATE TABLE passkeys (
 id TEXT PRIMARY KEY NOT NULL,
 owner_id TEXT NOT NULL REFERENCES owner(id),
 credential TEXT NOT NULL CHECK(json_valid(credential)),
 label TEXT NOT NULL CHECK(length(label) BETWEEN 1 AND 120),
 created_at TEXT NOT NULL,
 last_used_at TEXT,
 tested_at TEXT,
 recovery_pending INTEGER NOT NULL DEFAULT 0 CHECK(recovery_pending IN (0,1)),
 revoked_at TEXT
) STRICT;
CREATE TABLE recovery_codes (
 code_hash TEXT PRIMARY KEY NOT NULL,
 owner_id TEXT NOT NULL REFERENCES owner(id),
 consumed_at TEXT
) STRICT;
CREATE TABLE auth_throttles (
 bucket TEXT PRIMARY KEY NOT NULL,
 attempts INTEGER NOT NULL,
 expires_at TEXT NOT NULL
) STRICT;
CREATE TABLE security_events (
 id INTEGER PRIMARY KEY,
 event TEXT NOT NULL,
 request_id TEXT NOT NULL,
 created_at TEXT NOT NULL
) STRICT;
