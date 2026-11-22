CREATE TABLE plugin_settings_revisions (
 plugin_id TEXT NOT NULL REFERENCES plugin_installs(id) ON DELETE CASCADE,
 version INTEGER NOT NULL CHECK(version>0),
 snapshot TEXT NOT NULL,
 PRIMARY KEY (plugin_id, version)
) STRICT;
CREATE TABLE plugin_secrets (
 plugin_id TEXT NOT NULL REFERENCES plugin_installs(id) ON DELETE CASCADE,
 name TEXT NOT NULL,
 ciphertext BLOB NOT NULL,
 PRIMARY KEY (plugin_id, name)
) STRICT;
CREATE TABLE plugin_migrations (
 plugin_id TEXT NOT NULL REFERENCES plugin_installs(id) ON DELETE CASCADE,
 position INTEGER NOT NULL CHECK(position>=0),
 name TEXT NOT NULL,
 checksum TEXT NOT NULL CHECK(length(checksum)=64),
 PRIMARY KEY (plugin_id, position),
 UNIQUE(plugin_id, name)
) STRICT;
