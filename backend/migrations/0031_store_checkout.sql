ALTER TABLE store_variants ADD COLUMN reserved INTEGER NOT NULL DEFAULT 0 CHECK(reserved BETWEEN 0 AND stock);

CREATE TABLE store_checkout_settings (
 id INTEGER PRIMARY KEY CHECK(id=1),
 version INTEGER NOT NULL CHECK(version>0),
 enabled INTEGER NOT NULL CHECK(enabled IN (0,1)),
 zones TEXT NOT NULL CHECK(json_valid(zones)),
 coupons TEXT NOT NULL CHECK(json_valid(coupons))
) STRICT;
INSERT INTO store_checkout_settings VALUES(1,1,0,'[]','[]');

CREATE TABLE store_orders (
 id TEXT PRIMARY KEY,
 guest_hash TEXT NOT NULL,
 retry_key TEXT NOT NULL,
 fingerprint TEXT NOT NULL,
 locale TEXT NOT NULL,
 customer TEXT NOT NULL CHECK(json_valid(customer)),
 snapshot TEXT NOT NULL CHECK(json_valid(snapshot)),
 status TEXT NOT NULL CHECK(status IN ('awaiting_payment','confirmed','expired','review')),
 refund_state TEXT NOT NULL DEFAULT 'none' CHECK(refund_state IN ('none','pending','partial','refunded')),
 fulfillment_state TEXT NOT NULL DEFAULT 'unfulfilled' CHECK(fulfillment_state IN ('unfulfilled','shipped','cancelled')),
 reservation_state TEXT NOT NULL CHECK(reservation_state IN ('held','sold','released')),
 created_at INTEGER NOT NULL,
 expires_at INTEGER NOT NULL CHECK(expires_at>created_at),
 UNIQUE(guest_hash,retry_key)
) STRICT;
CREATE INDEX store_order_expiry ON store_orders(reservation_state,expires_at,id);
CREATE INDEX store_order_list ON store_orders(created_at DESC,id DESC);
CREATE INDEX store_guest_reservations ON store_orders(guest_hash,reservation_state);

CREATE TABLE store_order_lines (
 order_id TEXT NOT NULL REFERENCES store_orders(id),
 sku TEXT NOT NULL REFERENCES store_variants(sku),
 quantity INTEGER NOT NULL CHECK(quantity BETWEEN 1 AND 99),
 PRIMARY KEY(order_id,sku)
) STRICT, WITHOUT ROWID;

CREATE TABLE store_payment_operations (
 id TEXT PRIMARY KEY,
 order_id TEXT NOT NULL UNIQUE REFERENCES store_orders(id),
 amount_minor INTEGER NOT NULL CHECK(amount_minor>=0),
 currency TEXT NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('pending','uncertain','paid','released','late_paid')),
 connection TEXT,
 provider_order TEXT,
 capture_id TEXT,
 UNIQUE(connection,provider_order),
 UNIQUE(connection,capture_id),
 CHECK((connection IS NULL) = (provider_order IS NULL)),
 CHECK((state IN ('uncertain','paid','late_paid') AND connection IS NOT NULL) OR state IN ('pending','released')),
 CHECK((state IN ('paid','late_paid')) = (capture_id IS NOT NULL))
) STRICT;

CREATE TABLE store_order_events (
 id INTEGER PRIMARY KEY,
 order_id TEXT NOT NULL REFERENCES store_orders(id),
 kind TEXT NOT NULL CHECK(kind IN ('reserved','capture_pending','expired_unpaid','verified_unpaid','paid','paid_after_release')),
 created_at INTEGER NOT NULL
) STRICT;
CREATE INDEX store_order_events_history ON store_order_events(order_id,id);

CREATE TABLE store_checkout_rate (
 scope TEXT NOT NULL,
 bucket TEXT NOT NULL,
 window INTEGER NOT NULL,
 attempts INTEGER NOT NULL,
 PRIMARY KEY(scope,bucket,window)
) STRICT, WITHOUT ROWID;
CREATE INDEX store_checkout_rate_window ON store_checkout_rate(window);
