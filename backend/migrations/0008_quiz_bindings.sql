CREATE TABLE assessments (
 id TEXT PRIMARY KEY NOT NULL CHECK(length(id)=32),
 item_id TEXT NOT NULL REFERENCES content_items(id),
 canonical_hash TEXT NOT NULL CHECK(length(canonical_hash)=64),
 definition_json TEXT NOT NULL CHECK(json_valid(definition_json) AND length(CAST(definition_json AS BLOB))<=65536),
 created_at TEXT NOT NULL DEFAULT(strftime('%Y-%m-%dT%H:%M:%fZ','now')),
 UNIQUE(item_id,canonical_hash), UNIQUE(item_id,id)
) STRICT;
CREATE TABLE quiz_bindings (
 revision_id TEXT PRIMARY KEY NOT NULL REFERENCES revisions(id),
 assessment_id TEXT NOT NULL REFERENCES assessments(id),
 locale TEXT NOT NULL CHECK(locale IN ('en','ar','fr')),
 wording_json TEXT NOT NULL CHECK(json_valid(wording_json) AND length(CAST(wording_json AS BLOB))<=262144)
) STRICT;
CREATE TABLE assessment_reviews (
 assessment_id TEXT PRIMARY KEY NOT NULL REFERENCES assessments(id),
 reviewed_by TEXT NOT NULL REFERENCES owner(id),
 reviewed_at TEXT NOT NULL DEFAULT(strftime('%Y-%m-%dT%H:%M:%fZ','now'))
) STRICT;
CREATE TABLE quiz_translation_reviews (
 revision_id TEXT PRIMARY KEY NOT NULL REFERENCES quiz_bindings(revision_id),
 assessment_id TEXT NOT NULL REFERENCES assessments(id),
 reviewed_by TEXT NOT NULL REFERENCES owner(id),
 reviewed_at TEXT NOT NULL DEFAULT(strftime('%Y-%m-%dT%H:%M:%fZ','now'))
) STRICT;
CREATE TABLE collection_plan_gates (
 plan_id TEXT NOT NULL REFERENCES collection_plans(id),
 item_id TEXT NOT NULL REFERENCES content_items(id),
 assessment_id TEXT NOT NULL REFERENCES assessments(id),
 PRIMARY KEY(plan_id,item_id),
 FOREIGN KEY(plan_id,item_id) REFERENCES collection_plan_members(plan_id,item_id),
 FOREIGN KEY(item_id,assessment_id) REFERENCES assessments(item_id,id)
) STRICT;
CREATE TRIGGER immutable_assessment BEFORE UPDATE ON assessments BEGIN SELECT RAISE(ABORT,'immutable assessment'); END;
CREATE TRIGGER immutable_quiz_binding BEFORE UPDATE ON quiz_bindings BEGIN SELECT RAISE(ABORT,'immutable quiz binding'); END;
CREATE TRIGGER immutable_assessment_review BEFORE UPDATE ON assessment_reviews BEGIN SELECT RAISE(ABORT,'immutable assessment review'); END;
CREATE TRIGGER immutable_quiz_review BEFORE UPDATE ON quiz_translation_reviews BEGIN SELECT RAISE(ABORT,'immutable quiz review'); END;
CREATE TRIGGER immutable_plan_gate BEFORE UPDATE ON collection_plan_gates BEGIN SELECT RAISE(ABORT,'immutable plan gate'); END;
