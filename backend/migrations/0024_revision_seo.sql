ALTER TABLE revisions ADD COLUMN seo TEXT NOT NULL DEFAULT 'null' CHECK(json_valid(seo));
