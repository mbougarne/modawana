ALTER TABLE revisions ADD COLUMN slug TEXT NOT NULL DEFAULT '' CHECK(length(slug)<=160);
CREATE TRIGGER immutable_content_kind BEFORE UPDATE OF kind,source_locale ON content_items
 WHEN NEW.kind!=OLD.kind OR NEW.source_locale!=OLD.source_locale BEGIN SELECT RAISE(ABORT,'immutable content identity'); END;
