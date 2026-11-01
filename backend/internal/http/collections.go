package httpserver

import (
	"net/http"
	"strings"

	"github.com/mbougarne/modawana/backend/internal/content"
)

func (d *Domain) collectionRoutes(w http.ResponseWriter, r *http.Request) bool {
	p := r.URL.Path
	if strings.HasPrefix(p, "/api/v1/public/collections/") && r.Method == "GET" {
		parts := strings.Split(strings.TrimPrefix(p, "/api/v1/public/collections/"), "/")
		if len(parts) != 2 {
			writeError(w, NotFound)
			return true
		}
		out, e := d.Content.PublicCollection(r.Context(), parts[0], parts[1], d.readerPrincipal(r))
		if e != nil {
			domainError(w, e)
		} else {
			writeJSON(w, 200, out)
		}
		return true
	}
	if p != "/api/v1/collections" && !strings.HasPrefix(p, "/api/v1/collections/") {
		return false
	}
	owner, e := d.Auth.OwnerRequest(r)
	if e != nil {
		domainError(w, e)
		return true
	}
	if p == "/api/v1/collections" && r.Method == "GET" {
		items, next, e := d.Content.Collections(r.Context(), r.URL.Query().Get("cursor"), func() string {
			l := r.URL.Query().Get("locale")
			if l == "" {
				return "en"
			}
			return l
		}())
		if e != nil {
			domainError(w, e)
		} else {
			writeJSON(w, 200, map[string]any{"items": items, "next_cursor": next})
		}
		return true
	}
	parts := strings.Split(strings.TrimPrefix(p, "/api/v1/collections/"), "/")
	if len(parts) == 1 && r.Method == "DELETE" {
		var in struct {
			ExpectedVersion int64 `json:"expected_version"`
		}
		if !DecodeJSON(w, r, &in, 4096) {
			return true
		}
		if err := d.Content.RemoveCollection(r.Context(), owner, parts[0], in.ExpectedVersion); err != nil {
			domainError(w, err)
		} else {
			writeJSON(w, 200, map[string]bool{"removed": true})
		}
		return true
	}
	if len(parts) == 1 && r.Method == "GET" {
		out, e := d.Content.Collection(r.Context(), parts[0])
		if e != nil {
			domainError(w, e)
		} else {
			writeJSON(w, 200, out)
		}
		return true
	}
	if len(parts) == 2 {
		key, action := parts[0], parts[1]
		if action == "progress-reset" && r.Method == "POST" {
			var in struct {
				ExpectedVersion int64 `json:"expected_version"`
			}
			if !DecodeJSON(w, r, &in, 4096) {
				return true
			}
			check, e := d.Auth.FreshOwnerCheck(r)
			if e != nil {
				domainError(w, e)
				return true
			}
			epoch, e := d.Content.ResetPathProgress(r.Context(), key, in.ExpectedVersion, check)
			if e != nil {
				domainError(w, e)
			} else {
				writeJSON(w, 200, map[string]int64{"epoch": epoch})
			}
			return true
		}
		if action == "workspace" && r.Method == "PUT" {
			var in content.CollectionWorkspaceInput
			if !DecodeJSON(w, r, &in, 1<<20) {
				return true
			}
			out, err := d.Content.SaveCollectionWorkspace(r.Context(), owner, key, r.URL.Query().Get("locale"), r.Header.Get("Idempotency-Key"), in)
			if err != nil {
				domainError(w, err)
			} else {
				writeJSON(w, 200, out)
			}
			return true
		}
		if action == "plan" && r.Method == "PUT" {
			var in content.PlanInput
			if !DecodeJSON(w, r, &in, 1<<20) {
				return true
			}
			out, e := d.Content.SavePlan(r.Context(), owner, key, in)
			if e != nil {
				domainError(w, e)
			} else {
				writeJSON(w, 200, out)
			}
			return true
		}
		if action == "apply" && r.Method == "POST" {
			var in content.ApplyPlanInput
			if !DecodeJSON(w, r, &in, 4096) {
				return true
			}
			out, e := d.Content.ApplyPlan(r.Context(), key, in)
			if e != nil {
				domainError(w, e)
			} else {
				writeJSON(w, 200, out)
			}
			return true
		}
		if r.Method == "PUT" {
			var in content.CollectionInput
			if !DecodeJSON(w, r, &in, 4096) {
				return true
			}
			out, e := d.Content.SaveCollection(r.Context(), key, action, in)
			if e != nil {
				domainError(w, e)
			} else {
				writeJSON(w, 200, out)
			}
			return true
		}
	}
	writeError(w, MethodNotAllowed)
	return true
}
