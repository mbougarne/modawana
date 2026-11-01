-- Metadata only. Markdown and upload bytes live in external generated files.
CREATE TABLE owner (
 id TEXT PRIMARY KEY NOT NULL CHECK(length(id)=32 AND id NOT GLOB '*[^0-9a-f]*'),
 singleton INTEGER NOT NULL DEFAULT 1 UNIQUE CHECK(singleton=1),
 password_hash TEXT,
 auth_version INTEGER NOT NULL DEFAULT 1 CHECK(auth_version>=1),
 created_at TEXT NOT NULL DEFAULT(strftime('%Y-%m-%dT%H:%M:%fZ','now')) CHECK(created_at IS NULL OR (length(created_at)=24 AND substr(created_at,-1)='Z' AND strftime('%Y-%m-%dT%H:%M:%fZ',created_at) IS NOT NULL AND strftime('%Y-%m-%dT%H:%M:%fZ',created_at)=created_at))
) STRICT;

CREATE TABLE sessions (
 id TEXT PRIMARY KEY NOT NULL CHECK(length(id)=32 AND id NOT GLOB '*[^0-9a-f]*'),
 owner_id TEXT NOT NULL REFERENCES owner(id),
 token_hash TEXT NOT NULL UNIQUE CHECK(length(token_hash)=64 AND token_hash NOT GLOB '*[^0-9a-f]*'),
 kind TEXT NOT NULL CHECK(kind IN ('owner','restricted_enrollment')),
 auth_method TEXT NOT NULL CHECK(auth_method IN ('passkey','password_totp','recovery')),
 auth_version INTEGER NOT NULL CHECK(auth_version>=1),
 authenticated_at TEXT NOT NULL CHECK(authenticated_at IS NULL OR (length(authenticated_at)=24 AND substr(authenticated_at,-1)='Z' AND strftime('%Y-%m-%dT%H:%M:%fZ',authenticated_at) IS NOT NULL AND strftime('%Y-%m-%dT%H:%M:%fZ',authenticated_at)=authenticated_at)),
 created_at TEXT NOT NULL DEFAULT(strftime('%Y-%m-%dT%H:%M:%fZ','now')) CHECK(created_at IS NULL OR (length(created_at)=24 AND substr(created_at,-1)='Z' AND strftime('%Y-%m-%dT%H:%M:%fZ',created_at) IS NOT NULL AND strftime('%Y-%m-%dT%H:%M:%fZ',created_at)=created_at)),
 expires_at TEXT NOT NULL CHECK(expires_at IS NULL OR (length(expires_at)=24 AND substr(expires_at,-1)='Z' AND strftime('%Y-%m-%dT%H:%M:%fZ',expires_at) IS NOT NULL AND strftime('%Y-%m-%dT%H:%M:%fZ',expires_at)=expires_at)),
 revoked_at TEXT CHECK(revoked_at IS NULL OR (length(revoked_at)=24 AND substr(revoked_at,-1)='Z' AND strftime('%Y-%m-%dT%H:%M:%fZ',revoked_at) IS NOT NULL AND strftime('%Y-%m-%dT%H:%M:%fZ',revoked_at)=revoked_at)),
 CHECK(kind!='owner' OR auth_method IN ('passkey','password_totp'))
) STRICT;
CREATE INDEX sessions_owner_active ON sessions(owner_id,revoked_at,expires_at);

