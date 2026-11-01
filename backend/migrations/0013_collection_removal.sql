ALTER TABLE collections ADD COLUMN removed INTEGER NOT NULL DEFAULT 0 CHECK(removed IN (0,1));
CREATE TRIGGER removed_collection_visibility BEFORE UPDATE OF visible ON collection_locales
 WHEN NEW.visible=1 AND EXISTS(SELECT 1 FROM collections WHERE id=NEW.collection_id AND removed=1)
 BEGIN SELECT RAISE(ABORT,'collection removed'); END;
CREATE TRIGGER removed_collection_activation BEFORE UPDATE OF active_plan_id ON collection_plan_state
 WHEN NEW.active_plan_id IS NOT NULL AND EXISTS(SELECT 1 FROM collections WHERE id=NEW.collection_id AND removed=1)
 BEGIN SELECT RAISE(ABORT,'collection removed'); END;
CREATE TRIGGER removed_collection_membership BEFORE INSERT ON collection_items
 WHEN EXISTS(SELECT 1 FROM collections WHERE id=NEW.collection_id AND removed=1)
 BEGIN SELECT RAISE(ABORT,'collection removed'); END;
