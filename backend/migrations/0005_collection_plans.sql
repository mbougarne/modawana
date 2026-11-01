CREATE TABLE collection_plans (
 id TEXT PRIMARY KEY NOT NULL CHECK(length(id)=32 AND id NOT GLOB '*[^0-9a-f]*'),
 collection_id TEXT NOT NULL REFERENCES collections(id),
 created_by TEXT NOT NULL REFERENCES owner(id),
 created_at TEXT NOT NULL DEFAULT(strftime('%Y-%m-%dT%H:%M:%fZ','now')),
 gate_policy_version INTEGER NOT NULL DEFAULT 0 CHECK(gate_policy_version>=0),
 UNIQUE(collection_id,id)
) STRICT;
CREATE TABLE collection_plan_members (
 plan_id TEXT NOT NULL REFERENCES collection_plans(id),
 item_id TEXT NOT NULL REFERENCES content_items(id),
 position INTEGER NOT NULL CHECK(position>=1),
 announcements_json TEXT NOT NULL CHECK(json_valid(announcements_json) AND json_type(announcements_json)='object' AND length(CAST(announcements_json AS BLOB))<=4096),
 PRIMARY KEY(plan_id,item_id), UNIQUE(plan_id,position)
) STRICT;
CREATE TABLE collection_plan_state (
 collection_id TEXT PRIMARY KEY NOT NULL REFERENCES collections(id),
 draft_plan_id TEXT,
 active_plan_id TEXT,
 epoch INTEGER NOT NULL DEFAULT 1 CHECK(epoch>=1),
 FOREIGN KEY(collection_id,draft_plan_id) REFERENCES collection_plans(collection_id,id),
 FOREIGN KEY(collection_id,active_plan_id) REFERENCES collection_plans(collection_id,id)
) STRICT;
CREATE TRIGGER immutable_collection_plan BEFORE UPDATE ON collection_plans BEGIN SELECT RAISE(ABORT,'immutable collection plan'); END;
CREATE TRIGGER immutable_collection_plan_member BEFORE UPDATE ON collection_plan_members BEGIN SELECT RAISE(ABORT,'immutable collection plan member'); END;
CREATE TRIGGER stable_collection_slug BEFORE UPDATE OF slug ON collection_locales
 WHEN NEW.slug!=OLD.slug AND EXISTS(SELECT 1 FROM collection_items m JOIN content_versions v ON v.item_id=m.item_id WHERE m.collection_id=OLD.collection_id AND v.locale=OLD.locale AND v.first_published_at IS NOT NULL)
 BEGIN SELECT RAISE(ABORT,'published collection route is stable'); END;

INSERT INTO collection_plan_state(collection_id) SELECT id FROM collections;
