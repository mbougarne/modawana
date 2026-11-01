ALTER TABLE media ADD COLUMN kind TEXT NOT NULL DEFAULT 'image' CHECK(kind IN ('image','video','pdf','captions'));
ALTER TABLE media ADD COLUMN purpose TEXT NOT NULL DEFAULT 'content' CHECK(purpose IN ('content','logo','favicon','portrait','banner'));
ALTER TABLE media ADD COLUMN width INTEGER NOT NULL DEFAULT 0 CHECK(width>=0);
ALTER TABLE media ADD COLUMN height INTEGER NOT NULL DEFAULT 0 CHECK(height>=0);
