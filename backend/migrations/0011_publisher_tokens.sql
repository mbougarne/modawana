ALTER TABLE api_tokens ADD COLUMN allow_articles INTEGER NOT NULL DEFAULT 0 CHECK(allow_articles IN (0,1));
CREATE TABLE publisher_media (
 token_id TEXT NOT NULL REFERENCES api_tokens(id) ON DELETE CASCADE,
 media_id TEXT NOT NULL UNIQUE REFERENCES media(id) ON DELETE CASCADE,
 PRIMARY KEY(token_id,media_id)
) STRICT;
