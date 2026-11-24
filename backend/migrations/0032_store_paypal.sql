ALTER TABLE store_checkout_settings ADD COLUMN payment_review_required INTEGER NOT NULL DEFAULT 0 CHECK(payment_review_required IN (0,1));

CREATE TABLE store_paypal_connections (
 id TEXT PRIMARY KEY,
 mode TEXT NOT NULL CHECK(mode IN ('sandbox','live')),
 merchant TEXT NOT NULL,
 client_hash TEXT NOT NULL
) STRICT;

CREATE TABLE store_paypal_approvals (
 operation TEXT PRIMARY KEY REFERENCES store_payment_operations(id),
 connection TEXT NOT NULL REFERENCES store_paypal_connections(id),
 created_at INTEGER NOT NULL,
 approval_url TEXT
) STRICT;

CREATE TABLE store_paypal_refunds (
 id TEXT PRIMARY KEY,
 order_id TEXT NOT NULL REFERENCES store_orders(id),
 retry_key TEXT NOT NULL,
 amount_minor INTEGER NOT NULL CHECK(amount_minor>0),
 connection TEXT NOT NULL REFERENCES store_paypal_connections(id),
 provider_refund TEXT,
 state TEXT NOT NULL CHECK(state IN ('pending','complete','failed')),
 created_at INTEGER NOT NULL,
 UNIQUE(order_id,retry_key),
 UNIQUE(connection,provider_refund)
) STRICT;

CREATE TABLE store_paypal_receipts (
 ordinal INTEGER PRIMARY KEY,
 connection TEXT NOT NULL REFERENCES store_paypal_connections(id),
 event_id TEXT NOT NULL,
 fingerprint TEXT NOT NULL,
 kind TEXT NOT NULL,
 resource TEXT NOT NULL,
 related_order TEXT NOT NULL,
 related_capture TEXT NOT NULL,
 state TEXT NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','applied','review')),
 attempts INTEGER NOT NULL DEFAULT 0,
 next_at INTEGER NOT NULL,
 UNIQUE(connection,event_id)
) STRICT;
CREATE INDEX store_paypal_receipts_due ON store_paypal_receipts(state,next_at,ordinal);
