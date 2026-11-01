package auth

import (
	"database/sql"
	"net/http"
)

// OwnerRequest authorizes domain APIs without exposing cookie secrets. Every
// mutation checks the same Origin/CSRF boundary as owner security operations.
func (s *Service) OwnerRequest(r *http.Request) (string, error) {
	raw := cookie(r, ownerCookie)
	session, e := s.Session(r.Context(), raw, false)
	if e != nil {
		return "", ErrAuth
	}
	if r.Method != "GET" && r.Method != "HEAD" && !s.checkWrite(r, raw) {
		return "", ErrForbidden
	}
	return session.Owner, nil
}

// FreshOwnerCheck captures the authenticated session, then rechecks it inside
// the domain mutation's transaction so concurrent revocation cannot race reset.
func (s *Service) FreshOwnerCheck(r *http.Request) (func(*sql.Tx) error, error) {
	raw := cookie(r, ownerCookie)
	if !s.checkWrite(r, raw) {
		return nil, ErrForbidden
	}
	session, e := s.Session(r.Context(), raw, true)
	if e != nil {
		return nil, e
	}
	return func(tx *sql.Tx) error { return s.freshInTx(r.Context(), tx, session) }, nil
}
