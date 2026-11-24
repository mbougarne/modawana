package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	"golang.org/x/text/language"
)

const maxCharge = 1_000_000_000_000
const reservationLifetime = 30 * time.Minute

var (
	ErrPaymentAmount   = errors.New("checkout total cannot be paid in this currency")
	ErrCheckoutClosed  = errors.New("checkout is closed")
	ErrQuoteChanged    = errors.New("checkout quote has changed")
	ErrShipping        = errors.New("shipping destination is unavailable")
	ErrCoupon          = errors.New("coupon is unavailable")
	ErrOrderReplay     = errors.New("checkout key reused with changed input")
	ErrLimited         = errors.New("checkout rate limit reached")
	ErrPaymentEvidence = errors.New("payment evidence does not match the operation")
)

var couponPattern = regexp.MustCompile(`^[A-Z0-9][A-Z0-9_-]{0,31}$`)

type ShippingZone struct {
	Name        string   `json:"name"`
	Countries   []string `json:"countries"`
	RateMinor   int64    `json:"rate_minor"`
	TaxBPS      int64    `json:"tax_bps"`
	TaxShipping bool     `json:"tax_shipping"`
}

type Coupon struct {
	Code          string `json:"code"`
	DiscountMinor int64  `json:"discount_minor"`
	DiscountBPS   int64  `json:"discount_bps"`
}

type CheckoutSettings struct {
	PaymentReviewRequired bool           `json:"-"`
	Version               int64          `json:"version"`
	Enabled               bool           `json:"enabled"`
	Zones                 []ShippingZone `json:"zones"`
	Coupons               []Coupon       `json:"coupons"`
}

func checkoutSettings(ctx context.Context, q querier) (CheckoutSettings, error) {
	var out CheckoutSettings
	var zones, coupons string
	err := q.QueryRowContext(ctx, "SELECT version,enabled,zones,coupons,payment_review_required FROM store_checkout_settings WHERE id=1").
		Scan(&out.Version, &out.Enabled, &zones, &coupons, &out.PaymentReviewRequired)
	if err != nil {
		return out, err
	}
	if err = json.Unmarshal([]byte(zones), &out.Zones); err != nil {
		return out, err
	}
	err = json.Unmarshal([]byte(coupons), &out.Coupons)
	return out, err
}

func (s *Service) CheckoutSettings(ctx context.Context) (CheckoutSettings, error) {
	return checkoutSettings(ctx, s.DB)
}

func isoCountry(code string) bool {
	if len(code) != 2 || strings.ToUpper(code) != code {
		return false
	}
	r, err := language.ParseRegion(code)
	return err == nil && r.IsCountry() && r.String() == code && code != "ZZ"
}

func validCheckoutSettings(in CheckoutSettings) bool {
	if in.Version < 1 || len(in.Zones) > 32 || len(in.Coupons) > 64 || (in.Enabled && len(in.Zones) == 0) {
		return false
	}
	countries := map[string]bool{}
	for _, zone := range in.Zones {
		if !plain(zone.Name, 80, true, false) || len(zone.Countries) == 0 || len(zone.Countries) > 250 ||
			zone.RateMinor < 0 || zone.RateMinor > maxCharge || zone.TaxBPS < 0 || zone.TaxBPS > 10000 {
			return false
		}
		for _, code := range zone.Countries {
			if !isoCountry(code) || countries[code] {
				return false
			}
			countries[code] = true
		}
	}
	codes := map[string]bool{}
	for _, coupon := range in.Coupons {
		if !couponPattern.MatchString(coupon.Code) || codes[coupon.Code] ||
			coupon.DiscountBPS < 0 || coupon.DiscountBPS > 10000 || coupon.DiscountMinor < 0 || coupon.DiscountMinor > maxCharge ||
			((coupon.DiscountBPS == 0) == (coupon.DiscountMinor == 0)) {
			return false
		}
		codes[coupon.Code] = true
	}
	return true
}

func (s *Service) SaveCheckoutSettings(ctx context.Context, in CheckoutSettings) (CheckoutSettings, error) {
	if !validCheckoutSettings(in) {
		return CheckoutSettings{}, ErrInvalid
	}
	if in.Zones == nil {
		in.Zones = []ShippingZone{}
	}
	if in.Coupons == nil {
		in.Coupons = []Coupon{}
	}
	zones, _ := json.Marshal(in.Zones)
	coupons, _ := json.Marshal(in.Coupons)
	err := s.write(ctx, "", func(tx *sql.Tx) error {
		if in.Enabled {
			if err := checkPaymentReview(ctx, tx); err != nil {
				return err
			}
		}
		return changed(tx.ExecContext(ctx, `UPDATE store_checkout_settings SET version=version+1,enabled=?,zones=?,coupons=? WHERE id=1 AND version=?`,
			in.Enabled, string(zones), string(coupons), in.Version))
	})
	in.Version++
	return in, err
}

type CheckoutOptions struct {
	Countries []string `json:"countries"`
}

func (s *Service) checkout(ctx context.Context, tx *sql.Tx, locale string) (Settings, CheckoutSettings, error) {
	base, err := storefront(ctx, tx, locale)
	if err != nil {
		return base, CheckoutSettings{}, err
	}
	settings, err := checkoutSettings(ctx, tx)
	if err == nil && !s.checkoutOpen(settings.Enabled && !settings.PaymentReviewRequired, base.Currency) {
		err = ErrCheckoutClosed
	}
	return base, settings, err
}

func (s *Service) CheckoutOptions(ctx context.Context, locale string) (CheckoutOptions, error) {
	var out CheckoutOptions
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	_, cfg, err := s.checkout(ctx, tx, locale)
	if err != nil {
		return out, err
	}
	for _, zone := range cfg.Zones {
		out.Countries = append(out.Countries, zone.Countries...)
	}
	return out, nil
}

func hash(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func (s *Service) AllowCheckout(ctx context.Context, scope, source string, limit int) error {
	window := time.Now().Unix() / 900
	var attempts int
	err := s.DB.QueryRowContext(ctx, `INSERT INTO store_checkout_rate(scope,bucket,window,attempts) VALUES(?,?,?,1)
		ON CONFLICT(scope,bucket,window) DO UPDATE SET attempts=min(attempts+1,?)
		RETURNING attempts`, scope, hash([]byte(source)), window, limit+1).Scan(&attempts)
	if err == nil && attempts > limit {
		return ErrLimited
	}
	return err
}

func (s *Service) checkoutOpen(enabled bool, currency string) bool {
	return enabled && (s.PaymentConnection == nil || s.PaymentConnection(currency) != "")
}

func checkPaymentReview(ctx context.Context, q querier) error {
	var review bool
	err := q.QueryRowContext(ctx, `SELECT payment_review_required FROM store_checkout_settings WHERE id=1`).Scan(&review)
	if err == nil && review {
		return ErrCheckoutClosed
	}
	return err
}

func (s *Service) AcknowledgePaymentReview(ctx context.Context, fresh func(*sql.Tx) error) error {
	if fresh == nil {
		return ErrInvalid
	}
	return s.write(ctx, "", func(tx *sql.Tx) error {
		if err := fresh(tx); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE store_checkout_settings SET payment_review_required=0,version=version+1 WHERE id=1 AND payment_review_required=1`)
		return err
	})
}
