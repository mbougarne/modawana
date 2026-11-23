package httpserver

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/mbougarne/modawana/backend/internal/webhook"
)

func (d *Domain) webhookRoutes(w http.ResponseWriter, r *http.Request) bool {
	const prefix = "/api/v1/webhooks/"
	if r.URL.Path == "/api/v1/webhook-receipts" {
		d.webhookReceipts(w, r)
		return true
	}
	if !strings.HasPrefix(r.URL.Path, prefix) {
		return false
	}
	adapter, connection, ok := strings.Cut(strings.TrimPrefix(r.URL.Path, prefix), "/")
	if !ok || !d.Webhooks.HasConnection(adapter, connection) {
		writeError(w, NotFound)
		return true
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeError(w, MethodNotAllowed)
		return true
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" || r.Header.Get("Content-Encoding") != "" {
		writeError(w, UnsupportedMediaType)
		return true
	}
	defer r.Body.Close()
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, webhook.MaxBody))
	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &tooLarge):
		writeError(w, TooLarge)
		return true
	case err != nil:
		writeError(w, BadRequest)
		return true
	}
	duplicate, err := d.Webhooks.Accept(r.Context(), adapter, connection, r.Header, body)
	switch {
	case errors.Is(err, webhook.ErrSignature):
		writeError(w, Unauthenticated)
	case errors.Is(err, webhook.ErrScope):
		writeError(w, Forbidden)
	case errors.Is(err, webhook.ErrInvalid):
		writeError(w, InvalidFields)
	case errors.Is(err, webhook.ErrUnknown):
		writeError(w, NotFound)
	case errors.Is(err, webhook.ErrConflict):
		writeError(w, IdempotencyConflict)
	case err != nil:
		w.Header().Set("Retry-After", "5")
		writeError(w, Unavailable)
	case duplicate:
		w.WriteHeader(http.StatusOK)
	default:
		w.WriteHeader(http.StatusAccepted)
	}
	return true
}

func (d *Domain) webhookReceipts(w http.ResponseWriter, r *http.Request) {
	if _, err := d.Auth.OwnerRequest(r); err != nil {
		domainError(w, err)
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, MethodNotAllowed)
		return
	}
	var before int64
	if raw := r.URL.Query().Get("before"); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || n < 1 {
			writeError(w, InvalidFields)
			return
		}
		before = n
	}
	page, err := d.Webhooks.List(r.Context(), before)
	if err != nil {
		domainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}
