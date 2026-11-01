package auth

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
)

const ownerCookie = "__Host-owner"
const flowCookie = "__Host-auth"

func cookie(r *http.Request, name string) string {
	c, e := r.Cookie(name)
	if e != nil {
		return ""
	}
	return c.Value
}
func setCookie(w http.ResponseWriter, name, value string, max int) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: max})
}
func jsonOut(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, e error) {
	var action *OwnerActionError
	if errors.As(e, &action) {
		status, code := 422, "invalid_fields"
		if action.HTTPStatus == 403 {
			status, code = 403, "forbidden"
		}
		if action.Conflict {
			status, code = 409, "revision_conflict"
		}
		jsonOut(w, status, map[string]any{"error": map[string]any{"code": code, "message": "Owner action could not be completed.", "request_id": w.Header().Get("X-Request-ID"), "fields": []map[string]string{{"path": action.Path, "code": action.Reason}}}})
		return
	}
	status := 500
	code := "internal_error"
	message := "Request failed."
	switch {
	case errors.Is(e, ErrAuth):
		status = 401
		code = "unauthenticated"
		message = "Authentication failed."
	case errors.Is(e, ErrForbidden):
		status = 403
		code = "forbidden"
		message = "Access denied."
	case errors.Is(e, ErrRate):
		status = 429
		code = "rate_limited"
		message = "Try again later."
		w.Header().Set("Retry-After", "900")
	case errors.Is(e, ErrConflict):
		status = 409
		code = "revision_conflict"
		message = "Security state changed."
	case errors.Is(e, ErrUnavailable):
		status = 503
		code = "unavailable"
		message = "Authentication unavailable."
	}
	jsonOut(w, status, map[string]any{"error": map[string]string{"code": code, "message": message, "request_id": w.Header().Get("X-Request-ID")}})
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	invalid := func(status int, code string) bool {
		jsonOut(w, status, map[string]any{"error": map[string]string{"code": code, "message": "Invalid request.", "request_id": w.Header().Get("X-Request-ID")}})
		return false
	}
	contentType, _, mediaError := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mediaError != nil || contentType != "application/json" {
		return invalid(http.StatusUnsupportedMediaType, "unsupported_media_type")
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	b, e := io.ReadAll(r.Body)
	b = bytes.TrimSpace(b)
	if e != nil {
		var limit *http.MaxBytesError
		if errors.As(e, &limit) {
			return invalid(http.StatusRequestEntityTooLarge, "too_large")
		}
		fail(w, e)
		return false
	}
	if len(b) == 0 || b[0] != '{' {
		return invalid(http.StatusBadRequest, "bad_request")
	}
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil {
		return invalid(http.StatusBadRequest, "bad_request")
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return invalid(http.StatusBadRequest, "bad_request")
	}
	return true
}
func (s *Service) checkWrite(r *http.Request, token string) bool {
	return r.Header.Get("Origin") == s.Config.BaseURL && r.Header.Get("Sec-Fetch-Site") != "cross-site" && same(r.Header.Get("X-CSRF-Token"), csrf(token)) && validSecret(token)
}

// Handle returns false for routes outside the authentication namespace.
func (s *Service) Handle(w http.ResponseWriter, r *http.Request) bool {
	if s.oauthRoutes(w, r) {
		return true
	}
	p := r.URL.Path
	if p != "/api/v1/session" && !strings.HasPrefix(p, "/api/v1/auth/") && !strings.HasPrefix(p, "/api/v1/security/") && !strings.HasPrefix(p, "/api/v1/session/") {
		return false
	}
	w.Header().Set("Cache-Control", "no-store")
	r.Header.Set("X-Request-ID", w.Header().Get("X-Request-ID"))
	raw := cookie(r, ownerCookie)
	flow := cookie(r, flowCookie)
	ip := s.Config.ThrottleIP(r.RemoteAddr, r.Header.Get("X-Forwarded-For"))
	request := w.Header().Get("X-Request-ID")
	if p == "/api/v1/auth/start" && r.Method == "POST" {
		// A custom header plus an exact Origin bootstraps CSRF without a state-changing GET.
		if r.Header.Get("Origin") != s.Config.BaseURL || r.Header.Get("X-Blog-Request") != "1" {
			fail(w, ErrForbidden)
			return true
		}
		next, e := s.Start(r.Context(), ip)
		if e != nil {
			fail(w, e)
			return true
		}
		setCookie(w, flowCookie, next, 300)
		jsonOut(w, 200, map[string]string{"csrf": csrf(next), "state": "anonymous"})
		return true
	}
	if p == "/api/v1/session" && r.Method == "GET" {
		if session, e := s.Session(r.Context(), raw, false); e == nil {
			jsonOut(w, 200, map[string]any{"state": "owner", "csrf": csrf(raw), "session": session})
			return true
		}
		if f, e := s.flow(r.Context(), flow); e == nil {
			jsonOut(w, 200, map[string]string{"state": f.Kind, "csrf": csrf(flow)})
			return true
		}
		jsonOut(w, 200, map[string]string{"state": "anonymous"})
		return true
	}
	if r.Method != "GET" {
		token := flow
		if strings.HasPrefix(p, "/api/v1/security/") || r.Method == "DELETE" {
			token = raw
		}
		if !s.checkWrite(r, token) {
			if strings.HasPrefix(p, "/api/v1/security/") {
				fail(w, &OwnerActionError{Path: "security.request", Reason: "request_confirmation", Cause: ErrForbidden, HTTPStatus: 403})
			} else {
				fail(w, ErrForbidden)
			}
			return true
		}
	}
	if p == "/api/v1/auth/grant" && r.Method == "POST" {
		if e := s.checkFailures(r); e != nil {
			fail(w, e)
			return true
		}
		if e := s.limit(r.Context(), "failure:"+ip, 5); e != nil {
			fail(w, e)
			return true
		}
		var in struct {
			Grant string `json:"grant"`
			Email string `json:"email"`
		}
		if !decode(w, r, &in) {
			return true
		}
		next, e := s.Redeem(r.Context(), flow, in.Grant, in.Email, request)
		if e != nil {
			if failureError := s.failure(r, "grant_failed"); failureError != nil {
				e = failureError
			}
			fail(w, e)
			return true
		}
		setCookie(w, flowCookie, next, 300)
		jsonOut(w, 200, map[string]string{"state": "enrollment", "csrf": csrf(next)})
		return true
	}
	if p == "/api/v1/session" && r.Method == "DELETE" {
		session, e := s.Session(r.Context(), raw, false)
		if e != nil {
			fail(w, e)
			return true
		}
		tx, e := s.DB.BeginTx(r.Context(), nil)
		if e != nil {
			fail(w, e)
			return true
		}
		defer tx.Rollback()
		if _, e = tx.ExecContext(r.Context(), "UPDATE sessions SET revoked_at=? WHERE id=?", s.now(), session.ID); e == nil {
			e = s.audit(r.Context(), tx, "logout", request)
		}
		if e == nil {
			e = tx.Commit()
		}
		if e != nil {
			fail(w, e)
			return true
		}
		setCookie(w, ownerCookie, "", -1)
		setCookie(w, flowCookie, "", -1)
		jsonOut(w, 200, map[string]bool{"ok": true})
		return true
	}
	if strings.HasPrefix(p, "/api/v1/security/") {
		session, e := s.Session(r.Context(), raw, r.Method != "GET")
		if e != nil {
			fail(w, e)
			return true
		}
		if s.security(w, r, session) {
			return true
		}
	}
	if s.ceremony(w, r) {
		return true
	}
	fail(w, ErrAuth)
	return true
}
