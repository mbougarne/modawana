package store

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"io"
	"strconv"
	"time"

	"github.com/mbougarne/modawana/backend/internal/csvexport"
)

func (s *Service) ExportOrders(ctx context.Context, output io.Writer) error {
	rows, err := s.DB.QueryContext(ctx, `SELECT o.id,o.created_at,o.locale,o.customer,o.snapshot,`+orderState+`,
		p.state,o.refund_state,o.fulfillment_state,p.id,coalesce(p.connection,''),coalesce(p.provider_order,''),coalesce(p.capture_id,''),
		coalesce(sh.shipped_at,0),coalesce(sh.carrier,''),coalesce(sh.tracking_number,''),
		coalesce((SELECT sum(amount_minor) FROM store_paypal_refunds WHERE order_id=o.id AND state='complete'),0),
		(SELECT json_group_object(sku,restocked) FROM store_order_lines WHERE order_id=o.id AND restocked>0),
		(SELECT json_group_array(json_object('id',id,'provider_id',coalesce(provider_refund,''),'amount_minor',amount_minor,'state',state))
		 FROM (SELECT id,provider_refund,amount_minor,state FROM store_paypal_refunds WHERE order_id=o.id ORDER BY created_at,id))
		`+orderTables+` LEFT JOIN store_shipments sh ON sh.order_id=o.id ORDER BY o.created_at DESC,o.id DESC`)
	if err != nil {
		return err
	}
	defer rows.Close()
	writer := csv.NewWriter(output)
	if err = writer.Write([]string{
		"order_reference", "created_at_utc", "locale", "order_status", "payment_status", "refund_status", "fulfillment_status",
		"payment_operation", "payment_connection", "provider_order", "provider_capture", "currency", "subtotal_minor", "discount_minor",
		"shipping_minor", "tax_minor", "total_minor", "refunded_minor", "shipped_at_utc", "carrier", "tracking_number",
		"customer_name", "customer_email", "address", "address_2", "city", "region", "postal_code", "country",
		"lines", "restocked_quantities", "refund_operations",
	}); err != nil {
		return err
	}
	for rows.Next() {
		var row MerchantOrder
		var customer, snapshot, restocked, refunds string
		var shipment Shipment
		payment := &row.PaymentRecord
		if err = rows.Scan(&row.Reference, &row.CreatedAt, &row.Locale, &customer, &snapshot, &row.Status, &row.Payment, &row.Refund, &row.Fulfillment,
			&row.Operation, &payment.Connection, &payment.ProviderOrder, &payment.Capture,
			&shipment.ShippedAt, &shipment.Carrier, &shipment.TrackingNumber, &row.RefundedMinor, &restocked, &refunds); err != nil {
			return err
		}
		if err = json.Unmarshal([]byte(customer), &row.Customer); err != nil {
			return err
		}
		if err = json.Unmarshal([]byte(snapshot), &row.Snapshot); err != nil {
			return err
		}
		lineJSON, err := json.Marshal(row.Snapshot.Lines)
		if err != nil {
			return err
		}
		buyer, totals := row.Customer, row.Snapshot
		number := func(i int64) string { return strconv.FormatInt(i, 10) }
		plain := csvexport.Literal
		if err = writer.Write([]string{
			row.Reference, exportTime(row.CreatedAt), row.Locale, row.Status, row.Payment, row.Refund, row.Fulfillment,
			row.Operation, plain(payment.Connection), plain(payment.ProviderOrder), plain(payment.Capture), totals.Currency,
			number(totals.SubtotalMinor), number(totals.DiscountMinor), number(totals.ShippingMinor), number(totals.TaxMinor),
			number(totals.TotalMinor), number(row.RefundedMinor), exportTime(shipment.ShippedAt), plain(shipment.Carrier), plain(shipment.TrackingNumber),
			plain(buyer.Name), plain(buyer.Email), plain(buyer.Address), plain(buyer.Address2), plain(buyer.City), plain(buyer.Region),
			plain(buyer.PostalCode), totals.Country, string(lineJSON), restocked, refunds,
		}); err != nil {
			return err
		}
	}
	if err = rows.Err(); err != nil {
		return err
	}
	writer.Flush()
	return writer.Error()
}

func exportTime(seconds int64) string {
	if seconds == 0 {
		return ""
	}
	return time.Unix(seconds, 0).UTC().Format(time.RFC3339)
}
