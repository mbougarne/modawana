package auth

import (
	"database/sql"
	"net/http"
)

func (s *Service) checkFailures(r *http.Request) error {
	ip := s.Config.ThrottleIP(r.RemoteAddr, r.Header.Get("X-Forwarded-For"))
	for _, bucket := range []struct {
		key string
		max int
	}{{"failed_ip:" + ip, 5}, {"failed_account", 25}} {
		var n int
		e := s.DB.QueryRowContext(r.Context(), "SELECT attempts FROM auth_throttles WHERE bucket=? AND expires_at>?", digest(bucket.key), s.now()).Scan(&n)
		if e != nil && e != sql.ErrNoRows {
			return e
		}
		if e == nil && n >= bucket.max {
			return ErrRate
		}
	}
	return nil
}
func (s *Service) failure(r *http.Request, event string) error {
	ip := s.Config.ThrottleIP(r.RemoteAddr, r.Header.Get("X-Forwarded-For"))
	for _, key := range []string{"failed_ip:" + ip, "failed_account"} {
		max := 5
		if key == "failed_account" {
			max = 25
		}
		if e := s.limit(r.Context(), key, max); e != nil && e != ErrRate {
			return e
		}
	}
	return s.audit(r.Context(), s.DB, event, r.Header.Get("X-Request-ID"))
}
func (s *Service) fallback(w http.ResponseWriter, r *http.Request) bool {
	p := r.URL.Path
	if r.Method != "POST" || (p != "/api/v1/session" && p != "/api/v1/session/totp" && p != "/api/v1/session/recovery") {
		return false
	}
	if e := s.checkFailures(r); e != nil {
		fail(w, e)
		return true
	}
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if !decode(w, r, &in) {
		return true
	}
	var next string
	var e error
	full := false
	switch p {
	case "/api/v1/session":
		next, e = s.password(r.Context(), cookie(r, flowCookie), in.Email, in.Password, w.Header().Get("X-Request-ID"))
	case "/api/v1/session/totp":
		next, e = s.verifyOTP(r.Context(), cookie(r, flowCookie), in.Code, cookie(r, ownerCookie), w.Header().Get("X-Request-ID"))
		full = true
	case "/api/v1/session/recovery":
		next, full, e = s.recoverCode(r.Context(), cookie(r, flowCookie), in.Code, cookie(r, ownerCookie), w.Header().Get("X-Request-ID"))
	}
	if e != nil {
		if failureError := s.failure(r, "fallback_failed"); failureError != nil {
			e = failureError
		}
		fail(w, e)
		return true
	}
	state := "password"
	if p == "/api/v1/session/recovery" {
		state = "recovery"
	}
	if full {
		state = "owner"
		setCookie(w, ownerCookie, next, 43200)
		setCookie(w, flowCookie, "", -1)
	} else {
		setCookie(w, flowCookie, next, 300)
	}
	jsonOut(w, 200, map[string]string{"state": state, "csrf": csrf(next)})
	return true
}
func (s *Service) securityFallback(w http.ResponseWriter, r *http.Request, session Session) bool {
	p := r.URL.Path
	ctx := r.Context()
	request := w.Header().Get("X-Request-ID")
	if p == "/api/v1/security/factors" && r.Method == "GET" {
		var enabled bool
		var email string
		var remaining int
		if s.DB.QueryRowContext(ctx, "SELECT totp_cipher IS NOT NULL,login_email FROM owner WHERE id=?", session.Owner).Scan(&enabled, &email) != nil {
			fail(w, ErrUnavailable)
			return true
		}
		if e := s.DB.QueryRowContext(ctx, "SELECT count(*) FROM recovery_codes WHERE owner_id=? AND consumed_at IS NULL", session.Owner).Scan(&remaining); e != nil {
			fail(w, e)
			return true
		}
		_, e := s.encryptionKey()
		jsonOut(w, 200, map[string]any{"totp_enabled": enabled, "totp_available": e == nil, "login_email": email, "recovery_codes_remaining": remaining})
		return true
	}
	if p == "/api/v1/security/recovery-codes" && r.Method == "POST" {
		codes, e := s.regenerateCodes(ctx, session, request)
		if e != nil {
			fail(w, e)
			return true
		}
		jsonOut(w, 200, map[string]any{"codes": codes})
		return true
	}
	if p == "/api/v1/security/totp/options" && r.Method == "POST" {
		var in struct {
			Password string `json:"password"`
		}
		if !decode(w, r, &in) {
			return true
		}
		raw, seed, e := s.enrollTOTP(ctx, session, cookie(r, ownerCookie), in.Password)
		if e != nil {
			fail(w, e)
			return true
		}
		setCookie(w, flowCookie, raw, 300)
		jsonOut(w, 200, map[string]string{"secret": seed})
		return true
	}
	if p == "/api/v1/security/totp/verify" && r.Method == "POST" {
		if e := s.checkFailures(r); e != nil {
			fail(w, e)
			return true
		}
		var in struct {
			Code string `json:"code"`
		}
		if !decode(w, r, &in) {
			return true
		}
		if e := s.confirmTOTP(ctx, session, cookie(r, flowCookie), cookie(r, ownerCookie), in.Code, request); e != nil {
			if failureError := s.failure(r, "totp_enrollment_failed"); failureError != nil {
				e = failureError
			}
			fail(w, e)
			return true
		}
		setCookie(w, ownerCookie, "", -1)
		setCookie(w, flowCookie, "", -1)
		jsonOut(w, 200, map[string]string{"state": "sign_in"})
		return true
	}
	if p == "/api/v1/security/totp" && r.Method == "DELETE" {
		tx, e := s.DB.BeginTx(ctx, nil)
		if e != nil {
			fail(w, e)
			return true
		}
		defer tx.Rollback()
		if e = s.freshInTx(ctx, tx, session); e != nil {
			fail(w, e)
			return true
		}
		var n int
		if e := tx.QueryRowContext(ctx, "SELECT count(*) FROM passkeys WHERE owner_id=? AND revoked_at IS NULL AND tested_at IS NOT NULL", session.Owner).Scan(&n); e != nil {
			fail(w, e)
			return true
		} else if n == 0 {
			fail(w, &OwnerActionError{Path: "security.passkeys", Reason: "tested_passkey", Conflict: true})
			return true
		}
		if _, e = tx.ExecContext(ctx, "UPDATE owner SET password_hash=NULL,totp_cipher=NULL,totp_step=-1 WHERE id=?", session.Owner); e == nil {
			e = s.invalidate(ctx, tx)
		}
		if e == nil {
			e = s.audit(ctx, tx, "totp_disabled", request)
		}
		if e == nil {
			e = tx.Commit()
		}
		if e != nil {
			fail(w, e)
			return true
		}
		setCookie(w, ownerCookie, "", -1)
		jsonOut(w, 200, map[string]bool{"ok": true})
		return true
	}
	return false
}
