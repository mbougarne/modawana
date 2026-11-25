CREATE TABLE store_paypal_recovery (
 id INTEGER PRIMARY KEY CHECK(id=1),
 snapshot_started_at INTEGER NOT NULL DEFAULT 0,
 target_at INTEGER NOT NULL DEFAULT 0
) STRICT;
INSERT INTO store_paypal_recovery(id,target_at)
 SELECT 1,CASE WHEN payment_review_required THEN unixepoch() ELSE 0 END FROM store_checkout_settings WHERE id=1;

CREATE TABLE store_paypal_recovery_coverage (
 connection TEXT PRIMARY KEY REFERENCES store_paypal_connections(id),
 start_at INTEGER NOT NULL,
 through_at INTEGER NOT NULL CHECK(through_at>=start_at),
 note TEXT NOT NULL DEFAULT '',
 actor TEXT NOT NULL DEFAULT ''
) STRICT;

ALTER TABLE store_paypal_receipts ADD COLUMN recovered INTEGER NOT NULL DEFAULT 0 CHECK(recovered IN (0,1));
ALTER TABLE store_paypal_receipts ADD COLUMN review_note TEXT NOT NULL DEFAULT '';
ALTER TABLE store_paypal_receipts ADD COLUMN reviewed_by TEXT NOT NULL DEFAULT '';
ALTER TABLE store_paypal_receipts ADD COLUMN reviewed_at INTEGER NOT NULL DEFAULT 0;
ALTER TABLE store_paypal_receipts ADD COLUMN refund_hold_capture TEXT NOT NULL DEFAULT '';
CREATE INDEX store_paypal_receipt_refund_hold ON store_paypal_receipts(connection,refund_hold_capture)
 WHERE refund_hold_capture<>'';
