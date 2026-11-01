-- Durable achievements outlive the bounded attempt ledger; deletion of a browser
-- identity removes both. Evidence is immutable revision/assessment IDs, not answers.
CREATE TABLE learning_completion (
 identity_id TEXT NOT NULL REFERENCES learning_identities(id) ON DELETE CASCADE,
 item_id TEXT NOT NULL REFERENCES content_items(id),
 collection_id TEXT REFERENCES collections(id),
 epoch INTEGER NOT NULL CHECK(epoch>=1),
 method TEXT NOT NULL CHECK(method IN ('quiz_pass','read')),
 assessment_id TEXT REFERENCES assessments(id),
 revision_id TEXT NOT NULL REFERENCES revisions(id),
 created_at TEXT NOT NULL,
 PRIMARY KEY(identity_id,item_id,epoch),
 CHECK((method='quiz_pass' AND assessment_id IS NOT NULL) OR (method='read' AND assessment_id IS NULL))
) STRICT;
CREATE INDEX learning_completion_path ON learning_completion(identity_id,collection_id,epoch);
