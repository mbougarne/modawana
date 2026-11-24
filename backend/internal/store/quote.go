package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"
)

type CartItem struct {
	SKU      string `json:"sku"`
	Quantity int64  `json:"quantity"`
}

type QuoteInput struct {
	Locale  string     `json:"locale"`
	Country string     `json:"country"`
	Coupon  string     `json:"coupon"`
	Items   []CartItem `json:"items"`
}

type OrderLine struct {
	CartItem
	Title     string `json:"title"`
	Variant   string `json:"variant"`
	UnitMinor int64  `json:"unit_minor"`
	LineMinor int64  `json:"line_minor"`
}

type Snapshot struct {
	Currency      string      `json:"currency"`
	Digits        int         `json:"digits"`
	Country       string      `json:"country"`
	Lines         []OrderLine `json:"lines"`
	ShippingZone  string      `json:"shipping_zone"`
	Coupon        string      `json:"coupon"`
	TaxBPS        int64       `json:"tax_bps"`
	TaxShipping   bool        `json:"tax_shipping"`
	SubtotalMinor int64       `json:"subtotal_minor"`
	DiscountMinor int64       `json:"discount_minor"`
	ShippingMinor int64       `json:"shipping_minor"`
	TaxMinor      int64       `json:"tax_minor"`
	TotalMinor    int64       `json:"total_minor"`
}

type Quote struct {
	Snapshot
	Fingerprint string `json:"fingerprint"`
}

func validCart(in QuoteInput) bool {
	if len(in.Items) == 0 || len(in.Items) > 20 || !isoCountry(in.Country) ||
		(in.Coupon != "" && !couponPattern.MatchString(strings.ToUpper(strings.TrimSpace(in.Coupon)))) {
		return false
	}
	seen := map[string]bool{}
	for _, item := range in.Items {
		if !skuPattern.MatchString(item.SKU) || item.Quantity < 1 || item.Quantity > 99 || seen[item.SKU] {
			return false
		}
		seen[item.SKU] = true
	}
	return true
}

func quoteCart(ctx context.Context, tx *sql.Tx, in QuoteInput) (Quote, error) {
	var out Quote
	if !validCart(in) {
		return out, ErrInvalid
	}
	base, settings, err := checkout(ctx, tx, in.Locale)
	if err != nil {
		return out, err
	}
	out.Currency, out.Digits, out.Country = base.Currency, base.Digits, in.Country
	for _, zone := range settings.Zones {
		if slices.Contains(zone.Countries, in.Country) {
			out.ShippingZone, out.ShippingMinor, out.TaxBPS, out.TaxShipping = zone.Name, zone.RateMinor, zone.TaxBPS, zone.TaxShipping
			break
		}
	}
	if out.ShippingZone == "" {
		return out, ErrShipping
	}
	if out.Lines, out.SubtotalMinor, err = cartLines(ctx, tx, in); err != nil {
		return out, err
	}
	if in.Coupon != "" {
		out.Coupon = strings.ToUpper(strings.TrimSpace(in.Coupon))
		index := slices.IndexFunc(settings.Coupons, func(c Coupon) bool { return c.Code == out.Coupon })
		if index < 0 {
			return out, ErrCoupon
		}
		coupon := settings.Coupons[index]
		out.DiscountMinor = min(out.SubtotalMinor, coupon.DiscountMinor+percentage(out.SubtotalMinor, coupon.DiscountBPS))
	}
	taxBase := out.SubtotalMinor - out.DiscountMinor
	if out.TaxShipping {
		taxBase += out.ShippingMinor
	}
	out.TaxMinor = percentage(taxBase, out.TaxBPS)
	out.TotalMinor = out.SubtotalMinor - out.DiscountMinor + out.ShippingMinor + out.TaxMinor
	if out.SubtotalMinor > maxCharge || out.TotalMinor > maxCharge {
		return Quote{}, ErrInvalid
	}
	raw, err := json.Marshal(out.Snapshot)
	out.Fingerprint = hash(raw)
	return out, err
}

// Amounts are bounded by 20 lines of 99 units and maxCharge per unit.
// Applying basis points in two steps avoids overflow at the cart limit.
func percentage(amount, bps int64) int64 {
	return amount/10000*bps + (amount%10000*bps+5000)/10000
}

func cartLines(ctx context.Context, tx *sql.Tx, in QuoteInput) ([]OrderLine, int64, error) {
	items := slices.Clone(in.Items)
	slices.SortFunc(items, func(a, b CartItem) int { return strings.Compare(a.SKU, b.SKU) })
	lines := make([]OrderLine, 0, len(items))
	var subtotal int64
	for _, item := range items {
		line := OrderLine{CartItem: item}
		var available int64
		err := tx.QueryRowContext(ctx, `SELECT p.title,coalesce(json_extract(v.labels,?),v.sku),v.price_minor,v.stock-v.reserved
			FROM store_variants v JOIN store_publications p ON p.product=v.product
			WHERE v.sku=? AND v.active=1 AND p.locale=?`, "$."+in.Locale, item.SKU, in.Locale).
			Scan(&line.Title, &line.Variant, &line.UnitMinor, &available)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && available < item.Quantity) {
			return nil, 0, ErrStock
		}
		if err != nil {
			return nil, 0, err
		}
		line.LineMinor = line.UnitMinor * item.Quantity
		subtotal += line.LineMinor
		lines = append(lines, line)
	}
	return lines, subtotal, nil
}

func (s *Service) Quote(ctx context.Context, in QuoteInput) (Quote, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Quote{}, err
	}
	defer tx.Rollback()
	return quoteCart(ctx, tx, in)
}
