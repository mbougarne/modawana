CREATE TABLE theme_packs (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    checksum TEXT NOT NULL CHECK(length(checksum) = 64),
    revision TEXT NOT NULL UNIQUE CHECK(length(revision) = 32)
) STRICT;
