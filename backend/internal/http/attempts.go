package httpserver

import (
	"github.com/mbougarne/modawana/backend/internal/learning"
	"net/http"
	"strings"
)

func (d *Domain) attemptRoutes(w http.ResponseWriter, r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, "/api/v1/learning/content/") {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/learning/content/"), "/")
	if len(parts) != 3 {
		writeError(w, NotFound)
		return true
	}
	key, locale, action := parts[0], parts[1], parts[2]
	if r.URL.RawQuery != "" {
		writeError(w, BadRequest)
		return true
	}
	p := d.readerPrincipal(r)
	if r.Method == "GET" && action == "quiz" {
		out, e := d.Content.ReaderQuiz(r.Context(), key, locale, p)
		if e != nil {
			domainError(w, e)
		} else {
			writeJSON(w, 200, out)
		}
		return true
	}
	if r.Method != "POST" || (action != "attempts" && action != "read") {
		writeError(w, MethodNotAllowed)
		return true
	}
	if r.Header.Get("Origin") != d.Auth.Config.BaseURL || r.Header.Get("Sec-Fetch-Site") == "cross-site" || !learning.CheckCSRF(learningToken(r), r.Header.Get("X-CSRF-Token")) {
		writeError(w, Forbidden)
		return true
	}
	if !d.Learning.AllowAttempt(d.Auth.Config.ClientIP(r.RemoteAddr, r.Header.Get("X-Forwarded-For")).String()) {
		writeError(w, RateLimited)
		return true
	}
	if action == "attempts" {
		var in learning.Submission
		if !DecodeJSON(w, r, &in, 32768) {
			return true
		}
		out, e := d.Content.SubmitQuiz(r.Context(), key, locale, p, in)
		if e != nil {
			domainError(w, e)
		} else {
			writeJSON(w, 200, out)
		}
	} else {
		var in struct {
			Version learning.Version `json:"version"`
		}
		if !DecodeJSON(w, r, &in, 4096) {
			return true
		}
		out, e := d.Content.MarkRead(r.Context(), key, locale, p, in.Version)
		if e != nil {
			domainError(w, e)
		} else {
			writeJSON(w, 200, out)
		}
	}
	return true
}
