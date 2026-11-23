-- Keep existing item IDs and foreign-key references while extending the kind constraint.
PRAGMA defer_foreign_keys=ON;
DROP TRIGGER collection_kind_insert;
DROP TRIGGER collection_kind_update;
CREATE TABLE content_items_next (
 id TEXT PRIMARY KEY NOT NULL CHECK(length(id)=32 AND id NOT GLOB '*[^0-9a-f]*'),
 key TEXT NOT NULL UNIQUE CHECK(length(key) BETWEEN 1 AND 120 AND key NOT GLOB '*[^a-z0-9_-]*'),
 kind TEXT NOT NULL CHECK(kind IN ('article','lesson','doc','page')),
 source_locale TEXT NOT NULL CHECK(source_locale IN ('en','ar','fr')),
 created_at TEXT NOT NULL DEFAULT(strftime('%Y-%m-%dT%H:%M:%fZ','now')) CHECK(created_at IS NULL OR (length(created_at)=24 AND substr(created_at,-1)='Z' AND strftime('%Y-%m-%dT%H:%M:%fZ',created_at) IS NOT NULL AND strftime('%Y-%m-%dT%H:%M:%fZ',created_at)=created_at)),
 updated_at TEXT NOT NULL DEFAULT(strftime('%Y-%m-%dT%H:%M:%fZ','now')) CHECK(updated_at IS NULL OR (length(updated_at)=24 AND substr(updated_at,-1)='Z' AND strftime('%Y-%m-%dT%H:%M:%fZ',updated_at) IS NOT NULL AND strftime('%Y-%m-%dT%H:%M:%fZ',updated_at)=updated_at))
) STRICT;
INSERT INTO content_items_next (id,key,kind,source_locale,created_at,updated_at)
 SELECT id,key,kind,source_locale,created_at,updated_at FROM content_items;
DROP TABLE content_items;
ALTER TABLE content_items_next RENAME TO content_items;
CREATE TRIGGER collection_kind_insert BEFORE INSERT ON collection_items WHEN NOT EXISTS (
 SELECT 1 FROM content_items i JOIN collections c ON c.id=NEW.collection_id WHERE i.id=NEW.item_id AND ((i.kind='lesson' AND c.kind='learning') OR (i.kind='doc' AND c.kind='docs'))
) BEGIN SELECT RAISE(ABORT,'collection kind mismatch'); END;
CREATE TRIGGER collection_kind_update BEFORE UPDATE OF collection_id,item_id ON collection_items WHEN NOT EXISTS (
 SELECT 1 FROM content_items i JOIN collections c ON c.id=NEW.collection_id WHERE i.id=NEW.item_id AND ((i.kind='lesson' AND c.kind='learning') OR (i.kind='doc' AND c.kind='docs'))
) BEGIN SELECT RAISE(ABORT,'collection kind mismatch'); END;
CREATE TRIGGER immutable_content_key BEFORE UPDATE OF key ON content_items WHEN NEW.key!=OLD.key BEGIN SELECT RAISE(ABORT,'immutable content key'); END;
CREATE TRIGGER content_kind_member_update BEFORE UPDATE OF kind ON content_items
 WHEN NEW.kind!=OLD.kind AND EXISTS(SELECT 1 FROM collection_items WHERE item_id=OLD.id)
 BEGIN SELECT RAISE(ABORT,'content kind has collection membership'); END;
CREATE TRIGGER immutable_content_kind BEFORE UPDATE OF kind,source_locale ON content_items
 WHEN NEW.source_locale!=OLD.source_locale OR (NEW.kind!=OLD.kind AND NOT EXISTS(
 SELECT 1 FROM content_type_conversion_guard g WHERE g.item_id=OLD.id AND g.from_kind=OLD.kind AND g.to_kind=NEW.kind))
 BEGIN SELECT RAISE(ABORT,'explicit content conversion required'); END;
ALTER TABLE revisions ADD COLUMN page_template TEXT NOT NULL DEFAULT ''
 CHECK(page_template IN ('','standard','landing'));
