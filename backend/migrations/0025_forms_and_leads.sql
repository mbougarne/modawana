CREATE TABLE lead_forms (
 key TEXT PRIMARY KEY,
 version INTEGER NOT NULL CHECK(version>0),
 enabled INTEGER NOT NULL CHECK(enabled IN (0,1)),
 FOREIGN KEY (key,version) REFERENCES lead_form_versions(form_key,version) DEFERRABLE INITIALLY DEFERRED
) STRICT;
CREATE TABLE lead_form_versions (
 form_key TEXT NOT NULL REFERENCES lead_forms(key),
 version INTEGER NOT NULL CHECK(version>0),
 definition TEXT NOT NULL CHECK(json_valid(definition)),
 PRIMARY KEY(form_key,version)
) STRICT;
CREATE TRIGGER immutable_lead_form BEFORE UPDATE ON lead_form_versions
 BEGIN SELECT RAISE(ABORT,'immutable form revision'); END;
CREATE TABLE leads (
 id TEXT PRIMARY KEY,
 form_key TEXT NOT NULL,
 form_version INTEGER NOT NULL,
 locale TEXT NOT NULL REFERENCES locale_packs(code),
 page_key TEXT NOT NULL,
 payload TEXT NOT NULL CHECK(json_valid(payload)),
 consent TEXT NOT NULL,
 status TEXT NOT NULL DEFAULT 'new' CHECK(status IN ('new','contacted','closed')),
 tags TEXT NOT NULL DEFAULT '[]' CHECK(json_valid(tags)),
 created_at INTEGER NOT NULL,
 expires_at INTEGER NOT NULL,
 notified_at INTEGER NOT NULL DEFAULT 0,
 FOREIGN KEY(form_key,form_version) REFERENCES lead_form_versions(form_key,version)
) STRICT;
CREATE INDEX leads_inbox ON leads(created_at DESC,id DESC);
CREATE INDEX leads_expiry ON leads(expires_at);
CREATE TABLE lead_idempotency (
 key_hash TEXT PRIMARY KEY,
 fingerprint TEXT NOT NULL,
 expires_at INTEGER NOT NULL
) STRICT;
CREATE INDEX lead_idempotency_expiry ON lead_idempotency(expires_at);
CREATE TABLE lead_rate (
 bucket TEXT NOT NULL,
 window INTEGER NOT NULL,
 attempts INTEGER NOT NULL,
 PRIMARY KEY(bucket,window)
) STRICT;
CREATE INDEX lead_rate_expiry ON lead_rate(window);
