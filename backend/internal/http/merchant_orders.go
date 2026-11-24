package httpserver

import (
	"bytes"
	"database/sql"
	"html/template"
	"net/http"
	"time"

	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/locale"
	"github.com/mbougarne/modawana/backend/internal/siteconfig"
	"github.com/mbougarne/modawana/backend/internal/store"
)

func (d *Domain) orderAction(w http.ResponseWriter, r *http.Request, actor, order, action string) {
	var kind string
	switch action {
	case "cancel":
		kind = "cancelled"
	case "ship":
		kind = "shipped"
	case "tracking":
		kind = "tracking_updated"
	case "restock":
		kind = "restocked"
	default:
		writeError(w, NotFound)
		return
	}
	var input store.MerchantInput
	if DecodeJSON(w, r, &input, 4096) {
		out, err := d.Store.MerchantAction(r.Context(), actor, order, kind, input)
		checkoutReply(w, out, err)
	}
}

func (d *Domain) exportStoreOrders(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="modawana-orders.csv"`)
	if err := d.Store.ExportOrders(r.Context(), w); err != nil {
		if response, ok := w.(*response); ok && response.status == 0 {
			w.Header().Del("Content-Disposition")
			domainError(w, err)
		}
	}
}

type receiptLine struct {
	Title, Variant, SKU string
	Quantity            int64
	Amount              string
}

type receiptTotal struct {
	Label, Amount string
}

type receiptPage struct {
	Locale, Direction, Nonce, Title, ReferenceLabel, PaidLabel, PaidAt, Back, BackURL, ColumnItem, ColumnQuantity, ColumnTotal string
	Reference                                                                                                                  string
	Lines                                                                                                                      []receiptLine
	Totals                                                                                                                     []receiptTotal
}

var orderReceiptTemplate = template.Must(template.New("order-receipt").Parse(`<!doctype html>
<html lang="{{.Locale}}" dir="{{.Direction}}">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex, nofollow"><title>{{.Title}}</title>
<style nonce="{{.Nonce}}">
body{max-inline-size:48rem;margin:2rem auto;padding-inline:1rem;font:1rem/1.6 system-ui,sans-serif}
table{border-collapse:collapse;inline-size:100%}th,td{text-align:start;padding:0.5rem;border-block-end:1px solid currentColor}
dl div{display:flex;justify-content:space-between;gap:1rem}dd{margin:0}a{color:inherit}
@media print{nav{display:none}}
</style></head>
<body><main><h1>{{.Title}}</h1>
<p>{{.ReferenceLabel}} <bdi>{{.Reference}}</bdi></p><p>{{.PaidLabel}} <bdi>{{.PaidAt}}</bdi></p>
<table><thead><tr><th scope="col">{{.ColumnItem}}</th><th scope="col">{{.ColumnQuantity}}</th><th scope="col">{{.ColumnTotal}}</th></tr></thead>
<tbody>{{range .Lines}}<tr><th scope="row"><bdi>{{.Title}}</bdi> · <bdi>{{.Variant}}</bdi><br><small><bdi>{{.SKU}}</bdi></small></th><td>{{.Quantity}}</td><td><bdi>{{.Amount}}</bdi></td></tr>{{end}}</tbody></table>
<dl>{{range .Totals}}<div><dt>{{.Label}}</dt><dd><bdi>{{.Amount}}</bdi></dd></div>{{end}}</dl>
<nav><a href="{{.BackURL}}">{{.Back}}</a></nav>
</main></body></html>`))

func (d *Domain) orderReceipt(w http.ResponseWriter, r *http.Request, lang string, out store.OrderStatus, err error, back string) {
	if err != nil {
		checkoutReply(w, nil, err)
		return
	}
	if out.PaidAt == 0 || (out.Payment != "paid" && out.Payment != "late_paid") {
		checkoutReply(w, nil, sql.ErrNoRows)
		return
	}
	var site siteconfig.Public
	if d.SiteConfig != nil {
		site, err = d.SiteConfig.Public(r.Context(), lang)
		if err != nil {
			domainError(w, err)
			return
		}
	}
	label := func(key string) string { return pageLabel(site, key, lang) }
	snapshot := out.Snapshot
	page := receiptPage{
		Locale: lang, Direction: locale.Direction(lang), Title: label("order.receipt"), ReferenceLabel: label("checkout.reference"),
		PaidLabel: label("order.paidAt"), PaidAt: time.Unix(out.PaidAt, 0).UTC().Format(time.RFC3339),
		Reference: out.Reference, Back: label("order.back"), BackURL: back, ColumnItem: label("order.item"),
		ColumnQuantity: label("order.quantity"), ColumnTotal: label("checkout.total"),
	}
	money := func(amount int64) string { return displayPrice(amount, snapshot.Currency, snapshot.Digits) }
	for _, line := range snapshot.Lines {
		page.Lines = append(page.Lines, receiptLine{Title: line.Title, Variant: line.Variant, SKU: line.SKU, Quantity: line.Quantity, Amount: money(line.LineMinor)})
	}
	page.Totals = []receiptTotal{
		{label("checkout.subtotal"), money(snapshot.SubtotalMinor)},
		{label("checkout.discount"), money(snapshot.DiscountMinor)},
		{label("checkout.shipping"), money(snapshot.ShippingMinor)},
		{label("checkout.tax"), money(snapshot.TaxMinor)},
		{label("order.amountPaid"), money(snapshot.TotalMinor)},
		{label("order.refundedAmount"), money(out.RefundedMinor)},
	}
	page.Nonce, err = db.NewID()
	if err != nil {
		writeError(w, InternalError)
		return
	}
	var body bytes.Buffer
	if err = orderReceiptTemplate.Execute(&body, page); err != nil {
		writeError(w, InternalError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'nonce-"+page.Nonce+"'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body.Bytes())
}
