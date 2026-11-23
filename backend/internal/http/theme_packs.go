package httpserver

import (
	"bytes"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/mbougarne/modawana/backend/internal/theme"
)

const packAPI = "/api/v1/settings/theme-packs"

func (d *Domain) themeRoutes(w http.ResponseWriter, r *http.Request) bool {
	p := r.URL.Path
	packs := d.SiteConfig.Themes
	if packs == nil {
		return false
	}
	if strings.HasPrefix(p, "/theme-assets/") {
		d.themeAsset(w, r)
		return true
	}
	if p != packAPI && !strings.HasPrefix(p, packAPI+"/") {
		return false
	}
	if d.Auth == nil {
		writeError(w, Unauthenticated)
		return true
	}
	if _, err := d.Auth.OwnerRequest(r); err != nil {
		domainError(w, err)
		return true
	}
	if p == packAPI && r.Method == http.MethodGet {
		items, err := packs.List(r.Context())
		if err != nil {
			writeError(w, Unavailable)
		} else {
			writeJSON(w, http.StatusOK, map[string]any{"items": items})
		}
		return true
	}
	if r.Method != http.MethodPost {
		writeError(w, MethodNotAllowed)
		return true
	}
	id, action, ok := strings.Cut(strings.TrimPrefix(p, packAPI+"/"), "/")
	if !ok || !theme.IsPack(id) || (action != "approve" && action != "remove") {
		writeError(w, NotFound)
		return true
	}
	var in struct {
		Revision *string `json:"expected_revision"`
		Checksum string  `json:"expected_checksum"`
	}
	if !DecodeJSON(w, r, &in, 4096) {
		return true
	}
	if in.Revision == nil || (action == "remove" && in.Checksum != "") {
		writeError(w, InvalidFields)
		return true
	}
	check, err := d.Auth.FreshOwnerCheck(r)
	if err != nil {
		domainError(w, err)
		return true
	}
	if action == "remove" {
		if err = packs.Remove(r.Context(), id, *in.Revision, check); err == nil {
			writeJSON(w, http.StatusOK, map[string]bool{"removed": true})
			return true
		}
	} else {
		var approval theme.Approval
		if approval, err = packs.Approve(r.Context(), id, in.Checksum, *in.Revision, check); err == nil {
			writeJSON(w, http.StatusOK, approval)
			return true
		}
	}
	switch {
	case errors.Is(err, theme.ErrPackChanged):
		writeError(w, RevisionConflict)
	case errors.Is(err, theme.ErrPack), errors.Is(err, theme.ErrPackFiles):
		writeError(w, InvalidFields)
	default:
		domainError(w, err)
	}
	return true
}

func (d *Domain) themeAsset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, MethodNotAllowed)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/theme-assets/"), "/")
	if len(parts) != 3 || !theme.IsPack(parts[0]) || len(parts[1]) != 64 {
		writeError(w, NotFound)
		return
	}
	pack, err := d.SiteConfig.Themes.ApprovedPack(r.Context(), d.SiteConfig.DB, parts[0])
	asset := pack.Asset(parts[2])
	if err != nil || pack.Checksum != parts[1] || len(asset) == 0 {
		writeError(w, NotFound)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	http.ServeContent(w, r, parts[2], time.Time{}, bytes.NewReader(asset))
}
