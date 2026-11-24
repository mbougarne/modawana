CREATE TABLE analytics_settings (
 id INTEGER PRIMARY KEY CHECK(id=1),
 version INTEGER NOT NULL CHECK(version>0),
 enabled INTEGER NOT NULL CHECK(enabled IN (0,1)),
 consent TEXT NOT NULL CHECK(consent IN ('opt_in','unless_declined')),
 retention_days INTEGER NOT NULL CHECK(retention_days BETWEEN 1 AND 90),
 exclusions TEXT NOT NULL CHECK(json_valid(exclusions))
) STRICT;

INSERT INTO analytics_settings VALUES(1,1,0,'opt_in',30,'[]');

CREATE TABLE analytics_daily (
 day TEXT NOT NULL,
 event TEXT NOT NULL CHECK(event IN ('page_view','lead_submitted')),
 path TEXT NOT NULL,
 count INTEGER NOT NULL CHECK(count>0),
 PRIMARY KEY(day,event,path)
) STRICT, WITHOUT ROWID;
