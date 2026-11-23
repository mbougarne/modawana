package httpserver

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/mbougarne/modawana/backend/internal/auth"
	"github.com/mbougarne/modawana/backend/internal/content"
)

func (d *Domain) staffContentRoutes(w http.ResponseWriter, r *http.Request) bool {
	const root = "/api/v1/staff/content"
	p := r.URL.Path
	if p != root && !strings.HasPrefix(p, root+"/") {
		return false
	}
	if p == root {
		if r.Method != http.MethodGet {
			writeError(w, MethodNotAllowed)
			return true
		}
		if _, _, err := d.Auth.StaffRequest(r, auth.StaffReadContent); err != nil {
			domainError(w, err)
			return true
		}
		q := r.URL.Query()
		if kind := q.Get("kind"); kind != "article" && kind != "page" {
			writeError(w, InvalidFields)
			return true
		}
		items, next, err := d.Content.OwnerList(r.Context(), q.Get("locale"), q.Get("kind"), "", q.Get("q"), q.Get("cursor"))
		if err != nil {
			domainError(w, err)
		} else {
			writeJSON(w, 200, map[string]any{"items": items, "next_cursor": next})
		}
		return true
	}
	parts := strings.Split(strings.TrimPrefix(p, root+"/"), "/")
	if len(parts) != 3 || (parts[2] != "draft" && parts[2] != "publish") {
		writeError(w, NotFound)
		return true
	}
	key, locale := parts[0], parts[1]
	permission := auth.StaffReadContent
	switch {
	case r.Method == http.MethodPut && parts[2] == "draft":
		permission = auth.StaffWriteDraft
	case r.Method == http.MethodPost && parts[2] == "publish":
		permission = auth.StaffPublish
	case r.Method == http.MethodGet && parts[2] == "draft":
	default:
		writeError(w, MethodNotAllowed)
		return true
	}
	r, owner, err := d.Auth.StaffRequest(r, permission)
	if err != nil {
		domainError(w, err)
		return true
	}
	draft, err := d.Content.Draft(r.Context(), key, locale)
	missing := errors.Is(err, sql.ErrNoRows)
	if err != nil && (!missing || permission != auth.StaffWriteDraft) {
		domainError(w, err)
		return true
	}
	if !missing && draft.Kind != "article" && draft.Kind != "page" {
		writeError(w, NotFound)
		return true
	}
	switch permission {
	case auth.StaffReadContent:
		writeJSON(w, 200, draft)
	case auth.StaffWriteDraft:
		d.saveStaffDraft(w, r, owner, key, locale, draft, missing)
	case auth.StaffPublish:
		var input content.Action
		if !DecodeJSON(w, r, &input, 8<<10) {
			return true
		}
		if input.Plan != nil {
			writeError(w, InvalidFields)
			return true
		}
		out, err := d.Content.ChangePublication(r.Context(), owner, key, locale, r.Header.Get("Idempotency-Key"), "publish", input)
		if err != nil {
			domainError(w, err)
		} else {
			writeJSON(w, 200, out)
		}
	}
	return true
}

func (d *Domain) saveStaffDraft(w http.ResponseWriter, r *http.Request, owner, key, locale string, draft content.Draft, missing bool) {
	var input struct {
		Expected *int64 `json:"expected_version"`
		Kind     string `json:"kind,omitempty"`
		Slug     string `json:"slug,omitempty"`
		Title    string `json:"title"`
		Markdown string `json:"markdown"`
	}
	if !DecodeJSON(w, r, &input, 128<<10) {
		return
	}
	in := content.DraftInput{ExpectedVersion: input.Expected, Title: input.Title, Markdown: input.Markdown}
	if missing {
		if input.Kind != "article" && input.Kind != "page" {
			writeError(w, InvalidFields)
			return
		}
		in.Kind, in.SourceLocale, in.Slug = input.Kind, locale, input.Slug
	} else {
		rev := draft.Revision
		if input.Kind != "" || input.Slug != "" || rev.BodyFormat == content.BlockBodyFormat {
			writeError(w, InvalidFields)
			return
		}
		in.Kind, in.SourceLocale, in.Slug = draft.Kind, draft.SourceLocale, rev.Slug
		in.Summary, in.SourceRevisionID, in.PageTemplate = rev.Summary, rev.SourceID, rev.PageTemplate
		in.CategoryKey, in.TagKeys, in.CoverMediaID, in.Presentations = rev.CategoryKey, rev.Tags, rev.CoverID, rev.Presentations
		in.CollectionKey, in.Position = draft.CollectionKey, draft.Position
	}
	out, err := d.Content.Save(r.Context(), owner, key, locale, r.Header.Get("Idempotency-Key"), in)
	if err != nil {
		domainError(w, err)
		return
	}
	writeJSON(w, 200, out)
}