CREATE TABLE api_tokens (
 id TEXT PRIMARY KEY NOT NULL CHECK(length(id)=32 AND id NOT GLOB '*[^0-9a-f]*'),
 owner_id TEXT NOT NULL REFERENCES owner(id),
 token_hash TEXT NOT NULL UNIQUE CHECK(length(token_hash)=64 AND token_hash NOT GLOB '*[^0-9a-f]*'),
 label TEXT NOT NULL CHECK(length(label) BETWEEN 1 AND 120),
 created_at TEXT NOT NULL DEFAULT(strftime('%Y-%m-%dT%H:%M:%fZ','now')) CHECK(created_at IS NULL OR (length(created_at)=24 AND substr(created_at,-1)='Z' AND strftime('%Y-%m-%dT%H:%M:%fZ',created_at) IS NOT NULL AND strftime('%Y-%m-%dT%H:%M:%fZ',created_at)=created_at)),
 expires_at TEXT NOT NULL CHECK(expires_at IS NULL OR (length(expires_at)=24 AND substr(expires_at,-1)='Z' AND strftime('%Y-%m-%dT%H:%M:%fZ',expires_at) IS NOT NULL AND strftime('%Y-%m-%dT%H:%M:%fZ',expires_at)=expires_at)),
 revoked_at TEXT CHECK(revoked_at IS NULL OR (length(revoked_at)=24 AND substr(revoked_at,-1)='Z' AND strftime('%Y-%m-%dT%H:%M:%fZ',revoked_at) IS NOT NULL AND strftime('%Y-%m-%dT%H:%M:%fZ',revoked_at)=revoked_at)),
 last_used_at TEXT CHECK(last_used_at IS NULL OR (length(last_used_at)=24 AND substr(last_used_at,-1)='Z' AND strftime('%Y-%m-%dT%H:%M:%fZ',last_used_at) IS NOT NULL AND strftime('%Y-%m-%dT%H:%M:%fZ',last_used_at)=last_used_at))
) STRICT;
CREATE TABLE api_token_scopes (
 token_id TEXT NOT NULL REFERENCES api_tokens(id) ON DELETE CASCADE,
 scope TEXT NOT NULL CHECK(length(scope) BETWEEN 1 AND 80),
 PRIMARY KEY(token_id,scope)
) STRICT;

CREATE TABLE media (
 id TEXT PRIMARY KEY NOT NULL CHECK(length(id)=32 AND id NOT GLOB '*[^0-9a-f]*'),
 storage_key TEXT NOT NULL UNIQUE,
 mime_type TEXT NOT NULL CHECK(length(mime_type) BETWEEN 1 AND 120),
 byte_size INTEGER NOT NULL CHECK(byte_size>0),
 sha256 TEXT NOT NULL CHECK(length(sha256)=64 AND sha256 NOT GLOB '*[^0-9a-f]*'),
 original_filename TEXT NOT NULL CHECK(length(original_filename)<=255),
 created_at TEXT NOT NULL DEFAULT(strftime('%Y-%m-%dT%H:%M:%fZ','now')) CHECK(created_at IS NULL OR (length(created_at)=24 AND substr(created_at,-1)='Z' AND strftime('%Y-%m-%dT%H:%M:%fZ',created_at) IS NOT NULL AND strftime('%Y-%m-%dT%H:%M:%fZ',created_at)=created_at)),
 created_by TEXT NOT NULL REFERENCES owner(id),
 CHECK(length(storage_key) BETWEEN 1 AND 240 AND storage_key NOT GLOB '*[^a-z0-9/._-]*' AND substr(storage_key,1,1)!='/' AND instr(storage_key,'..')=0 AND instr(storage_key,'//')=0 AND instr(storage_key,'/./')=0 AND substr(storage_key,1,2)!='./' AND substr(storage_key,-1)!='/')
) STRICT;
CREATE TABLE categories (id TEXT PRIMARY KEY NOT NULL CHECK(length(id)=32 AND id NOT GLOB '*[^0-9a-f]*'), key TEXT NOT NULL UNIQUE CHECK(length(key) BETWEEN 1 AND 80), version INTEGER NOT NULL DEFAULT 1 CHECK(version>=1)) STRICT;
CREATE TABLE category_locales (
 category_id TEXT NOT NULL REFERENCES categories(id), locale TEXT NOT NULL CHECK(locale IN ('en','ar','fr')),
 label TEXT NOT NULL CHECK(length(label) BETWEEN 1 AND 120), slug TEXT NOT NULL CHECK(length(slug) BETWEEN 1 AND 160),
 PRIMARY KEY(category_id,locale), UNIQUE(locale,slug)
) STRICT;
CREATE TABLE tags (id TEXT PRIMARY KEY NOT NULL CHECK(length(id)=32 AND id NOT GLOB '*[^0-9a-f]*'), key TEXT NOT NULL UNIQUE CHECK(length(key) BETWEEN 1 AND 80), version INTEGER NOT NULL DEFAULT 1 CHECK(version>=1)) STRICT;
CREATE TABLE tag_locales (
 tag_id TEXT NOT NULL REFERENCES tags(id), locale TEXT NOT NULL CHECK(locale IN ('en','ar','fr')),
 label TEXT NOT NULL CHECK(length(label) BETWEEN 1 AND 120), slug TEXT NOT NULL CHECK(length(slug) BETWEEN 1 AND 160),
 PRIMARY KEY(tag_id,locale), UNIQUE(locale,slug)
) STRICT;

