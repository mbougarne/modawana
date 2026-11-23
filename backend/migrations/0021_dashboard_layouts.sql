CREATE TABLE dashboard_defaults (
 id INTEGER PRIMARY KEY CHECK(id=1),
 version INTEGER NOT NULL CHECK(version>0),
 snapshot TEXT CHECK(snapshot IS NULL OR json_valid(snapshot))
) STRICT;

CREATE TABLE dashboard_preferences (
 owner_id TEXT PRIMARY KEY REFERENCES owner(id) ON DELETE CASCADE,
 version INTEGER NOT NULL CHECK(version>0),
 snapshot TEXT CHECK(snapshot IS NULL OR json_valid(snapshot))
) STRICT;
