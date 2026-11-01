ALTER TABLE config_revision_media ADD COLUMN public INTEGER NOT NULL DEFAULT 1 CHECK(public IN (0,1));
CREATE TABLE config_translation_reviews (
 revision_id TEXT NOT NULL REFERENCES config_revisions(id),
 field_path TEXT NOT NULL,
 locale TEXT NOT NULL CHECK(locale IN ('ar','fr')),
 source_hash TEXT NOT NULL CHECK(length(source_hash)=64),
 value_hash TEXT NOT NULL CHECK(length(value_hash)=64),
 reviewed_by TEXT NOT NULL REFERENCES owner(id),
 PRIMARY KEY(revision_id,field_path,locale)
) STRICT;
CREATE TRIGGER immutable_config_review BEFORE UPDATE ON config_translation_reviews BEGIN SELECT RAISE(ABORT,'immutable configuration review'); END;
CREATE TABLE config_audit (
 id TEXT PRIMARY KEY NOT NULL,
 revision_id TEXT NOT NULL REFERENCES config_revisions(id),
 actor_id TEXT NOT NULL REFERENCES owner(id),
 created_at TEXT NOT NULL DEFAULT(strftime('%Y-%m-%dT%H:%M:%fZ','now'))
) STRICT;
