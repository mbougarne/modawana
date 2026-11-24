CREATE TABLE store_settings (
 id INTEGER PRIMARY KEY CHECK(id=1),
 version INTEGER NOT NULL CHECK(version>0),
 enabled INTEGER NOT NULL CHECK(enabled IN (0,1)),
 currency TEXT NOT NULL CHECK(length(currency)=3)
) STRICT;
INSERT INTO store_settings VALUES(1,1,0,'USD');

CREATE TABLE store_products (
 key TEXT PRIMARY KEY
) STRICT;

CREATE TABLE store_categories (
 key TEXT NOT NULL,
 locale TEXT NOT NULL REFERENCES locale_packs(code),
 version INTEGER NOT NULL CHECK(version>0),
 title TEXT NOT NULL,
 PRIMARY KEY(key,locale)
) STRICT, WITHOUT ROWID;

CREATE TABLE store_localizations (
 product TEXT NOT NULL REFERENCES store_products(key),
 locale TEXT NOT NULL REFERENCES locale_packs(code),
 version INTEGER NOT NULL CHECK(version>0),
 slug TEXT NOT NULL,
 title TEXT NOT NULL,
 description TEXT NOT NULL,
 category TEXT,
 PRIMARY KEY(product,locale),
 UNIQUE(locale,slug),
 FOREIGN KEY(category,locale) REFERENCES store_categories(key,locale)
) STRICT, WITHOUT ROWID;

CREATE TABLE store_publications (
 product TEXT NOT NULL,
 locale TEXT NOT NULL,
 version INTEGER NOT NULL CHECK(version>0),
 slug TEXT NOT NULL,
 title TEXT NOT NULL,
 description TEXT NOT NULL,
 category TEXT,
 published_at TEXT NOT NULL,
 PRIMARY KEY(product,locale),
 UNIQUE(locale,slug),
 FOREIGN KEY(product,locale) REFERENCES store_localizations(product,locale),
 FOREIGN KEY(category,locale) REFERENCES store_categories(key,locale)
) STRICT, WITHOUT ROWID;

CREATE TABLE store_media (
 media_id TEXT PRIMARY KEY REFERENCES media(id) ON DELETE CASCADE
) STRICT;

CREATE TABLE store_draft_images (
 product TEXT NOT NULL,
 locale TEXT NOT NULL,
 position INTEGER NOT NULL CHECK(position BETWEEN 0 AND 7),
 media_id TEXT NOT NULL REFERENCES store_media(media_id),
 alt TEXT NOT NULL,
 PRIMARY KEY(product,locale,position),
 UNIQUE(product,locale,media_id),
 FOREIGN KEY(product,locale) REFERENCES store_localizations(product,locale)
) STRICT, WITHOUT ROWID;

CREATE TABLE store_published_images (
 product TEXT NOT NULL,
 locale TEXT NOT NULL,
 position INTEGER NOT NULL CHECK(position BETWEEN 0 AND 7),
 media_id TEXT NOT NULL REFERENCES store_media(media_id),
 alt TEXT NOT NULL,
 PRIMARY KEY(product,locale,position),
 UNIQUE(product,locale,media_id),
 FOREIGN KEY(product,locale) REFERENCES store_publications(product,locale) ON DELETE CASCADE
) STRICT, WITHOUT ROWID;

CREATE INDEX store_public_image_visibility ON store_published_images(media_id,locale);
CREATE INDEX store_public_categories ON store_publications(locale,category,slug);

CREATE TABLE store_variants (
 sku TEXT PRIMARY KEY,
 product TEXT NOT NULL REFERENCES store_products(key),
 version INTEGER NOT NULL CHECK(version>0),
 labels TEXT NOT NULL CHECK(json_valid(labels)),
 price_minor INTEGER NOT NULL CHECK(price_minor BETWEEN 1 AND 1000000000000),
 stock INTEGER NOT NULL DEFAULT 0 CHECK(stock BETWEEN 0 AND 1000000000),
 active INTEGER NOT NULL CHECK(active IN (0,1))
) STRICT;
CREATE INDEX store_product_variants ON store_variants(product,active,sku);

CREATE TABLE store_stock_adjustments (
 id TEXT PRIMARY KEY,
 sku TEXT NOT NULL REFERENCES store_variants(sku),
 actor TEXT NOT NULL,
 delta INTEGER NOT NULL CHECK(delta BETWEEN -1000000000 AND 1000000000 AND delta<>0),
 before_quantity INTEGER NOT NULL CHECK(before_quantity BETWEEN 0 AND 1000000000),
 after_quantity INTEGER NOT NULL CHECK(after_quantity BETWEEN 0 AND 1000000000 AND after_quantity=before_quantity+delta),
 reason TEXT NOT NULL,
 created_at TEXT NOT NULL
) STRICT;
CREATE INDEX store_adjustments_by_sku ON store_stock_adjustments(sku,created_at DESC,id DESC);
