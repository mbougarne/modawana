ALTER TABLE revisions ADD COLUMN reading_minutes INTEGER NOT NULL DEFAULT 0 CHECK(reading_minutes >= 0);
