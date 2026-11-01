package auth

import (
	"context"
	"net/http"
	"strings"
	"unicode/utf8"
)

type PasskeyInfo struct {
	ID       string  `json:"id"`
	Label    string  `json:"label"`
	Created  string  `json:"created_at"`
	LastUsed *string `json:"last_used_at"`
	Tested   *string `json:"tested_at"`
}

func (s *Service) revokeKey(ctx context.Context, session Session, id, request string) error {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = s.freshInTx(ctx, tx, session); e != nil {
		return e
	}
	var usable int
	if e = tx.QueryRowContext(ctx, "SELECT count(*) FROM passkeys WHERE owner_id=? AND id!=? AND revoked_at IS NULL AND tested_at IS NOT NULL", session.Owner, id).Scan(&usable); e != nil {
		return e
	}
	var encrypted []byte
	var hash string
	if tx.QueryRowContext(ctx, "SELECT password_hash,totp_cipher FROM owner WHERE id=? AND password_hash IS NOT NULL AND totp_cipher IS NOT NULL", session.Owner).Scan(&hash, &encrypted) == nil {
		if _, e := s.unseal(encrypted, session.Owner); e == nil {
			usable++
		}
	}

	if usable == 0 {
		return &OwnerActionError{Path: "security.passkeys", Reason: "last_factor", Conflict: true}
	}
	result, e := tx.ExecContext(ctx, "UPDATE passkeys SET revoked_at=? WHERE id=? AND owner_id=? AND revoked_at IS NULL", s.now(), id, session.Owner)
	if e != nil {
		return e
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return ErrConflict
	}
	if e = s.invalidate(ctx, tx); e != nil {
		return e
	}
	if e = s.audit(ctx, tx, "passkey_revoked", request); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Service) security(w http.ResponseWriter, r *http.Request, session Session) bool {
	if s.oauthSecurity(w, r, session) {
		return true
	}
	if s.publisherSecurity(w, r, session) {
		return true
	}
	p := r.URL.Path
	ctx := r.Context()
	request := w.Header().Get("X-Request-ID")
	if p == "/api/v1/security/passkeys" && r.Method == "GET" {
		rows, e := s.DB.QueryContext(ctx, "SELECT id,label,created_at,last_used_at,tested_at FROM passkeys WHERE owner_id=? AND revoked_at IS NULL ORDER BY created_at", session.Owner)
		if e != nil {
			fail(w, e)
			return true
		}
		defer rows.Close()
		keys := []PasskeyInfo{}
		for rows.Next() {
			var k PasskeyInfo
			if e = rows.Scan(&k.ID, &k.Label, &k.Created, &k.LastUsed, &k.Tested); e != nil {
				fail(w, e)
				return true
			}
			keys = append(keys, k)
		}
		if rows.Err() != nil {
			fail(w, rows.Err())
			return true
		}
		jsonOut(w, 200, keys)
		return true
	}
	if p == "/api/v1/security/passkeys" && (r.Method == "PATCH" || r.Method == "DELETE") {
		var in struct {
			ID    string `json:"id"`
			Label string `json:"label"`
		}
		if !decode(w, r, &in) {
			return true
		}
		if r.Method == "DELETE" {
			if e := s.revokeKey(ctx, session, in.ID, request); e != nil {
				fail(w, e)
				return true
			}
			setCookie(w, ownerCookie, "", -1)
		} else {
			if utf8.RuneCountInString(strings.TrimSpace(in.Label)) < 1 || utf8.RuneCountInString(in.Label) > 120 {
				fail(w, actionField("security.label", "passkey_label"))
				return true
			}
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
			result, e := tx.ExecContext(ctx, "UPDATE passkeys SET label=? WHERE id=? AND owner_id=? AND revoked_at IS NULL", strings.TrimSpace(in.Label), in.ID, session.Owner)
			if e != nil {
				fail(w, e)
				return true
			}
			n, _ := result.RowsAffected()
			if n != 1 {
				fail(w, ErrConflict)
				return true
			}
			if e = s.audit(ctx, tx, "passkey_renamed", request); e == nil {
				e = tx.Commit()
			}
			if e != nil {
				fail(w, e)
				return true
			}
		}
		jsonOut(w, 200, map[string]bool{"ok": true})
		return true
	}
	if p == "/api/v1/security/sessions" && r.Method == "GET" {
		rows, e := s.DB.QueryContext(ctx, "SELECT id,auth_method,authenticated_at,created_at,expires_at FROM sessions WHERE owner_id=? AND revoked_at IS NULL AND expires_at>? AND coalesce(last_seen_at,created_at)>? ORDER BY created_at DESC LIMIT 100", session.Owner, s.now(), s.nowIdle())
		if e != nil {
			fail(w, e)
			return true
		}
		defer rows.Close()
		items := []Session{}
		for rows.Next() {
			var item Session
			if e = rows.Scan(&item.ID, &item.Method, &item.Authenticated, &item.Created, &item.Expires); e != nil {
				fail(w, e)
				return true
			}
			items = append(items, item)
		}
		if e = rows.Err(); e != nil {
			fail(w, e)
			return true
		}
		jsonOut(w, 200, items)
		return true
	}
	if p == "/api/v1/security/sessions" && r.Method == "DELETE" {
		var in struct {
			ID string `json:"id"`
		}
		if !decode(w, r, &in) {
			return true
		}
		query := "UPDATE sessions SET revoked_at=? WHERE owner_id=?"
		args := []any{s.now(), session.Owner}
		if in.ID != "" {
			query += " AND id=?"
			args = append(args, in.ID)
		}
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
		if _, e = tx.ExecContext(ctx, query, args...); e == nil {
			e = s.audit(ctx, tx, "sessions_revoked", request)
		}
		if e == nil {
			e = tx.Commit()
		}
		if e != nil {
			fail(w, e)
			return true
		}
		if in.ID == "" || in.ID == session.ID {
			setCookie(w, ownerCookie, "", -1)
		}
		jsonOut(w, 200, map[string]bool{"ok": true})
		return true
	}
	if p == "/api/v1/security/events" && r.Method == "GET" {
		rows, e := s.DB.QueryContext(ctx, "SELECT event,request_id,created_at FROM security_events ORDER BY id DESC LIMIT 100")
		if e != nil {
			fail(w, e)
			return true
		}
		defer rows.Close()
		items := []map[string]string{}
		for rows.Next() {
			var event, request, created string
			if rows.Scan(&event, &request, &created) != nil {
				fail(w, ErrUnavailable)
				return true
			}
			items = append(items, map[string]string{"event": event, "request_id": request, "created_at": created})
		}
		if e = rows.Err(); e != nil {
			fail(w, e)
			return true
		}
		jsonOut(w, 200, items)
		return true
	}
	return s.securityFallback(w, r, session)
}
