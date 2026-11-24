CREATE TABLE editorial_settings (
 id INTEGER PRIMARY KEY CHECK(id=1),
 version INTEGER NOT NULL CHECK(version>0),
 enabled INTEGER NOT NULL CHECK(enabled IN (0,1)),
 daily_limit INTEGER NOT NULL CHECK(daily_limit BETWEEN 1 AND 100),
 credential BLOB
) STRICT;

INSERT INTO editorial_settings VALUES(1,1,0,20,NULL);

CREATE TABLE editorial_usage (
 day TEXT PRIMARY KEY,
 requests INTEGER NOT NULL CHECK(requests BETWEEN 1 AND 100)
) STRICT, WITHOUT ROWID;
