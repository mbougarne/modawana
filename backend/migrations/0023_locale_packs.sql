-- Rebuild locale-bearing tables without changing keys, relationships, or existing locale values.
PRAGMA defer_foreign_keys=ON;
CREATE TABLE locale_packs (
 code TEXT PRIMARY KEY NOT NULL CHECK(code GLOB '[a-z][a-z]')
) STRICT;
INSERT INTO locale_packs(code) VALUES('en'),('ar'),('fr'),('es');
DROP TRIGGER stable_content_slug;
DROP TRIGGER stable_content_version_owner;
DROP TRIGGER published_membership_update;
DROP TRIGGER stable_collection_slug;
DROP TRIGGER immutable_quiz_binding;
DROP TRIGGER published_membership_delete;
DROP TRIGGER removed_collection_visibility;
DROP TRIGGER collection_kind_insert;
DROP TRIGGER collection_kind_update;
DROP TRIGGER immutable_content_key;
DROP TRIGGER content_kind_member_update;
DROP TRIGGER immutable_content_kind;
CREATE TABLE category_locales_next (
 category_id TEXT NOT NULL REFERENCES categories(id), locale TEXT NOT NULL REFERENCES locale_packs(code),
 label TEXT NOT NULL CHECK(length(label) BETWEEN 1 AND 120), slug TEXT NOT NULL CHECK(length(slug) BETWEEN 1 AND 160),
 PRIMARY KEY(category_id,locale), UNIQUE(locale,slug)
) STRICT;
INSERT INTO category_locales_next(category_id,locale,label,slug) SELECT category_id,locale,label,slug FROM category_locales;
DROP TABLE category_locales;
ALTER TABLE category_locales_next RENAME TO category_locales;
CREATE TABLE tag_locales_next (
 tag_id TEXT NOT NULL REFERENCES tags(id), locale TEXT NOT NULL REFERENCES locale_packs(code),
 label TEXT NOT NULL CHECK(length(label) BETWEEN 1 AND 120), slug TEXT NOT NULL CHECK(length(slug) BETWEEN 1 AND 160),
 PRIMARY KEY(tag_id,locale), UNIQUE(locale,slug)
) STRICT;
INSERT INTO tag_locales_next(tag_id,locale,label,slug) SELECT tag_id,locale,label,slug FROM tag_locales;
DROP TABLE tag_locales;
ALTER TABLE tag_locales_next RENAME TO tag_locales;
CREATE TABLE content_versions_next (
 id TEXT PRIMARY KEY NOT NULL CHECK(length(id)=32 AND id NOT GLOB '*[^0-9a-f]*'),
 item_id TEXT NOT NULL REFERENCES content_items(id),
 locale TEXT NOT NULL REFERENCES locale_packs(code),
 slug TEXT NOT NULL CHECK(length(slug) BETWEEN 1 AND 160 AND instr(slug,'/')=0 AND instr(slug,char(92))=0 AND instr(slug,'?')=0 AND instr(slug,'#')=0 AND slug NOT IN ('.','..')),
 draft_revision_id TEXT NOT NULL,
 published_revision_id TEXT,
 version INTEGER NOT NULL DEFAULT 1 CHECK(version>=1),
 first_published_at TEXT CHECK(first_published_at IS NULL OR (length(first_published_at)=24 AND substr(first_published_at,-1)='Z' AND strftime('%Y-%m-%dT%H:%M:%fZ',first_published_at) IS NOT NULL AND strftime('%Y-%m-%dT%H:%M:%fZ',first_published_at)=first_published_at)),
 last_published_at TEXT CHECK(last_published_at IS NULL OR (length(last_published_at)=24 AND substr(last_published_at,-1)='Z' AND strftime('%Y-%m-%dT%H:%M:%fZ',last_published_at) IS NOT NULL AND strftime('%Y-%m-%dT%H:%M:%fZ',last_published_at)=last_published_at)),
 UNIQUE(item_id,locale), UNIQUE(locale,slug),
 FOREIGN KEY(id,draft_revision_id) REFERENCES revisions(version_id,id) DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(id,published_revision_id) REFERENCES revisions(version_id,id) DEFERRABLE INITIALLY DEFERRED,
 CHECK(published_revision_id IS NULL OR (first_published_at IS NOT NULL AND last_published_at IS NOT NULL))
) STRICT;
INSERT INTO content_versions_next(id,item_id,locale,slug,draft_revision_id,published_revision_id,version,first_published_at,last_published_at) SELECT id,item_id,locale,slug,draft_revision_id,published_revision_id,version,first_published_at,last_published_at FROM content_versions;
DROP TABLE content_versions;
ALTER TABLE content_versions_next RENAME TO content_versions;
CREATE TABLE collection_locales_next (
 collection_id TEXT NOT NULL REFERENCES collections(id), locale TEXT NOT NULL REFERENCES locale_packs(code),
 slug TEXT NOT NULL CHECK(length(slug) BETWEEN 1 AND 160 AND instr(slug,'/')=0),
 title TEXT NOT NULL CHECK(length(title) BETWEEN 1 AND 200), summary TEXT NOT NULL DEFAULT '' CHECK(length(summary)<=500),
 visible INTEGER NOT NULL DEFAULT 0 CHECK(visible IN (0,1)),
 PRIMARY KEY(collection_id,locale), UNIQUE(locale,slug)
) STRICT;
INSERT INTO collection_locales_next(collection_id,locale,slug,title,summary,visible) SELECT collection_id,locale,slug,title,summary,visible FROM collection_locales;
DROP TABLE collection_locales;
ALTER TABLE collection_locales_next RENAME TO collection_locales;
CREATE TABLE quiz_bindings_next (
 revision_id TEXT PRIMARY KEY NOT NULL REFERENCES revisions(id),
 assessment_id TEXT NOT NULL REFERENCES assessments(id),
 locale TEXT NOT NULL REFERENCES locale_packs(code),
 wording_json TEXT NOT NULL CHECK(json_valid(wording_json) AND length(CAST(wording_json AS BLOB))<=262144)
) STRICT;
INSERT INTO quiz_bindings_next(revision_id,assessment_id,locale,wording_json) SELECT revision_id,assessment_id,locale,wording_json FROM quiz_bindings;
DROP TABLE quiz_bindings;
ALTER TABLE quiz_bindings_next RENAME TO quiz_bindings;
CREATE TABLE content_route_aliases_next (
 path TEXT PRIMARY KEY NOT NULL,
 item_id TEXT NOT NULL REFERENCES content_items(id),
 locale TEXT NOT NULL REFERENCES locale_packs(code)
) STRICT;
INSERT INTO content_route_aliases_next(path,item_id,locale) SELECT path,item_id,locale FROM content_route_aliases;
DROP TABLE content_route_aliases;
ALTER TABLE content_route_aliases_next RENAME TO content_route_aliases;
CREATE TABLE content_items_next (
 id TEXT PRIMARY KEY NOT NULL CHECK(length(id)=32 AND id NOT GLOB '*[^0-9a-f]*'),
 key TEXT NOT NULL UNIQUE CHECK(length(key) BETWEEN 1 AND 120 AND key NOT GLOB '*[^a-z0-9_-]*'),
 kind TEXT NOT NULL CHECK(kind IN ('article','lesson','doc','page')),
 source_locale TEXT NOT NULL REFERENCES locale_packs(code),
 created_at TEXT NOT NULL DEFAULT(strftime('%Y-%m-%dT%H:%M:%fZ','now')) CHECK(created_at IS NULL OR (length(created_at)=24 AND substr(created_at,-1)='Z' AND strftime('%Y-%m-%dT%H:%M:%fZ',created_at) IS NOT NULL AND strftime('%Y-%m-%dT%H:%M:%fZ',created_at)=created_at)),
 updated_at TEXT NOT NULL DEFAULT(strftime('%Y-%m-%dT%H:%M:%fZ','now')) CHECK(updated_at IS NULL OR (length(updated_at)=24 AND substr(updated_at,-1)='Z' AND strftime('%Y-%m-%dT%H:%M:%fZ',updated_at) IS NOT NULL AND strftime('%Y-%m-%dT%H:%M:%fZ',updated_at)=updated_at))
) STRICT;
INSERT INTO content_items_next(id,key,kind,source_locale,created_at,updated_at) SELECT id,key,kind,source_locale,created_at,updated_at FROM content_items;
DROP TABLE content_items;
ALTER TABLE content_items_next RENAME TO content_items;
CREATE INDEX content_published_lookup ON content_versions(locale,published_revision_id) WHERE published_revision_id IS NOT NULL;
CREATE TRIGGER stable_content_slug BEFORE UPDATE OF slug ON content_versions WHEN OLD.first_published_at IS NOT NULL AND NEW.slug!=OLD.slug BEGIN SELECT RAISE(ABORT,'published slug is stable'); END;
CREATE TRIGGER stable_content_version_owner BEFORE UPDATE OF item_id,locale ON content_versions WHEN NEW.item_id!=OLD.item_id OR NEW.locale!=OLD.locale BEGIN SELECT RAISE(ABORT,'immutable version ownership'); END;
CREATE TRIGGER published_membership_update BEFORE UPDATE OF collection_id,item_id ON collection_items
 WHEN (NEW.collection_id!=OLD.collection_id OR NEW.item_id!=OLD.item_id) AND EXISTS(SELECT 1 FROM content_versions WHERE item_id=OLD.item_id AND first_published_at IS NOT NULL)
 BEGIN SELECT RAISE(ABORT,'published membership is stable'); END;
