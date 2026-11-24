package httpserver

import (
	"errors"
	"net/http"
	"strings"

	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/media"
	"github.com/mbougarne/modawana/backend/internal/store"
)

const checkoutAPI = "/api/v1/public/store/checkout/"
const checkoutCookie = "__Host-modawana_checkout"

func guestToken(r *http.Request) string {
	cookie, err := r.Cookie(checkoutCookie)
	if err != nil || !media.ID.MatchString(cookie.Value) {
		return ""
	}
	return cookie.Value
}

func (d *Domain) checkoutRoutes(w http.ResponseWriter, r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, checkoutAPI) || d.Auth == nil || !d.extensions().Installed(store.Slug) {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, checkoutAPI), "/")
	if len(parts) < 2 || len(parts) > 3 || !pageLocale(parts[0]) || r.URL.RawQuery != "" {
		writeError(w, NotFound)
		return true
	}
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	w.Header().Set("Referrer-Policy", "no-referrer")
	ctx := r.Context()
	guest := guestToken(r)
	ip := d.Auth.Config.ThrottleIP(r.RemoteAddr, r.Header.Get("X-Forwarded-For"))
	if len(parts) == 3 && (parts[1] == "orders" || parts[1] == "attempts") {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			writeError(w, MethodNotAllowed)
		} else if guest == "" {
			writeError(w, NotFound)
		} else if err := d.Store.AllowCheckout(ctx, "lookup", ip, 60); err != nil {
			checkoutReply(w, nil, err)
		} else if parts[1] == "attempts" {
			out, err := d.Store.GuestReceipt(ctx, guest, parts[2])
			checkoutReply(w, out, err)
		} else {
			out, err := d.Store.GuestOrder(ctx, guest, parts[2])
			checkoutReply(w, out, err)
		}
		return true
	}
	if len(parts) != 2 {
		writeError(w, NotFound)
		return true
	}
	switch parts[1] {
	case "options":
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			writeError(w, MethodNotAllowed)
			return true
		}
		out, err := d.Store.CheckoutOptions(ctx, parts[0])
		if err == nil && guest == "" {
			guest, err = db.NewID()
			if err == nil {
				http.SetCookie(w, &http.Cookie{Name: checkoutCookie, Value: guest, Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode, MaxAge: 30 * 24 * 3600})
			}
		}
		checkoutReply(w, out, err)
	case "quote", "orders":
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			writeError(w, MethodNotAllowed)
			return true
		}
		if len(r.Header.Values("Origin")) != 1 || r.Header.Get("Origin") != d.Auth.Config.BaseURL || guest == "" {
			writeError(w, Forbidden)
			return true
		}
		if parts[1] == "quote" {
			var in store.QuoteInput
			if DecodeJSON(w, r, &in, 8<<10) {
				if in.Locale != parts[0] {
					writeError(w, InvalidFields)
					return true
				}
				if err := d.Store.AllowCheckout(ctx, "quote", ip, 60); err != nil {
					checkoutReply(w, nil, err)
					return true
				}
				out, err := d.Store.Quote(ctx, in)
				checkoutReply(w, out, err)
			}
		} else {
			var in store.OrderInput
			if DecodeJSON(w, r, &in, 12<<10) {
				if in.Locale != parts[0] {
					writeError(w, InvalidFields)
					return true
				}
				if err := d.Store.AllowCheckout(ctx, "reserve", ip, 15); err != nil {
					checkoutReply(w, nil, err)
					return true
				}
				out, err := d.Store.CreateOrder(ctx, guest, in)
				checkoutReply(w, out, err)
			}
		}
	default:
		writeError(w, NotFound)
	}
	return true
}

func checkoutReply(w http.ResponseWriter, out any, err error) {
	switch {
	case errors.Is(err, store.ErrCheckoutClosed):
		writeError(w, NotFound)
	case errors.Is(err, store.ErrCoupon):
		writeJSON(w, http.StatusUnprocessableEntity, ErrorEnvelope{APIError{Code: "store_coupon", Message: "Coupon is unavailable.", RequestID: w.Header().Get("X-Request-ID")}})
	case errors.Is(err, store.ErrShipping):
		writeJSON(w, http.StatusUnprocessableEntity, ErrorEnvelope{APIError{Code: "store_shipping", Message: "Shipping destination is unavailable.", RequestID: w.Header().Get("X-Request-ID")}})
	case errors.Is(err, store.ErrQuoteChanged):
		writeJSON(w, http.StatusConflict, ErrorEnvelope{APIError{Code: "store_quote_changed", Message: "Get a fresh quote and review the updated totals.", RequestID: w.Header().Get("X-Request-ID")}})
	case errors.Is(err, store.ErrOrderReplay):
		writeError(w, IdempotencyConflict)
	case errors.Is(err, store.ErrLimited):
		w.Header().Set("Retry-After", "900")
		writeError(w, RateLimited)
	default:
		storeReply(w, out, err)
	}
}
