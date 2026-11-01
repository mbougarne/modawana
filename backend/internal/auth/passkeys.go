package auth

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

type ownerUser struct {
	ID          string
	Credentials []webauthn.Credential
}

func (u ownerUser) WebAuthnID() []byte                         { return []byte(u.ID) }
func (u ownerUser) WebAuthnName() string                       { return "Owner" }
func (u ownerUser) WebAuthnDisplayName() string                { return "Owner" }
func (u ownerUser) WebAuthnCredentials() []webauthn.Credential { return u.Credentials }

type querier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func loadUser(ctx context.Context, q querier, id string) (ownerUser, error) {
	u := ownerUser{}
	query := "SELECT id FROM owner"
	var args []any
	if id != "" {
		query += " WHERE id=?"
		args = append(args, id)
	}
	if e := q.QueryRowContext(ctx, query, args...).Scan(&u.ID); e != nil {
		return u, ErrAuth
	}
	rows, e := q.QueryContext(ctx, "SELECT credential FROM passkeys WHERE owner_id=? AND revoked_at IS NULL", u.ID)
	if e != nil {
		return u, e
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		var c webauthn.Credential
		if rows.Scan(&raw) != nil || json.Unmarshal([]byte(raw), &c) != nil {
			return u, ErrAuth
		}
		u.Credentials = append(u.Credentials, c)
	}
	return u, rows.Err()
}
func (s *Service) webauthn() (*webauthn.WebAuthn, error) {
	return webauthn.New(&webauthn.Config{RPID: s.Config.RPID, RPDisplayName: "Personal blog", RPOrigins: s.Config.WebAuthnOrigins, AttestationPreference: protocol.PreferNoAttestation, AuthenticatorSelection: protocol.AuthenticatorSelection{ResidentKey: protocol.ResidentKeyRequirementRequired, UserVerification: protocol.VerificationRequired}, Timeouts: webauthn.TimeoutsConfig{Login: webauthn.TimeoutConfig{Enforce: true, Timeout: 5 * time.Minute}, Registration: webauthn.TimeoutConfig{Enforce: true, Timeout: 5 * time.Minute}}})
}
func (s *Service) beginPasskey(ctx context.Context, flowRaw, ownerRaw, mode string) (any, error) {
	f, e := s.flow(ctx, flowRaw)
	if e != nil {
		return nil, e
	}
	wa, e := s.webauthn()
	if e != nil {
		return nil, e
	}
	var opts any
	var data *webauthn.SessionData
	if mode == "register" {
		if f.Kind != "bootstrap" && f.Kind != "recovery" {
			session, e := s.Session(ctx, ownerRaw, true)
			if e != nil {
				return nil, e
			}
			f.Kind = "managed"
			f.Owner = session.Owner
			f.Version = session.Version
			if _, e = s.DB.ExecContext(ctx, "UPDATE auth_flows SET kind='managed',owner_id=?,auth_version=?,session_hash=? WHERE token_hash=? AND consumed_at IS NULL", f.Owner, f.Version, digest(ownerRaw), f.Hash); e != nil {
				return nil, e
			}
		}
		u, e := loadUser(ctx, s.DB, f.Owner)
		if e != nil {
			return nil, e
		}
		if len(u.Credentials) >= 20 {
			return nil, ErrConflict
		}
		descriptors := []protocol.CredentialDescriptor{}
		for _, c := range u.Credentials {
			descriptors = append(descriptors, c.Descriptor())
		}
		opts, data, e = wa.BeginRegistration(u, webauthn.WithExclusions(descriptors), webauthn.WithRegistrationOrigin(s.Config.BaseURL))
	} else {
		if f.Kind != "anonymous" {
			return nil, ErrAuth
		}
		opts, data, e = wa.BeginDiscoverableLogin(webauthn.WithUserVerification(protocol.VerificationRequired), webauthn.WithLoginOrigin(s.Config.BaseURL))
	}
	if e != nil {
		return nil, ErrAuth
	}
	encoded, e := json.Marshal(data)
	if e != nil {
		return nil, e
	}
	result, e := s.DB.ExecContext(ctx, "UPDATE auth_flows SET challenge=?,ceremony=? WHERE token_hash=? AND consumed_at IS NULL AND expires_at>?", string(encoded), mode, f.Hash, s.now())
	if e != nil {
		return nil, e
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return nil, ErrAuth
	}
	return opts, nil
}
func (s *Service) claim(ctx context.Context, raw, mode string) (Flow, webauthn.SessionData, error) {
	f, e := s.flow(ctx, raw)
	var data webauthn.SessionData
	if e != nil || f.Ceremony != mode || f.Challenge == "" {
		return f, data, ErrAuth
	}
	result, e := s.DB.ExecContext(ctx, "UPDATE auth_flows SET challenge=NULL,ceremony=NULL WHERE token_hash=? AND challenge=? AND consumed_at IS NULL AND expires_at>?", f.Hash, f.Challenge, s.now())
	if e != nil {
		return f, data, e
	}
	n, _ := result.RowsAffected()
	if n != 1 || json.Unmarshal([]byte(f.Challenge), &data) != nil {
		return f, data, ErrAuth
	}
	return f, data, nil
}
func (s *Service) finishRegister(r *http.Request, label string) error {
	ctx := r.Context()
	f, data, e := s.claim(ctx, cookie(r, flowCookie), "register")
	if e != nil {
		return e
	}
	if utf8.RuneCountInString(strings.TrimSpace(label)) < 1 || utf8.RuneCountInString(label) > 120 {
		return actionField("security.label", "passkey_label")
	}
	var managed Session
	if f.Kind == "managed" {
		session, e := s.Session(ctx, cookie(r, ownerCookie), true)
		if e != nil || session.Owner != f.Owner {
			return ErrAuth
		}
		managed = session
		var binding string
		if s.DB.QueryRowContext(ctx, "SELECT session_hash FROM auth_flows WHERE token_hash=?", f.Hash).Scan(&binding) != nil || binding != digest(cookie(r, ownerCookie)) {
			return ErrAuth
		}
	}
	if f.Kind != "managed" && f.Kind != "bootstrap" && f.Kind != "recovery" {
		return ErrAuth
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if f.Kind == "managed" {
		if e = s.freshInTx(ctx, tx, managed); e != nil {
			return e
		}
	}
	u, e := loadUser(ctx, tx, f.Owner)
	if e != nil {
		return e
	}
	if len(u.Credentials) >= 20 {
		return ErrConflict
	}
	var version int64
	if tx.QueryRowContext(ctx, "SELECT auth_version FROM owner WHERE id=?", f.Owner).Scan(&version) != nil || version != f.Version {
		return ErrAuth
	}
	wa, e := s.webauthn()
	if e != nil {
		return e
	}
	credential, e := wa.FinishRegistration(u, data, r)
	if e != nil || !credential.Flags.UserVerified {
		return ErrAuth
	}
	encoded, e := json.Marshal(credential)
	if e != nil {
		return e
	}
	recovering := 0
	if f.Kind == "recovery" {
		recovering = 1
	}
	if _, e = tx.ExecContext(ctx, "INSERT INTO passkeys(id,owner_id,credential,label,created_at,recovery_pending) VALUES(?,?,?,?,?,?)", base64.RawURLEncoding.EncodeToString(credential.ID), u.ID, string(encoded), strings.TrimSpace(label), s.now(), recovering); e != nil {
		return ErrAuth
	}
	if e = consumeFlow(ctx, tx, f.Hash); e != nil {
		return e
	}
	if e = s.audit(ctx, tx, "passkey_registered", r.Header.Get("X-Request-ID")); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Service) finishLogin(r *http.Request) (string, error) {
	ctx := r.Context()
	f, data, e := s.claim(ctx, cookie(r, flowCookie), "login")
	if e != nil || f.Kind != "anonymous" {
		return "", ErrAuth
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return "", e
	}
	defer tx.Rollback()
	u, e := loadUser(ctx, tx, "")
	if e != nil {
		return "", ErrAuth
	}
	wa, e := s.webauthn()
	if e != nil {
		return "", e
	}
	credential, e := wa.FinishDiscoverableLogin(func(id, handle []byte) (webauthn.User, error) {
		if !same(string(handle), u.ID) {
			return nil, ErrAuth
		}
		for _, c := range u.Credentials {
			if same(string(c.ID), string(id)) {
				return u, nil
			}
		}
		return nil, ErrAuth
	}, data, r)
	if e != nil || credential.Authenticator.CloneWarning {
		return "", ErrAuth
	}
	key := base64.RawURLEncoding.EncodeToString(credential.ID)
	encoded, e := json.Marshal(credential)
	if e != nil {
		return "", e
	}
	var recovering int
	if e = tx.QueryRowContext(ctx, "SELECT recovery_pending FROM passkeys WHERE id=? AND revoked_at IS NULL", key).Scan(&recovering); e != nil {
		return "", ErrAuth
	}
	if e = consumeFlow(ctx, tx, f.Hash); e != nil {
		return "", e
	}
	if recovering == 1 {
		if e = s.invalidate(ctx, tx); e != nil {
			return "", e
		}
		for _, q := range []string{"UPDATE passkeys SET revoked_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id!=? AND revoked_at IS NULL", "UPDATE owner SET password_hash=NULL,totp_cipher=NULL,totp_step=-1 WHERE singleton=1 AND ? IS NOT NULL", "DELETE FROM recovery_codes WHERE ? IS NOT NULL"} {
			if _, e = tx.ExecContext(ctx, q, key); e != nil {
				return "", e
			}
		}
		if e = s.audit(ctx, tx, "recovery_completed", r.Header.Get("X-Request-ID")); e != nil {
			return "", e
		}
	}
	if _, e = tx.ExecContext(ctx, "UPDATE passkeys SET credential=?,last_used_at=?,tested_at=?,recovery_pending=0 WHERE id=?", string(encoded), s.now(), s.now(), key); e != nil {
		return "", e
	}
	raw, e := s.complete(ctx, tx, u.ID, "passkey", key, cookie(r, ownerCookie), r.Header.Get("X-Request-ID"))
	if e != nil {
		return "", e
	}
	if e = tx.Commit(); e != nil {
		return "", e
	}
	return raw, nil
}
func (s *Service) ceremony(w http.ResponseWriter, r *http.Request) bool {
	p := r.URL.Path
	if !strings.HasPrefix(p, "/api/v1/auth/passkeys/") {
		return s.fallback(w, r)
	}
	if e := s.checkFailures(r); e != nil {
		fail(w, e)
		return true
	}
	if r.Method != "POST" {
		fail(w, ErrAuth)
		return true
	}
	ip := s.Config.ThrottleIP(r.RemoteAddr, r.Header.Get("X-Forwarded-For"))
	if e := s.limit(r.Context(), "passkey:"+ip, 30); e != nil {
		fail(w, e)
		return true
	}
	mode := "login"
	if strings.Contains(p, "/register/") {
		mode = "register"
	}
	if p == "/api/v1/auth/passkeys/"+mode+"/options" {
		opts, e := s.beginPasskey(r.Context(), cookie(r, flowCookie), cookie(r, ownerCookie), mode)
		if e != nil {
			fail(w, e)
			return true
		}
		jsonOut(w, 200, opts)
		return true
	}
	if p != "/api/v1/auth/passkeys/"+mode+"/verify" {
		fail(w, ErrAuth)
		return true
	}
	// The library owns the credential JSON schema; limit bytes before parsing it.
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	var e error
	if mode == "register" {
		label, err := url.PathUnescape(r.Header.Get("X-Passkey-Label"))
		if err != nil {
			fail(w, ErrAuth)
			return true
		}
		e = s.finishRegister(r, label)
		if e == nil {
			setCookie(w, flowCookie, "", -1)
			jsonOut(w, 200, map[string]string{"state": "sign_in_to_test"})
			return true
		}
	} else {
		var raw string
		raw, e = s.finishLogin(r)
		if e == nil {
			setCookie(w, ownerCookie, raw, 43200)
			setCookie(w, flowCookie, "", -1)
			jsonOut(w, 200, map[string]string{"state": "owner", "csrf": csrf(raw)})
			return true
		}
	}
	if failureError := s.failure(r, "passkey_failed"); failureError != nil {
		e = failureError
	}
	fail(w, e)
	return true
}
