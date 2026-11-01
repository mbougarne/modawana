package httpserver

import (
	"errors"
	"github.com/mbougarne/modawana/backend/internal/learning"
	"net/http"
	"strings"

	"github.com/mbougarne/modawana/backend/internal/content"
)

func (d *Domain) contentRoutes(w http.ResponseWriter, r *http.Request) bool {
	p := r.URL.Path
	if strings.HasPrefix(p, "/api/v1/public/content/") && r.Method == "GET" {
		parts := strings.Split(strings.TrimPrefix(p, "/api/v1/public/content/"), "/")
		if len(parts) != 2 {
			writeError(w, NotFound)
			return true
		}
		out, e := d.Content.Public(r.Context(), parts[0], parts[1], d.readerPrincipal(r))
		if errors.Is(e, content.ErrLocked) {
			writeJSON(w, statuses[LessonLocked], map[string]any{"error": APIError{Code: LessonLocked, Message: messages[LessonLocked], RequestID: w.Header().Get("X-Request-ID")}, "teaser": out})
		} else if e != nil {
			domainError(w, e)
		} else {
			writeJSON(w, 200, out)
		}
		return true
	}
	if strings.HasPrefix(p, "/api/v1/public/alternatives/") && r.Method == "GET" {
		out, e := d.Content.Alternatives(r.Context(), strings.TrimPrefix(p, "/api/v1/public/alternatives/"))
		if e != nil {
			domainError(w, e)
		} else {
			writeJSON(w, 200, map[string]any{"items": out})
		}
		return true
	}
	if p != "/api/v1/content" && !strings.HasPrefix(p, "/api/v1/content/") {
		return false
	}
	owner, e := d.Auth.OwnerRequest(r)
	if e != nil {
		domainError(w, e)
		return true
	}
	if p == "/api/v1/content" && r.Method == "GET" {
		q := r.URL.Query()
		if q.Has("locale") {
			items, next, e := d.Content.OwnerList(r.Context(), q.Get("locale"), q.Get("kind"), q.Get("status"), q.Get("q"), q.Get("cursor"))
			if e != nil {
				domainError(w, e)
			} else {
				writeJSON(w, 200, map[string]any{"items": items, "next_cursor": next})
			}
			return true
		}
		items, next, e := d.Content.List(r.Context(), r.URL.Query().Get("cursor"))
		if e != nil {
			domainError(w, e)
		} else {
			writeJSON(w, 200, map[string]any{"items": items, "next_cursor": next})
		}
		return true
	}
	if p == "/api/v1/content/bulk-publication" && r.Method == "POST" {
		var in content.BulkPublication
		if !DecodeJSON(w, r, &in, 256<<10) {
			return true
		}
		out, e := d.Content.ChangePublicationBulk(r.Context(), owner, in)
		if e != nil {
			domainError(w, e)
		} else {
			writeJSON(w, 200, out)
		}
		return true
	}
	parts := strings.Split(strings.TrimPrefix(p, "/api/v1/content/"), "/")
	if len(parts) < 2 || len(parts) > 4 {
		writeError(w, NotFound)
		return true
	}
	key, locale := parts[0], parts[1]
	if len(parts) == 3 && parts[2] == "type" && r.Method == "POST" {
		var in content.TypeChange
		if !DecodeJSON(w, r, &in, 4096) {
			return true
		}
		out, err := d.Content.ChangeType(r.Context(), owner, key, locale, r.Header.Get("Idempotency-Key"), in)
		if err != nil {
			domainError(w, err)
		} else {
			writeJSON(w, 200, out)
		}
		return true
	}
	if len(parts) == 3 && parts[2] == "quiz-gate" {
		if r.Method == "GET" {
			out, e := d.Content.QuizPath(r.Context(), key, locale)
			if e != nil {
				domainError(w, e)
			} else {
				writeJSON(w, 200, out)
			}
			return true
		}
		if r.Method == "PUT" {
			var in content.QuizGateInput
			if !DecodeJSON(w, r, &in, 4096) {
				return true
			}
			out, e := d.Content.SaveQuizGate(r.Context(), owner, key, locale, in)
			if e != nil {
				domainError(w, e)
			} else {
				writeJSON(w, 200, out)
			}
			return true
		}
	}
	if len(parts) == 3 && r.Method == "POST" && parts[2] == "quiz-preview" {
		var in learning.Submission
		if !DecodeJSON(w, r, &in, 32768) {
			return true
		}
		out, e := d.Content.PreviewQuiz(r.Context(), key, locale, in)
		if e != nil {
			domainError(w, e)
		} else {
			writeJSON(w, 200, out)
		}
		return true
	}
	if len(parts) == 3 && r.Method == "POST" && parts[2] == "quiz-review" {
		var in content.Action
		if !DecodeJSON(w, r, &in, 4096) {
			return true
		}
		if e := d.Content.ReviewQuiz(r.Context(), owner, key, locale, in); e != nil {
			domainError(w, e)
		} else {
			writeJSON(w, 200, map[string]bool{"reviewed": true})
		}
		return true
	}
	if len(parts) == 4 && parts[2] == "revisions" && r.Method == "GET" {
		out, e := d.Content.Historical(r.Context(), key, locale, parts[3])
		if e != nil {
			domainError(w, e)
		} else {
			writeJSON(w, 200, out)
		}
		return true
	}
	if len(parts) == 2 {
		switch r.Method {
		case "GET":
			v, e := d.Content.Draft(r.Context(), key, locale)
			if e != nil {
				domainError(w, e)
			} else {
				writeJSON(w, 200, v)
			}
			return true
		case "PUT":
			var in content.DraftInput
			if !DecodeJSON(w, r, &in, 7<<20) {
				return true
			}
			out, e := d.Content.Save(r.Context(), owner, key, locale, r.Header.Get("Idempotency-Key"), in)
			if e != nil {
				domainError(w, e)
			} else {
				status := 200
				if out.Created {
					status = 201
				}
				writeJSON(w, status, out)
			}
			return true
		}
	}
	if len(parts) == 3 && parts[2] == "revisions" && r.Method == "GET" {
		items, next, e := d.Content.History(r.Context(), key, locale, r.URL.Query().Get("cursor"))
		if e != nil {
			domainError(w, e)
		} else {
			writeJSON(w, 200, map[string]any{"items": items, "next_cursor": next})
		}
		return true
	}
	if len(parts) == 3 && parts[2] == "restore" && r.Method == "POST" {
		var in content.Action
		if !DecodeJSON(w, r, &in, 4096) {
			return true
		}
		out, e := d.Content.Restore(r.Context(), owner, key, locale, r.Header.Get("Idempotency-Key"), in)
		if e != nil {
			domainError(w, e)
		} else {
			writeJSON(w, 200, out)
		}
		return true
	}
	if len(parts) == 3 && r.Method == "POST" && (parts[2] == "review" || parts[2] == "publish" || parts[2] == "unpublish") {
		var in content.Action
		if !DecodeJSON(w, r, &in, 4096) {
			return true
		}
		out, e := d.Content.ChangePublication(r.Context(), owner, key, locale, r.Header.Get("Idempotency-Key"), parts[2], in)
		if e != nil {
			domainError(w, e)
		} else {
			writeJSON(w, 200, out)
		}
		return true
	}
	writeError(w, MethodNotAllowed)
	return true
}
