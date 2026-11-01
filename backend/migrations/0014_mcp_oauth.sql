CREATE TABLE oauth_clients (
 id TEXT PRIMARY KEY, redirect_uri TEXT NOT NULL, expires_at TEXT NOT NULL
) STRICT;
CREATE TABLE oauth_requests (
 id TEXT PRIMARY KEY, client_id TEXT NOT NULL REFERENCES oauth_clients(id) ON DELETE CASCADE,
 state TEXT NOT NULL, challenge TEXT NOT NULL, expires_at TEXT NOT NULL,
 code_hash TEXT UNIQUE, token_id TEXT REFERENCES api_tokens(id) ON DELETE CASCADE,
 auth_version INTEGER, consumed INTEGER NOT NULL DEFAULT 0 CHECK(consumed IN (0,1))
) STRICT;
CREATE TABLE oauth_grants (
 id TEXT PRIMARY KEY, code_hash TEXT NOT NULL UNIQUE, client_id TEXT NOT NULL REFERENCES oauth_clients(id) ON DELETE CASCADE,
 token_id TEXT NOT NULL REFERENCES api_tokens(id) ON DELETE CASCADE,
 auth_version INTEGER NOT NULL, access_hash TEXT NOT NULL UNIQUE,
 access_expires TEXT NOT NULL, refresh_hash TEXT NOT NULL UNIQUE,
 expires_at TEXT NOT NULL, revoked INTEGER NOT NULL DEFAULT 0 CHECK(revoked IN (0,1))
) STRICT;
CREATE TABLE oauth_refresh_used (
 hash TEXT PRIMARY KEY, grant_id TEXT NOT NULL REFERENCES oauth_grants(id) ON DELETE CASCADE
) STRICT;
