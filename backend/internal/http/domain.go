package httpserver

import (
	"database/sql"
	"errors"
	"github.com/mbougarne/modawana/backend/internal/learning"
	"io"
	"mime"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/mbougarne/modawana/backend/internal/access"
	"github.com/mbougarne/modawana/backend/internal/auth"
	"github.com/mbougarne/modawana/backend/internal/content"
	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/extension"
	"github.com/mbougarne/modawana/backend/internal/media"
	"github.com/mbougarne/modawana/backend/internal/siteconfig"
)

var uploadHTTPGate = make(chan struct{}, 1)

type Domain struct {
	Learning   *learning.IdentityService
	SiteConfig *siteconfig.Service
	Content    *content.Service
	Auth       *auth.Service
	Media      *media.Service
	Extensions *extension.Host

	builtinOnce sync.Once
}

// extensions installs the built-in publisher MCP tools exactly once on the
// shared host, or on a private one when the caller provided none.
func (d *Domain) extensions() *extension.Host {
	d.builtinOnce.Do(func() {
		if d.Extensions == nil {
			d.Extensions = extension.NewHost(nil)
		}
		// Registration only fails on a malformed built-in declaration or a
		// duplicate slug, both programming errors; the tests pin the list.
		_ = d.Extensions.Install(publisherTools{domain: d})
	})
	return d.Extensions
}

func domainError(w http.ResponseWriter, e error) {
	var validation *siteconfig.ValidationError
	var conversion *content.TypeChangeError
	if errors.As(e, &conversion) {
		validation = &siteconfig.ValidationError{Fields: []siteconfig.FieldIssue{{Path: conversion.Path, Code: conversion.Code}}}
	}
	if validation == nil && errors.Is(e, learning.ErrReview) {
		validation = &siteconfig.ValidationError{Fields: []siteconfig.FieldIssue{{Path: "quiz", Code: "assessment_review"}}}
	}
	if errors.Is(e, siteconfig.ErrReview) {
		validation = &siteconfig.ValidationError{Fields: []siteconfig.FieldIssue{{Path: "reviews", Code: "translation_review"}}}
	}
	if validation != nil || errors.As(e, &validation) {
		writeJSON(w, 422, map[string]any{"error": map[string]any{"code": "invalid_fields", "message": messages[InvalidFields], "request_id": w.Header().Get("X-Request-ID"), "fields": validation.Fields}})
		return
	}
	switch {
	case errors.Is(e, content.ErrLocked):
		writeError(w, LessonLocked)
	case errors.Is(e, learning.ErrIdentity):
		writeError(w, Unauthenticated)
	case errors.Is(e, learning.ErrLimited):
		writeError(w, RateLimited)
	case errors.Is(e, auth.ErrAuth):
		writeError(w, Unauthenticated)
	case errors.Is(e, auth.ErrForbidden):
		writeError(w, Forbidden)
	case errors.Is(e, sql.ErrNoRows):
		writeError(w, NotFound)
	case errors.Is(e, db.ErrVersionConflict):
		writeError(w, RevisionConflict)
	case errors.Is(e, content.ErrIdempotency), errors.Is(e, media.ErrIdempotency):
		writeError(w, IdempotencyConflict)
	case errors.Is(e, learning.ErrInvalid), errors.Is(e, learning.ErrReview), errors.Is(e, siteconfig.ErrInvalid), errors.Is(e, siteconfig.ErrReview), errors.Is(e, content.ErrInvalid), errors.Is(e, content.ErrReview), errors.Is(e, media.ErrInvalid):
		writeError(w, InvalidFields)
	case errors.Is(e, media.ErrTooLarge):
		writeError(w, TooLarge)
	case errors.Is(e, media.ErrReferenced):
		writeError(w, RevisionConflict)
	default:
		writeError(w, InternalError)
	}
}
func (d *Domain) Handle(w http.ResponseWriter, r *http.Request) bool {
	if d.Content != nil && d.mcpRoutes(w, r) {
		return true
	}
	if d.Content != nil && (d.publisherRoutes(w, r) || d.portabilityRoutes(w, r)) {
		return true
	}
	if d.Learning != nil && (d.learningRoutes(w, r) || d.Content != nil && d.attemptRoutes(w, r)) {
		return true
	}
	if d.SiteConfig != nil && d.configurationRoutes(w, r) {
		return true
	}
	if d.Content != nil && (d.referenceRoutes(w, r) || d.pageRoutes(w, r) || d.contentRoutes(w, r) || d.discoveryRoutes(w, r) || d.collectionRoutes(w, r)) {
		return true
	}
	p := r.URL.Path
	if strings.HasPrefix(p, "/media/") {
		if r.Method != "GET" && r.Method != "HEAD" {
			writeError(w, MethodNotAllowed)
			return true
		}
		principal := d.readerPrincipal(r)
		if owner, e := d.Auth.OwnerRequest(r); e == nil {
			principal = access.Principal{Audience: access.Owner, ID: owner}
		}
		if e := d.Media.Serve(w, r, strings.TrimPrefix(p, "/media/"), principal); e != nil {
			domainError(w, e)
		}
		return true
	}
	if p != "/api/v1/media" && !strings.HasPrefix(p, "/api/v1/media/") {
		return false
	}
	owner, e := d.Auth.OwnerRequest(r)
	if e != nil {
		domainError(w, e)
		return true
	}
	if p == "/api/v1/media" && r.Method == "GET" {
		items, next, e := d.Media.List(r.Context(), r.URL.Query().Get("kind"), r.URL.Query().Get("cursor"))
		if e != nil {
			domainError(w, e)
		} else {
			writeJSON(w, 200, map[string]any{"items": items, "next_cursor": next})
		}
		return true
	}
	if p == "/api/v1/media" && r.Method == "POST" {
		return d.uploadMedia(w, r, owner)
	}
	if strings.HasPrefix(p, "/api/v1/media/") && r.Method == "DELETE" {
		e := d.Media.Delete(r.Context(), strings.TrimPrefix(p, "/api/v1/media/"))
		if e != nil {
			domainError(w, e)
		} else {
			writeJSON(w, 200, map[string]bool{"ok": true})
		}
		return true
	}
	writeError(w, MethodNotAllowed)
	return true
}

