CREATE TABLE plugin_installs (
 id TEXT PRIMARY KEY, slug TEXT NOT NULL UNIQUE,
 name TEXT NOT NULL, plugin_version TEXT NOT NULL,
 api_version INTEGER NOT NULL CHECK(api_version>0),
 manifest TEXT NOT NULL, files TEXT NOT NULL,
 enabled INTEGER NOT NULL DEFAULT 0 CHECK(enabled IN (0,1)),
 version INTEGER NOT NULL DEFAULT 1 CHECK(version>0),
 installed_at TEXT NOT NULL, updated_at TEXT NOT NULL
) STRICT;
CREATE TABLE plugin_kv (
 plugin_id TEXT NOT NULL REFERENCES plugin_installs(id) ON DELETE CASCADE,
 key TEXT NOT NULL CHECK(length(key) BETWEEN 1 AND 128),
 value BLOB NOT NULL,
 updated_at TEXT NOT NULL,
 PRIMARY KEY (plugin_id, key)
) STRICT;
CREATE TABLE plugin_documents (
 plugin_id TEXT NOT NULL REFERENCES plugin_installs(id) ON DELETE CASCADE,
 collection TEXT NOT NULL CHECK(length(collection) BETWEEN 1 AND 64),
 doc_id TEXT NOT NULL CHECK(length(doc_id) BETWEEN 1 AND 128),
 body TEXT NOT NULL,
 updated_at TEXT NOT NULL,
 PRIMARY KEY (plugin_id, collection, doc_id)
) STRICT;
