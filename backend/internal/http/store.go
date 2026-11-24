package httpserver

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/mbougarne/modawana/backend/internal/access"
	"github.com/mbougarne/modawana/backend/internal/auth"
	"github.com/mbougarne/modawana/backend/internal/content"
	"github.com/mbougarne/modawana/backend/internal/siteconfig"
	"github.com/mbougarne/modawana/backend/internal/store"
)

const ownerStore = "/api/v1/owner/store"
const staffStore = "/api/v1/staff/store"

func storePath(path string) bool {
	_, suffix, found := strings.Cut(strings.TrimPrefix(path, "/"), "/")
	return found && (suffix == "store" || strings.HasPrefix(suffix, "store/"))
}

func (d *Domain) resolveStore(ctx context.Context, path string) (content.PageRoute, error) {
	var page content.PageRoute
	if d.Store == nil || !d.extensions().Installed(store.Slug) {
		return page, sql.ErrNoRows
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	language := parts[0]
	var result store.PublicPage
	var err error
	switch len(parts) {
	case 4:
		if parts[2] != "checkout" || parts[3] != "cart" {
			return page, sql.ErrNoRows
		}
		visible, err := siteconfig.LocaleVisibleIn(ctx, d.Store.DB, language)
		if err != nil {
			return page, err
		}
		if !visible {
			return page, sql.ErrNoRows
		}
		return content.PageRoute{Kind: "checkout", URL: "/" + language + "/store/checkout/cart", Locale: language}, nil
	case 2:
		result, err = d.Store.Catalog(ctx, store.Filter{Locale: language, Limit: 24})
		page.Kind, page.URL = "store", "/"+language+"/store"
	case 3:
		result, err = d.Store.PublicProduct(ctx, language, parts[2])
		if err == nil {
			page.Kind, page.URL, page.Title, page.Summary = "product", result.Product.URL, result.Product.Title, result.Product.Description
		}
	default:
		return page, sql.ErrNoRows
	}
	if err != nil {
		return page, err
	}
	page.Locale, page.Store = language, &result
	for _, alt := range result.Alternatives {
		page.Alternatives = append(page.Alternatives, content.Alternative{Locale: alt.Locale, URL: alt.URL})
	}
	return page, nil
}

func (d *Domain) StoreCatalog(ctx context.Context, f store.Filter) (store.PublicPage, error) {
	if d.Store == nil || !d.extensions().Installed(store.Slug) {
		return store.PublicPage{}, sql.ErrNoRows
	}
	return d.Store.Catalog(ctx, f)
}

func (d *Domain) storeRoutes(w http.ResponseWriter, r *http.Request) bool {
	path := r.URL.Path
	prefix := ownerStore
	if path == staffStore || strings.HasPrefix(path, staffStore+"/") {
		prefix = staffStore
	}
	if (path != prefix && !strings.HasPrefix(path, prefix+"/")) || !d.extensions().Installed(store.Slug) {
		return false
	}
	if d.Auth == nil {
		writeError(w, Unauthenticated)
		return true
	}
	var owner string
	var err error
	if prefix == staffStore {
		r, owner, err = d.Auth.StaffRequest(r, auth.StaffManageCatalog)
	} else {
		owner, err = d.Auth.OwnerRequest(r)
	}
	if err != nil {
		domainError(w, err)
		return true
	}
	ctx := r.Context()
	relative := strings.TrimPrefix(path, prefix)
	parts := strings.Split(strings.TrimPrefix(relative, "/"), "/")
	q := r.URL.Query()
	switch {
	case relative == "" && r.Method == http.MethodGet:
		out, err := d.Store.Workspace(ctx, q.Get("locale"), q.Get("cursor"))
		storeReply(w, out, err)
	case relative == "/settings" && r.Method == http.MethodPut:
		if prefix != ownerStore {
			writeError(w, Forbidden)
			return true
		}
		var in store.SettingsInput
		if DecodeJSON(w, r, &in, 1024) {
			out, err := d.Store.SaveSettings(ctx, in)
			storeReply(w, out, err)
		}
	case relative == "/checkout" && r.Method == http.MethodGet:
		if prefix != ownerStore {
			writeError(w, Forbidden)
			return true
		}
		out, err := d.Store.CheckoutSettings(ctx)
		checkoutReply(w, out, err)
	case relative == "/checkout" && r.Method == http.MethodPut:
		if prefix != ownerStore {
			writeError(w, Forbidden)
			return true
		}
		var in store.CheckoutSettings
		if DecodeJSON(w, r, &in, 32<<10) {
			out, err := d.Store.SaveCheckoutSettings(ctx, in)
			checkoutReply(w, out, err)
		}
	case relative == "/orders" && r.Method == http.MethodGet:
		out, err := d.Store.Orders(ctx, q.Get("cursor"))
		checkoutReply(w, out, err)
	case len(parts) == 2 && parts[0] == "orders" && r.Method == http.MethodGet:
		out, err := d.Store.MerchantOrder(ctx, parts[1])
		checkoutReply(w, out, err)
	case relative == "/categories" && r.Method == http.MethodPut:
		var in store.Category
		if DecodeJSON(w, r, &in, 4096) {
			out, err := d.Store.SaveCategory(ctx, in)
			storeReply(w, out, err)
		}
	case relative == "/images" && r.Method == http.MethodPost && d.Media != nil:
		return d.uploadMedia(w, r, owner, true)
	case len(parts) == 2 && parts[0] == "images" && (r.Method == http.MethodGet || r.Method == http.MethodHead) && d.Media != nil:
		ok, err := d.Store.OwnsImage(ctx, parts[1])
		if err == nil && !ok {
			err = sql.ErrNoRows
		}
		if err == nil {
			err = d.Media.Serve(w, r, parts[1], access.Principal{Audience: access.Owner, ID: owner})
		}
		if err != nil {
			domainError(w, err)
		}
	case len(parts) == 3 && parts[0] == "products" && r.Method == http.MethodGet:
		out, err := d.Store.Draft(ctx, parts[1], parts[2])
		storeReply(w, out, err)
	case len(parts) == 3 && parts[0] == "products" && r.Method == http.MethodPut:
		var in store.DraftInput
		if DecodeJSON(w, r, &in, 32<<10) {
			out, err := d.Store.SaveDraft(ctx, parts[1], parts[2], in)
			storeReply(w, out, err)
		}
	case len(parts) == 4 && parts[0] == "products" && parts[3] == "publication" && r.Method == http.MethodPut:
		var in struct {
			Version   int64 `json:"version"`
			Published *bool `json:"published"`
		}
		if DecodeJSON(w, r, &in, 1024) {
			if in.Published == nil {
				writeError(w, InvalidFields)
			} else {
				out, err := d.Store.Publication(ctx, parts[1], parts[2], in.Version, *in.Published)
				storeReply(w, out, err)
			}
		}
	case len(parts) == 4 && parts[0] == "products" && parts[2] == "variants" && r.Method == http.MethodPut:
		var in store.VariantInput
		if DecodeJSON(w, r, &in, 16<<10) {
			out, err := d.Store.SaveVariant(ctx, parts[1], parts[3], in)
			storeReply(w, out, err)
		}
	case len(parts) == 2 && parts[0] == "inventory" && r.Method == http.MethodGet:
		out, err := d.Store.Adjustments(ctx, parts[1])
		storeReply(w, out, err)
	case len(parts) == 2 && parts[0] == "inventory" && r.Method == http.MethodPost:
		var in store.AdjustmentInput
		if DecodeJSON(w, r, &in, 4096) {
			out, err := d.Store.Adjust(ctx, owner, parts[1], in)
			storeReply(w, out, err)
		}
	default:
		writeError(w, MethodNotAllowed)
	}
	return true
}

func storeReply(w http.ResponseWriter, out any, err error) {
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, out)
	case errors.Is(err, store.ErrInvalid):
		writeError(w, InvalidFields)
	case errors.Is(err, store.ErrConflict):
		writeError(w, RevisionConflict)
	case errors.Is(err, store.ErrStock):
		writeJSON(w, http.StatusConflict, map[string]any{"error": map[string]string{"code": "store_stock",
			"message": "Stock would be outside the supported range.", "request_id": w.Header().Get("X-Request-ID")}})
	case errors.Is(err, store.ErrVariant):
		writeJSON(w, http.StatusConflict, map[string]any{"error": map[string]string{"code": "store_variant",
			"message": "A published product requires an active variant.", "request_id": w.Header().Get("X-Request-ID")}})
	default:
		domainError(w, err)
	}
}
