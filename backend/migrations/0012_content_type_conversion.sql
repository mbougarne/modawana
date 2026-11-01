-- Explicit owner conversion keeps item identity, revisions and publication pointers.
CREATE TABLE content_type_conversion_guard (
 item_id TEXT PRIMARY KEY NOT NULL REFERENCES content_items(id),
 from_kind TEXT NOT NULL CHECK(from_kind IN ('article','lesson','doc')),
 to_kind TEXT NOT NULL CHECK(to_kind IN ('article','lesson','doc') AND to_kind!=from_kind)
) STRICT;
DROP TRIGGER immutable_content_kind;
CREATE TRIGGER immutable_content_kind BEFORE UPDATE OF kind,source_locale ON content_items
 WHEN NEW.source_locale!=OLD.source_locale OR (NEW.kind!=OLD.kind AND NOT EXISTS(
 SELECT 1 FROM content_type_conversion_guard g WHERE g.item_id=OLD.id AND g.from_kind=OLD.kind AND g.to_kind=NEW.kind))
 BEGIN SELECT RAISE(ABORT,'explicit content conversion required'); END;
DROP TRIGGER published_membership_delete;
CREATE TRIGGER published_membership_delete BEFORE DELETE ON collection_items
 WHEN EXISTS(SELECT 1 FROM content_versions WHERE item_id=OLD.item_id AND first_published_at IS NOT NULL)
 AND (NOT EXISTS(SELECT 1 FROM content_type_conversion_guard WHERE item_id=OLD.item_id)
 OR EXISTS(SELECT 1 FROM collection_plan_members m JOIN collection_plan_state st ON m.plan_id=st.active_plan_id OR m.plan_id=st.draft_plan_id WHERE m.item_id=OLD.item_id))
 BEGIN SELECT RAISE(ABORT,'published membership is stable'); END;
CREATE TABLE content_type_events (
 id TEXT PRIMARY KEY NOT NULL CHECK(length(id)=32),
 item_id TEXT NOT NULL REFERENCES content_items(id),
 from_kind TEXT NOT NULL,
 to_kind TEXT NOT NULL,
 created_by TEXT NOT NULL REFERENCES owner(id),
 created_at TEXT NOT NULL DEFAULT(strftime('%Y-%m-%dT%H:%M:%fZ','now'))
) STRICT;
CREATE TABLE content_route_aliases (
 path TEXT PRIMARY KEY NOT NULL,
 item_id TEXT NOT NULL REFERENCES content_items(id),
 locale TEXT NOT NULL CHECK(locale IN ('en','ar','fr'))
) STRICT;
