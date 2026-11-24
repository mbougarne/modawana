package auth

import (
	"context"
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
	id, err := s.FreshOwnerBinding(r)
	if err != nil {
		return nil, err
	}
	return func(tx *sql.Tx) error { return s.CheckFreshOwnerBinding(r.Context(), tx, id) }, nil
}

// FreshOwnerBinding returns a server-side session identifier for binding a
// fresh owner action across an OAuth round trip.
func (s *Service) FreshOwnerBinding(r *http.Request) (string, error) {
	raw := cookie(r, ownerCookie)
	if !s.checkWrite(r, raw) {
		return "", ErrForbidden
	}
	session, err := s.Session(r.Context(), raw, true)
	if err != nil {
		return "", err
	}
	return session.ID, nil
}

func (s *Service) CheckFreshOwnerBinding(ctx context.Context, tx *sql.Tx, id string) error {
	return s.freshInTx(ctx, tx, Session{ID: id})
}
