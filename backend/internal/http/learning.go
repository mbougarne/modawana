package httpserver

import (
	"errors"
	"github.com/mbougarne/modawana/backend/internal/learning"
	"net/http"
)

const learningCookie = "__Host-learning"

func learningToken(r *http.Request) string {
	c, e := r.Cookie(learningCookie)
	if e != nil {
		return ""
	}
	return c.Value
}
func (d *Domain) learningRoutes(w http.ResponseWriter, r *http.Request) bool {
	path := r.URL.Path
	if path != "/api/v1/learning/session" && path != "/api/v1/learning/progress" {
		return false
	}
	raw := learningToken(r)
	set := func(value string, max int) {
		http.SetCookie(w, &http.Cookie{Name: learningCookie, Value: value, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: max})
	}
	output := func(identity learning.Identity) {
		progress, e := d.Learning.Progress(r.Context(), identity.ID)
		if e != nil {
			domainError(w, e)
			return
		}
		writeJSON(w, 200, map[string]any{"active": true, "csrf": learning.LearningCSRF(raw), "expires_at": identity.Expires, "idle_days": d.Learning.IdleDays, "scope": "browser", "progress": progress})
	}
	if path == "/api/v1/learning/session" && r.Method == "POST" {
		// Exact Origin plus a custom header bootstraps CSRF without state-changing GETs.
		if r.Header.Get("Origin") != d.Auth.Config.BaseURL || r.Header.Get("Sec-Fetch-Site") == "cross-site" || r.Header.Get("X-Blog-Request") != "1" || r.URL.RawQuery != "" {
			writeError(w, Forbidden)
			return true
		}
		identity, e := d.Learning.Current(r.Context(), raw, true)
		if errors.Is(e, learning.ErrIdentity) {
			raw, identity, e = d.Learning.Create(r.Context(), d.Auth.Config.ClientIP(r.RemoteAddr, r.Header.Get("X-Forwarded-For")).String())
		}
		if e != nil {
			domainError(w, e)
			return true
		}
		set(raw, d.Learning.IdleDays*86400)
		output(identity)
		return true
	}
	if path == "/api/v1/learning/progress" && (r.Method == "GET" || r.Method == "DELETE") {
		if r.URL.RawQuery != "" || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			writeError(w, Forbidden)
			return true
		}
		if r.Method == "DELETE" {
			if r.Header.Get("Origin") != d.Auth.Config.BaseURL || !learning.CheckCSRF(raw, r.Header.Get("X-CSRF-Token")) {
				writeError(w, Forbidden)
				return true
			}
			if e := d.Learning.Reset(r.Context(), raw); e != nil {
				domainError(w, e)
				return true
			}
			set("", -1)
			writeJSON(w, 200, map[string]bool{"reset": true})
			return true
		}
		identity, e := d.Learning.Current(r.Context(), raw, true)
		if errors.Is(e, learning.ErrIdentity) {
			writeJSON(w, 200, map[string]any{"active": false, "idle_days": d.Learning.IdleDays, "scope": "browser", "progress": []any{}})
			return true
		}
		if e != nil {
			domainError(w, e)
			return true
		}
		set(raw, d.Learning.IdleDays*86400)
		output(identity)
		return true
	}
	writeError(w, MethodNotAllowed)
	return true
}