func (d *Domain) uploadMedia(w http.ResponseWriter, r *http.Request, owner string) bool {
	select {
	case uploadHTTPGate <- struct{}{}:
		defer func() { <-uploadHTTPGate }()
	default:
		writeError(w, RateLimited)
		return true
	}
	controller := http.NewResponseController(w)
	_ = controller.SetReadDeadline(time.Now().Add(2 * time.Minute))
	_ = controller.SetWriteDeadline(time.Now().Add(3 * time.Minute))
	r.Body = http.MaxBytesReader(w, r.Body, (50<<20)+(64<<10))
	defer r.Body.Close()
	mr, e := r.MultipartReader()
	if e != nil {
		writeError(w, BadRequest)
		return true
	}
	part, e := mr.NextPart()
	if e != nil || part.FormName() != "file" || part.FileName() == "" {
		writeError(w, BadRequest)
		return true
	}
	declared, _, _ := mime.ParseMediaType(part.Header.Get("Content-Type"))
	purpose := r.URL.Query().Get("purpose")
	if purpose == "" {
		purpose = "content"
	}
	// Consume the complete bounded multipart before committing: cancellation or
	// extra fields cannot produce a successful partial upload.
	b, e := io.ReadAll(io.LimitReader(part, (50<<20)+1))
	if e != nil {
		writeError(w, BadRequest)
		return true
	}
	if len(b) > 50<<20 {
		writeError(w, TooLarge)
		return true
	}
	if _, e = mr.NextPart(); e != io.EOF {
		writeError(w, BadRequest)
		return true
	}
	m, e := d.Media.UploadWithKey(r.Context(), owner, part.FileName(), declared, purpose, strings.NewReader(string(b)), r.Header.Get("Idempotency-Key"))
	if e != nil {
		domainError(w, e)
	} else {
		writeJSON(w, 201, m)
	}
	return true
}
