package httpserver

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mbougarne/modawana/backend/internal/paypal"
	"github.com/mbougarne/modawana/backend/internal/store"
	"github.com/mbougarne/modawana/backend/internal/webhook"
)

func (d *Domain) paypalRoutes(w http.ResponseWriter, r *http.Request) bool {
	const hook = "/api/v1/webhooks/paypal/"
	path := r.URL.Path
	switch {
	case strings.HasPrefix(path, hook):
		d.paypalWebhook(w, r, strings.TrimPrefix(path, hook))
		return true
	case strings.HasPrefix(path, checkoutAPI):
		parts := strings.Split(strings.TrimPrefix(path, checkoutAPI), "/")
		if len(parts) == 5 && parts[1] == "orders" && parts[3] == "paypal" {
			d.paypalGuest(w, r, parts)
			return true
		}
	case path == ownerStore+"/paypal" || strings.HasPrefix(path, ownerStore+"/paypal/"):
		d.paypalOwner(w, r, strings.TrimPrefix(path, ownerStore+"/paypal"))
		return true
	}
	return false
}

func (d *Domain) paypalWebhook(w http.ResponseWriter, r *http.Request, id string) {
	if d.Auth == nil || d.Store == nil || !d.PayPal.HasConnection(id) || r.URL.RawQuery != "" {
		writeError(w, NotFound)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeError(w, MethodNotAllowed)
		return
	}
	body, ok := readWebhookBody(w, r)
	if !ok {
		return
	}
	ip := d.Auth.Config.ThrottleIP(r.RemoteAddr, r.Header.Get("X-Forwarded-For"))
	if err := d.Store.AllowCheckout(r.Context(), "paypal-webhook", ip, 900); err != nil {
		checkoutReply(w, nil, err)
		return
	}
	err := d.PayPal.Accept(r.Context(), id, r.Header, body)
	switch {
	case err == nil:
		w.WriteHeader(http.StatusAccepted)
	case errors.Is(err, paypal.ErrSignature):
		writeError(w, Unauthenticated)
	case errors.Is(err, webhook.ErrConflict):
		writeError(w, IdempotencyConflict)
	case errors.Is(err, paypal.ErrInvalid):
		writeError(w, InvalidFields)
	default:
		w.Header().Set("Retry-After", "5")
		writeError(w, Unavailable)
	}
}

func (d *Domain) paypalGuest(w http.ResponseWriter, r *http.Request, parts []string) {
	if d.Auth == nil || d.Store == nil || !d.extensions().Installed(paypal.Slug) ||
		!pageLocale(parts[0]) || r.URL.RawQuery != "" {
		writeError(w, NotFound)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeError(w, MethodNotAllowed)
		return
	}
	guest := guestToken(r)
	if guest == "" || len(r.Header.Values("Origin")) != 1 || r.Header.Get("Origin") != d.Auth.Config.BaseURL {
		writeError(w, Forbidden)
		return
	}
	if !DecodeJSON(w, r, &struct{}{}, 1024) {
		return
	}
	ip := d.Auth.Config.ThrottleIP(r.RemoteAddr, r.Header.Get("X-Forwarded-For"))
	if err := d.Store.AllowCheckout(r.Context(), "paypal-guest", ip, 45); err != nil {
		checkoutReply(w, nil, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 18*time.Second)
	defer cancel()
	switch parts[4] {
	case "approval":
		u, err := d.PayPal.Approval(ctx, guest, parts[2])
		paypalReply(w, map[string]string{"approval_url": u}, err)
	case "finish", "check":
		out, err := d.PayPal.Finish(ctx, guest, parts[2], parts[4] == "finish")
		paypalReply(w, out, err)
	default:
		writeError(w, NotFound)
	}
}

func (d *Domain) paypalOwner(w http.ResponseWriter, r *http.Request, path string) {
	if d.Auth == nil || !d.extensions().Installed(paypal.Slug) {
		writeError(w, NotFound)
		return
	}
	if _, err := d.Auth.OwnerRequest(r); err != nil {
		domainError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 18*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	switch {
	case path == "" && r.Method == http.MethodGet:
		out, err := d.PayPal.Status(ctx)
		paypalReply(w, out, err)
	case path == "/resume" && r.Method == http.MethodPost:
		fresh, err := d.Auth.FreshOwnerCheck(r)
		if err == nil {
			err = d.Store.AcknowledgePaymentReview(ctx, fresh)
		}
		paypalReply(w, map[string]bool{"recorded": true}, err)
	case len(parts) == 3 && parts[0] == "refunds" && parts[2] == "retry" && r.Method == http.MethodPost:
		fresh, err := d.Auth.FreshOwnerCheck(r)
		if err != nil {
			domainError(w, err)
			return
		}
		out, err := d.PayPal.ResumeRefund(ctx, parts[1], fresh)
		paypalReply(w, out, err)
	case path == "/receipts" && r.Method == http.MethodGet:
		var cursor int64
		if before := r.URL.Query().Get("before"); before != "" {
			n, err := strconv.ParseInt(before, 10, 64)
			if err != nil || n < 1 {
				writeError(w, InvalidFields)
				return
			}
			cursor = n
		}
		out, err := d.PayPal.Receipts(ctx, cursor)
		paypalReply(w, out, err)
	case len(parts) == 3 && parts[0] == "receipts" && parts[2] == "retry" && r.Method == http.MethodPost:
		id, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil || id < 1 {
			writeError(w, InvalidFields)
			return
		}
		paypalReply(w, map[string]bool{"queued": true}, d.PayPal.RetryReceipt(ctx, id))
	case len(parts) == 3 && parts[0] == "orders" && parts[2] == "reconcile" && r.Method == http.MethodPost:
		paypalReply(w, map[string]bool{"checked": true}, d.PayPal.Reconcile(ctx, parts[1]))
	case len(parts) == 3 && parts[0] == "orders" && parts[2] == "refunds" && r.Method == http.MethodGet:
		out, err := d.Store.Refunds(ctx, parts[1])
		paypalReply(w, out, err)
	case len(parts) == 3 && parts[0] == "orders" && parts[2] == "refunds" && r.Method == http.MethodPost:
		var in store.RefundInput
		if DecodeJSON(w, r, &in, 2048) {
			fresh, err := d.Auth.FreshOwnerCheck(r)
			if err != nil {
				domainError(w, err)
				return
			}
			out, err := d.PayPal.Refund(ctx, parts[1], in, fresh)
			paypalReply(w, out, err)
		}
	default:
		writeError(w, MethodNotAllowed)
	}
}

func paypalReply(w http.ResponseWriter, out any, err error) {
	switch {
	case errors.Is(err, paypal.ErrInvalid):
		writeError(w, InvalidFields)
	case errors.Is(err, paypal.ErrUnavailable), errors.Is(err, context.DeadlineExceeded):
		writeError(w, Unavailable)
	case errors.Is(err, paypal.ErrReview), errors.Is(err, paypal.ErrMismatch), errors.Is(err, store.ErrPaymentEvidence):
		writeJSON(w, http.StatusConflict, ErrorEnvelope{APIError{Code: "paypal_review", Message: "Payment operation needs merchant review.", RequestID: w.Header().Get("X-Request-ID")}})
	default:
		checkoutReply(w, out, err)
	}
}