CREATE TABLE content_items (
 id TEXT PRIMARY KEY NOT NULL CHECK(length(id)=32 AND id NOT GLOB '*[^0-9a-f]*'),
 key TEXT NOT NULL UNIQUE CHECK(length(key) BETWEEN 1 AND 120 AND key NOT GLOB '*[^a-z0-9_-]*'),
 kind TEXT NOT NULL CHECK(kind IN ('article','lesson','doc')),
 source_locale TEXT NOT NULL CHECK(source_locale IN ('en','ar','fr')),
 created_at TEXT NOT NULL DEFAULT(strftime('%Y-%m-%dT%H:%M:%fZ','now')) CHECK(created_at IS NULL OR (length(created_at)=24 AND substr(created_at,-1)='Z' AND strftime('%Y-%m-%dT%H:%M:%fZ',created_at) IS NOT NULL AND strftime('%Y-%m-%dT%H:%M:%fZ',created_at)=created_at)),
 updated_at TEXT NOT NULL DEFAULT(strftime('%Y-%m-%dT%H:%M:%fZ','now')) CHECK(updated_at IS NULL OR (length(updated_at)=24 AND substr(updated_at,-1)='Z' AND strftime('%Y-%m-%dT%H:%M:%fZ',updated_at) IS NOT NULL AND strftime('%Y-%m-%dT%H:%M:%fZ',updated_at)=updated_at))
) STRICT;
CREATE TRIGGER immutable_content_key BEFORE UPDATE OF key ON content_items WHEN NEW.key!=OLD.key BEGIN SELECT RAISE(ABORT,'immutable content key'); END;

