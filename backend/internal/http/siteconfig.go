package httpserver

import (
	"github.com/mbougarne/modawana/backend/internal/siteconfig"
	"net/http"
	"strings"
)

func (d *Domain) configurationRoutes(w http.ResponseWriter, r *http.Request) bool {
	p := r.URL.Path
	if p == "/api/v1/public/site" || p == "/api/v1/public/labels" {
		if r.Method != "GET" {
			writeError(w, MethodNotAllowed)
			return true
		}
		out, e := d.SiteConfig.Public(r.Context(), r.URL.Query().Get("locale"))
		if e != nil {
			domainError(w, e)
		} else if strings.HasSuffix(p, "/labels") {
			writeJSON(w, 200, map[string]any{"overrides": out.Labels, "version": out.Versions["labels"]})
		} else {
			writeJSON(w, 200, out)
		}
		return true
	}
	parts := strings.Split(strings.TrimPrefix(p, "/api/v1/"), "/")
	kinds := map[string]string{"settings": "site", "profile": "profile", "labels": "labels"}
	kind := kinds[parts[0]]
	if !strings.HasPrefix(p, "/api/v1/") || kind == "" {
		return false
	}
	owner, e := d.Auth.OwnerRequest(r)
	if e != nil {
		domainError(w, e)
		return true
	}
	if len(parts) == 1 && r.Method == "GET" {
		out, e := d.SiteConfig.Get(r.Context(), kind)
		if e != nil {
			domainError(w, e)
		} else {
			writeJSON(w, 200, out)
		}
		return true
	}
	if len(parts) == 2 && parts[1] == "defaults" && r.Method == "GET" {
		out, e := d.SiteConfig.Defaults(r.Context(), kind)
		if e != nil {
			domainError(w, e)
		} else {
			writeJSON(w, 200, out)
		}
		return true
	}
	if len(parts) == 2 && parts[1] == "registry" && kind == "labels" && r.Method == "GET" {
		writeJSON(w, 200, siteconfig.Registry)
		return true
	}
	if len(parts) == 2 && parts[1] == "history" && r.Method == "GET" {
		items, next, e := d.SiteConfig.History(r.Context(), kind, r.URL.Query().Get("cursor"))
		if e != nil {
			domainError(w, e)
		} else {
			writeJSON(w, 200, map[string]any{"items": items, "next_cursor": next})
		}
		return true
	}
	if len(parts) == 2 && parts[1] == "restore" && r.Method == "POST" {
		var in struct {
			ExpectedVersion *int64 `json:"expected_version"`
			RevisionID      string `json:"revision_id"`
		}
		if !DecodeJSON(w, r, &in, 4096) {
			return true
		}
		if in.ExpectedVersion == nil {
			writeError(w, InvalidFields)
			return true
		}
		out, e := d.SiteConfig.Restore(r.Context(), kind, in.RevisionID, *in.ExpectedVersion)
		if e != nil {
			domainError(w, e)
		} else {
			writeJSON(w, 200, out)
		}
		return true
	}
	if len(parts) == 1 && r.Method == "PUT" || len(parts) == 2 && parts[1] == "preview" && r.Method == "POST" {
		var in siteconfig.Input
		if !DecodeJSON(w, r, &in, 1<<20) {
			return true
		}
		if len(parts) == 2 {
			out, e := d.SiteConfig.Preview(r.Context(), kind, in)
			if e != nil {
				domainError(w, e)
			} else {
				writeJSON(w, 200, out)
			}
		} else {
			out, e := d.SiteConfig.Apply(r.Context(), owner, kind, in)
			if e != nil {
				domainError(w, e)
			} else {
				writeJSON(w, 200, out)
			}
		}
		return true
	}
	writeError(w, MethodNotAllowed)
	return true
}