CREATE TRIGGER stable_collection_slug BEFORE UPDATE OF slug ON collection_locales
 WHEN NEW.slug!=OLD.slug AND EXISTS(SELECT 1 FROM collection_items m JOIN content_versions v ON v.item_id=m.item_id WHERE m.collection_id=OLD.collection_id AND v.locale=OLD.locale AND v.first_published_at IS NOT NULL)
 BEGIN SELECT RAISE(ABORT,'published collection route is stable'); END;
CREATE TRIGGER immutable_quiz_binding BEFORE UPDATE ON quiz_bindings BEGIN SELECT RAISE(ABORT,'immutable quiz binding'); END;
CREATE TRIGGER published_membership_delete BEFORE DELETE ON collection_items
 WHEN EXISTS(SELECT 1 FROM content_versions WHERE item_id=OLD.item_id AND first_published_at IS NOT NULL)
 AND (NOT EXISTS(SELECT 1 FROM content_type_conversion_guard WHERE item_id=OLD.item_id)
 OR EXISTS(SELECT 1 FROM collection_plan_members m JOIN collection_plan_state st ON m.plan_id=st.active_plan_id OR m.plan_id=st.draft_plan_id WHERE m.item_id=OLD.item_id))
 BEGIN SELECT RAISE(ABORT,'published membership is stable'); END;
CREATE TRIGGER removed_collection_visibility BEFORE UPDATE OF visible ON collection_locales
 WHEN NEW.visible=1 AND EXISTS(SELECT 1 FROM collections WHERE id=NEW.collection_id AND removed=1)
 BEGIN SELECT RAISE(ABORT,'collection removed'); END;
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