CREATE TABLE content_versions (
 id TEXT PRIMARY KEY NOT NULL CHECK(length(id)=32 AND id NOT GLOB '*[^0-9a-f]*'),
 item_id TEXT NOT NULL REFERENCES content_items(id),
 locale TEXT NOT NULL CHECK(locale IN ('en','ar','fr')),
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
CREATE INDEX content_published_lookup ON content_versions(locale,published_revision_id) WHERE published_revision_id IS NOT NULL;
CREATE TRIGGER stable_content_slug BEFORE UPDATE OF slug ON content_versions WHEN OLD.first_published_at IS NOT NULL AND NEW.slug!=OLD.slug BEGIN SELECT RAISE(ABORT,'published slug is stable'); END;
CREATE TRIGGER stable_content_version_owner BEFORE UPDATE OF item_id,locale ON content_versions WHEN NEW.item_id!=OLD.item_id OR NEW.locale!=OLD.locale BEGIN SELECT RAISE(ABORT,'immutable version ownership'); END;

CREATE TABLE revisions (
 id TEXT PRIMARY KEY NOT NULL CHECK(length(id)=32 AND id NOT GLOB '*[^0-9a-f]*'),
 version_id TEXT NOT NULL REFERENCES content_versions(id) DEFERRABLE INITIALLY DEFERRED,
 title TEXT NOT NULL CHECK(length(title) BETWEEN 1 AND 200),
 summary TEXT NOT NULL DEFAULT '' CHECK(length(summary)<=500),
 markdown_path TEXT NOT NULL UNIQUE,
 sha256 TEXT NOT NULL CHECK(length(sha256)=64 AND sha256 NOT GLOB '*[^0-9a-f]*'),
 byte_size INTEGER NOT NULL CHECK(byte_size>=0),
 source_revision_id TEXT REFERENCES revisions(id),
 category_id TEXT REFERENCES categories(id),
 cover_media_id TEXT REFERENCES media(id),
 created_at TEXT NOT NULL DEFAULT(strftime('%Y-%m-%dT%H:%M:%fZ','now')) CHECK(created_at IS NULL OR (length(created_at)=24 AND substr(created_at,-1)='Z' AND strftime('%Y-%m-%dT%H:%M:%fZ',created_at) IS NOT NULL AND strftime('%Y-%m-%dT%H:%M:%fZ',created_at)=created_at)),
 created_by TEXT NOT NULL REFERENCES owner(id),
 UNIQUE(version_id,id),
 CHECK(length(markdown_path) BETWEEN 4 AND 240 AND markdown_path LIKE '%.md' AND markdown_path NOT GLOB '*[^a-z0-9/._-]*' AND substr(markdown_path,1,1)!='/' AND instr(markdown_path,'..')=0 AND instr(markdown_path,'//')=0 AND instr(markdown_path,'/./')=0 AND substr(markdown_path,1,2)!='./')
) STRICT;
CREATE INDEX revisions_history ON revisions(version_id,created_at,id);
CREATE TRIGGER immutable_revision BEFORE UPDATE ON revisions BEGIN SELECT RAISE(ABORT,'immutable revision'); END;
CREATE TABLE translation_reviews (
 revision_id TEXT NOT NULL REFERENCES revisions(id),
 source_revision_id TEXT NOT NULL REFERENCES revisions(id),
 reviewed_by TEXT NOT NULL REFERENCES owner(id),
 reviewed_at TEXT NOT NULL DEFAULT(strftime('%Y-%m-%dT%H:%M:%fZ','now')) CHECK(reviewed_at IS NULL OR (length(reviewed_at)=24 AND substr(reviewed_at,-1)='Z' AND strftime('%Y-%m-%dT%H:%M:%fZ',reviewed_at) IS NOT NULL AND strftime('%Y-%m-%dT%H:%M:%fZ',reviewed_at)=reviewed_at)),
 PRIMARY KEY(revision_id,source_revision_id), CHECK(revision_id!=source_revision_id)
) STRICT;

CREATE TABLE revision_media (
 revision_id TEXT NOT NULL REFERENCES revisions(id) ON DELETE CASCADE,
 media_id TEXT NOT NULL REFERENCES media(id), PRIMARY KEY(revision_id,media_id)
) STRICT;
CREATE INDEX revision_media_usage ON revision_media(media_id,revision_id);
CREATE TABLE revision_tags (
 revision_id TEXT NOT NULL REFERENCES revisions(id) ON DELETE CASCADE,
 tag_id TEXT NOT NULL REFERENCES tags(id), PRIMARY KEY(revision_id,tag_id)
) STRICT;
CREATE INDEX revision_tags_usage ON revision_tags(tag_id,revision_id);
CREATE TABLE revision_media_presentations (
 revision_id TEXT NOT NULL, media_id TEXT NOT NULL,
 poster_media_id TEXT REFERENCES media(id), caption_track_media_id TEXT REFERENCES media(id),
 caption TEXT NOT NULL DEFAULT '' CHECK(length(caption)<=2000),
 transcript TEXT NOT NULL DEFAULT '' CHECK(length(transcript)<=20000),
 PRIMARY KEY(revision_id,media_id),
 FOREIGN KEY(revision_id,media_id) REFERENCES revision_media(revision_id,media_id) ON DELETE CASCADE
) STRICT;

CREATE TABLE collections (
 id TEXT PRIMARY KEY NOT NULL CHECK(length(id)=32 AND id NOT GLOB '*[^0-9a-f]*'),
 key TEXT NOT NULL UNIQUE CHECK(length(key) BETWEEN 1 AND 120 AND key NOT GLOB '*[^a-z0-9_-]*'),
 kind TEXT NOT NULL CHECK(kind IN ('learning','docs')),
 version INTEGER NOT NULL DEFAULT 1 CHECK(version>=1),
 position INTEGER NOT NULL DEFAULT 1 CHECK(position>=1),
 created_at TEXT NOT NULL DEFAULT(strftime('%Y-%m-%dT%H:%M:%fZ','now')) CHECK(created_at IS NULL OR (length(created_at)=24 AND substr(created_at,-1)='Z' AND strftime('%Y-%m-%dT%H:%M:%fZ',created_at) IS NOT NULL AND strftime('%Y-%m-%dT%H:%M:%fZ',created_at)=created_at))
) STRICT;
CREATE TABLE collection_locales (
 collection_id TEXT NOT NULL REFERENCES collections(id), locale TEXT NOT NULL CHECK(locale IN ('en','ar','fr')),
 slug TEXT NOT NULL CHECK(length(slug) BETWEEN 1 AND 160 AND instr(slug,'/')=0),
 title TEXT NOT NULL CHECK(length(title) BETWEEN 1 AND 200), summary TEXT NOT NULL DEFAULT '' CHECK(length(summary)<=500),
 visible INTEGER NOT NULL DEFAULT 0 CHECK(visible IN (0,1)),
 PRIMARY KEY(collection_id,locale), UNIQUE(locale,slug)
) STRICT;
CREATE TABLE collection_items (
 collection_id TEXT NOT NULL REFERENCES collections(id), item_id TEXT NOT NULL UNIQUE REFERENCES content_items(id),
 position INTEGER NOT NULL CHECK(position>=1), PRIMARY KEY(collection_id,item_id), UNIQUE(collection_id,position)
) STRICT;
CREATE TRIGGER collection_kind_insert BEFORE INSERT ON collection_items WHEN NOT EXISTS (
 SELECT 1 FROM content_items i JOIN collections c ON c.id=NEW.collection_id WHERE i.id=NEW.item_id AND ((i.kind='lesson' AND c.kind='learning') OR (i.kind='doc' AND c.kind='docs'))
) BEGIN SELECT RAISE(ABORT,'collection kind mismatch'); END;
CREATE TRIGGER collection_kind_update BEFORE UPDATE OF collection_id,item_id ON collection_items WHEN NOT EXISTS (
 SELECT 1 FROM content_items i JOIN collections c ON c.id=NEW.collection_id WHERE i.id=NEW.item_id AND ((i.kind='lesson' AND c.kind='learning') OR (i.kind='doc' AND c.kind='docs'))
) BEGIN SELECT RAISE(ABORT,'collection kind mismatch'); END;
CREATE TABLE api_token_collections (
 token_id TEXT NOT NULL REFERENCES api_tokens(id) ON DELETE CASCADE,
 collection_id TEXT NOT NULL REFERENCES collections(id), PRIMARY KEY(token_id,collection_id)
) STRICT;

-- Appendix authority: versioned configuration replaces site_settings/profile_locales.
CREATE TABLE config_documents (
 id TEXT PRIMARY KEY NOT NULL CHECK(length(id)=32 AND id NOT GLOB '*[^0-9a-f]*'),
 kind TEXT NOT NULL UNIQUE CHECK(kind IN ('site','profile','labels')),
 version INTEGER NOT NULL DEFAULT 1 CHECK(version>=1),
 applied_revision_id TEXT,
 FOREIGN KEY(id,applied_revision_id) REFERENCES config_revisions(document_id,id) DEFERRABLE INITIALLY DEFERRED
) STRICT;
CREATE TABLE config_revisions (
 id TEXT PRIMARY KEY NOT NULL CHECK(length(id)=32 AND id NOT GLOB '*[^0-9a-f]*'),
 document_id TEXT NOT NULL REFERENCES config_documents(id),
 snapshot_json TEXT NOT NULL CHECK(length(CAST(snapshot_json AS BLOB))<=262144 AND json_valid(snapshot_json) AND json_type(snapshot_json)='object'),
 schema_version INTEGER NOT NULL CHECK(schema_version>=1),
 colour_algorithm_version INTEGER NOT NULL CHECK(colour_algorithm_version>=1),
 created_by TEXT NOT NULL REFERENCES owner(id),
 created_at TEXT NOT NULL DEFAULT(strftime('%Y-%m-%dT%H:%M:%fZ','now')) CHECK(created_at IS NULL OR (length(created_at)=24 AND substr(created_at,-1)='Z' AND strftime('%Y-%m-%dT%H:%M:%fZ',created_at) IS NOT NULL AND strftime('%Y-%m-%dT%H:%M:%fZ',created_at)=created_at)),
 UNIQUE(document_id,id)
) STRICT;
CREATE TRIGGER immutable_config_revision BEFORE UPDATE ON config_revisions BEGIN SELECT RAISE(ABORT,'immutable configuration revision'); END;
CREATE TABLE config_revision_media (
 revision_id TEXT NOT NULL REFERENCES config_revisions(id) ON DELETE CASCADE,
 media_id TEXT NOT NULL REFERENCES media(id), role TEXT NOT NULL CHECK(length(role) BETWEEN 1 AND 80),
 PRIMARY KEY(revision_id,media_id,role)
) STRICT;
CREATE INDEX config_media_usage ON config_revision_media(media_id,revision_id);

CREATE TABLE idempotency_records (
 actor_id TEXT NOT NULL CHECK(length(actor_id) BETWEEN 1 AND 160),
 operation TEXT NOT NULL CHECK(length(operation) BETWEEN 1 AND 80),
 key_hash TEXT NOT NULL CHECK(length(key_hash)=64 AND key_hash NOT GLOB '*[^0-9a-f]*'),
 request_hash TEXT NOT NULL CHECK(length(request_hash)=64 AND request_hash NOT GLOB '*[^0-9a-f]*'),
 http_status INTEGER NOT NULL CHECK(http_status BETWEEN 100 AND 599),
 result_id TEXT CHECK(result_id IS NULL OR length(result_id) BETWEEN 1 AND 160),
 result_version INTEGER CHECK(result_version>=1),
 created_at TEXT NOT NULL DEFAULT(strftime('%Y-%m-%dT%H:%M:%fZ','now')) CHECK(created_at IS NULL OR (length(created_at)=24 AND substr(created_at,-1)='Z' AND strftime('%Y-%m-%dT%H:%M:%fZ',created_at) IS NOT NULL AND strftime('%Y-%m-%dT%H:%M:%fZ',created_at)=created_at)),
 expires_at TEXT NOT NULL CHECK(expires_at IS NULL OR (length(expires_at)=24 AND substr(expires_at,-1)='Z' AND strftime('%Y-%m-%dT%H:%M:%fZ',expires_at) IS NOT NULL AND strftime('%Y-%m-%dT%H:%M:%fZ',expires_at)=expires_at)),
 PRIMARY KEY(actor_id,operation,key_hash)
) STRICT;
CREATE INDEX idempotency_expiry ON idempotency_records(expires_at);

CREATE TRIGGER immutable_config_kind BEFORE UPDATE OF kind ON config_documents WHEN NEW.kind!=OLD.kind BEGIN SELECT RAISE(ABORT,'immutable configuration kind'); END;
CREATE TRIGGER published_membership_update BEFORE UPDATE OF collection_id,item_id ON collection_items
 WHEN (NEW.collection_id!=OLD.collection_id OR NEW.item_id!=OLD.item_id) AND EXISTS(SELECT 1 FROM content_versions WHERE item_id=OLD.item_id AND first_published_at IS NOT NULL)
 BEGIN SELECT RAISE(ABORT,'published membership is stable'); END;
CREATE TRIGGER published_membership_delete BEFORE DELETE ON collection_items
 WHEN EXISTS(SELECT 1 FROM content_versions WHERE item_id=OLD.item_id AND first_published_at IS NOT NULL)
 BEGIN SELECT RAISE(ABORT,'published membership is stable'); END;
CREATE TRIGGER collection_kind_parent_update BEFORE UPDATE OF kind ON collections
 WHEN NEW.kind!=OLD.kind AND EXISTS(SELECT 1 FROM collection_items WHERE collection_id=OLD.id)
 BEGIN SELECT RAISE(ABORT,'collection kind has members'); END;
CREATE TRIGGER content_kind_member_update BEFORE UPDATE OF kind ON content_items
 WHEN NEW.kind!=OLD.kind AND EXISTS(SELECT 1 FROM collection_items WHERE item_id=OLD.id)
 BEGIN SELECT RAISE(ABORT,'content kind has collection membership'); END;
