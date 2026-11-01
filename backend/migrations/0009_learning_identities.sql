CREATE TABLE learning_identities (
 id TEXT PRIMARY KEY NOT NULL CHECK(length(id)=32),
 token_hash TEXT NOT NULL UNIQUE CHECK(length(token_hash)=64),
 last_seen_at TEXT NOT NULL,
 expires_at TEXT NOT NULL,
 created_at TEXT NOT NULL
) STRICT;
CREATE INDEX learning_identity_expiry ON learning_identities(expires_at);
-- Bounded result ledger for the upcoming public attempt transaction. No raw answers.
CREATE TABLE learning_attempts (
 id TEXT PRIMARY KEY NOT NULL CHECK(length(id)=32),
 identity_id TEXT NOT NULL REFERENCES learning_identities(id) ON DELETE CASCADE,
 assessment_id TEXT NOT NULL REFERENCES assessments(id),
 revision_id TEXT NOT NULL REFERENCES revisions(id),
 path_plan_id TEXT REFERENCES collection_plans(id),
 epoch INTEGER NOT NULL DEFAULT 1 CHECK(epoch>=1),
 score INTEGER NOT NULL CHECK(score>=0 AND score<=total),
 total INTEGER NOT NULL CHECK(total BETWEEN 1 AND 20),
 passed INTEGER NOT NULL CHECK(passed IN (0,1)),
 feedback_json TEXT NOT NULL CHECK(json_valid(feedback_json) AND length(CAST(feedback_json AS BLOB))<=131072),
 idempotency_hash TEXT NOT NULL CHECK(length(idempotency_hash)=64),
 request_hash TEXT NOT NULL CHECK(length(request_hash)=64),
 created_at TEXT NOT NULL,
 UNIQUE(identity_id,idempotency_hash)
) STRICT;
CREATE INDEX learning_attempt_retention ON learning_attempts(identity_id,created_at,id);
CREATE INDEX learning_attempt_age ON learning_attempts(created_at);
CREATE TRIGGER bounded_learning_attempts AFTER INSERT ON learning_attempts BEGIN
 DELETE FROM learning_attempts WHERE id IN (
  SELECT id FROM learning_attempts WHERE identity_id=NEW.identity_id
  ORDER BY created_at DESC,id DESC LIMIT -1 OFFSET 100
 );
END;
