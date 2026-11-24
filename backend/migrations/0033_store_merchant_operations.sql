ALTER TABLE store_order_lines ADD COLUMN restocked INTEGER NOT NULL DEFAULT 0 CHECK(restocked BETWEEN 0 AND quantity);

CREATE TABLE store_merchant_operations (
 id TEXT PRIMARY KEY,
 order_id TEXT NOT NULL REFERENCES store_orders(id),
 actor TEXT NOT NULL,
 kind TEXT NOT NULL CHECK(kind IN ('cancelled','shipped','tracking_updated','restocked')),
 data TEXT NOT NULL CHECK(json_valid(data)),
 created_at INTEGER NOT NULL
) STRICT;
CREATE INDEX store_merchant_history ON store_merchant_operations(order_id,created_at,id);

CREATE TABLE store_shipments (
 order_id TEXT PRIMARY KEY REFERENCES store_orders(id),
 shipped_at INTEGER NOT NULL,
 carrier TEXT NOT NULL,
 tracking_number TEXT NOT NULL
) STRICT;

ALTER TABLE store_order_events RENAME TO store_order_events_v31;

CREATE TABLE store_order_events (
 id INTEGER PRIMARY KEY,
 order_id TEXT NOT NULL REFERENCES store_orders(id),
 kind TEXT NOT NULL CHECK(kind IN (
  'reserved','capture_pending','expired_unpaid','verified_unpaid','paid','paid_after_release',
  'cancelled','shipped','tracking_updated','restocked',
  'refund_requested','refund_retry','refund_pending','refund_complete','refund_failed'
 )),
 actor TEXT NOT NULL DEFAULT '',
 detail TEXT NOT NULL DEFAULT 'null' CHECK(json_valid(detail)),
 created_at INTEGER NOT NULL
) STRICT;

INSERT INTO store_order_events(id,order_id,kind,created_at)
 SELECT id,order_id,kind,created_at FROM store_order_events_v31 ORDER BY id;
DROP TABLE store_order_events_v31;
CREATE INDEX store_order_events_history ON store_order_events(order_id,created_at,id);

INSERT INTO store_order_events(order_id,kind,detail,created_at)
 SELECT order_id,'refund_requested',json_object('refund_id',id,'amount_minor',amount_minor),created_at
 FROM store_paypal_refunds ORDER BY created_at,id;

CREATE TABLE store_order_notices (
 order_id TEXT NOT NULL REFERENCES store_orders(id),
 kind TEXT NOT NULL CHECK(kind IN ('order.paid','order.shipped')),
 created_at INTEGER NOT NULL,
 PRIMARY KEY(order_id,kind)
) STRICT, WITHOUT ROWID;

INSERT INTO outbox_deliveries(id,event_id,plugin_id,plugin_slug,available_at,updated_at)
 SELECT lower(hex(randomblob(16))),id,'','store',created_at,created_at
 FROM outbox_events WHERE name='order.paid' AND contract_version=1;
