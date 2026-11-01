// Package auth owns the singleton owner, restricted ceremonies and strong sessions.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/mbougarne/modawana/backend/internal/config"
	"github.com/mbougarne/modawana/backend/internal/db"
)

var ErrAuth = errors.New("authentication failed")
var ErrForbidden = errors.New("access denied")
var ErrRate = errors.New("try again later")
var ErrConflict = errors.New("security state changed")
var ErrUnavailable = errors.New("authentication unavailable")

type Service struct {
	DB       *sql.DB
	Config   config.Config
	Now      func() time.Time
	hashGate chan struct{}
}

func New(d *sql.DB, c config.Config) *Service {
	return &Service{DB: d, Config: c, Now: time.Now, hashGate: make(chan struct{}, 2)}
}
func secret() (string, error) {
	var b [32]byte
	if _, e := rand.Read(b[:]); e != nil {
		return "", e
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}
func digest(s string) string   { sum := sha256.Sum256([]byte(s)); return hex.EncodeToString(sum[:]) }
func csrf(token string) string { return digest("csrf:" + token) }
func same(a, b string) bool {
	return len(a) > 0 && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
func validSecret(s string) bool {
	b, e := base64.RawURLEncoding.DecodeString(s)
	return e == nil && len(b) == 32
}
func (s *Service) now() string { return db.UTC(s.Now()) }

type executor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func (s *Service) audit(ctx context.Context, q executor, event, request string) error {
	if _, e := q.ExecContext(ctx, "INSERT INTO security_events(event,request_id,created_at) VALUES(?,?,?)", event, request, s.now()); e != nil {
		return e
	}
	_, e := q.ExecContext(ctx, "DELETE FROM security_events WHERE id NOT IN (SELECT id FROM security_events ORDER BY id DESC LIMIT 1000)")
	return e
}

// Grant is only called by a local operator command, never by an HTTP route.
func (s *Service) Grant(ctx context.Context, recovery bool) (string, error) {
	raw, e := secret()
	if e != nil {
		return "", e
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return "", e
	}
	defer tx.Rollback()
	var count int
	if e = tx.QueryRowContext(ctx, "SELECT count(*) FROM owner").Scan(&count); e != nil {
		return "", e
	}
	purpose := "bootstrap"
	if recovery {
		purpose = "recovery"
		if count != 1 {
			return "", ErrAuth
		}
		if e = s.invalidate(ctx, tx); e != nil {
			return "", e
		}
	} else if count != 0 {
		return "", ErrConflict
	}
	if _, e = tx.ExecContext(ctx, "DELETE FROM auth_grants"); e != nil {
		return "", e
	}
	if _, e = tx.ExecContext(ctx, "INSERT INTO auth_grants(token_hash,purpose,expires_at) VALUES(?,?,?)", digest(raw), purpose, db.UTC(s.Now().Add(10*time.Minute))); e != nil {
		return "", e
	}
	if e = s.audit(ctx, tx, "local_"+purpose, ""); e != nil {
		return "", e
	}
	if e = tx.Commit(); e != nil {
		return "", e
	}
	return raw, nil
}
func (s *Service) invalidate(ctx context.Context, tx *sql.Tx) error {
	for _, query := range []string{"UPDATE owner SET auth_version=auth_version+1", "DELETE FROM auth_flows", "UPDATE sessions SET revoked_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE revoked_at IS NULL"} {
		if _, e := tx.ExecContext(ctx, query); e != nil {
			return e
		}
	}
	return nil
}

type Flow struct {
	Hash, Kind, Owner   string
	Version             int64
	Challenge, Ceremony string
}

func (s *Service) flow(ctx context.Context, raw string) (Flow, error) {
	var f Flow
	if !validSecret(raw) {
		return f, ErrAuth
	}
	e := s.DB.QueryRowContext(ctx, `SELECT token_hash,kind,coalesce(owner_id,''),coalesce(auth_version,0),coalesce(challenge,''),coalesce(ceremony,'') FROM auth_flows WHERE token_hash=? AND consumed_at IS NULL AND expires_at>? AND (owner_id IS NULL OR auth_version=(SELECT auth_version FROM owner WHERE id=owner_id))`, digest(raw), s.now()).Scan(&f.Hash, &f.Kind, &f.Owner, &f.Version, &f.Challenge, &f.Ceremony)
	if e != nil {
		return Flow{}, ErrAuth
	}
	return f, nil
}
func (s *Service) newFlow(ctx context.Context, q executor, kind, owner string, version int64) (string, error) {
	raw, e := secret()
	if e != nil {
		return "", e
	}
	var own any
	var v any
	if owner != "" {
		own = owner
		v = version
	}
	_, e = q.ExecContext(ctx, "INSERT INTO auth_flows(token_hash,kind,owner_id,auth_version,expires_at) VALUES(?,?,?,?,?)", digest(raw), kind, own, v, db.UTC(s.Now().Add(5*time.Minute)))
	return raw, e
}
func (s *Service) Start(ctx context.Context, ip string) (string, error) {
	if e := s.limit(ctx, "start:"+ip, 30); e != nil {
		return "", e
	}
	// Rows are bounded by their short lifetime; no secret or caller label is logged.
	if _, e := s.DB.ExecContext(ctx, "DELETE FROM auth_flows WHERE expires_at<=? OR consumed_at IS NOT NULL", s.now()); e != nil {
		return "", e
	}
	return s.newFlow(ctx, s.DB, "anonymous", "", 0)
}
func (s *Service) Redeem(ctx context.Context, flowRaw, grant, email, request string) (string, error) {
	f, e := s.flow(ctx, flowRaw)
	if e != nil || f.Kind != "anonymous" || !validSecret(grant) {
		return "", ErrAuth
	}
	addr, e := mail.ParseAddress(email)
	if e != nil || addr.Address != email || len(email) > 254 {
		return "", ErrAuth
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return "", e
	}
	defer tx.Rollback()
	var purpose string
	if e = tx.QueryRowContext(ctx, "SELECT purpose FROM auth_grants WHERE token_hash=? AND consumed_at IS NULL AND expires_at>?", digest(grant), s.now()).Scan(&purpose); e != nil {
		return "", ErrAuth
	}
	var owner string
	var version int64
	if purpose == "bootstrap" {
		owner, e = db.NewID()
		if e != nil {
			return "", e
		}
		version = 1
		if _, e = tx.ExecContext(ctx, "INSERT INTO owner(id,login_email) VALUES(?,?)", owner, strings.ToLower(email)); e != nil {
			return "", ErrAuth
		}
	} else {
		if e = tx.QueryRowContext(ctx, "SELECT id,auth_version FROM owner WHERE login_email=?", strings.ToLower(email)).Scan(&owner, &version); e != nil {
			return "", ErrAuth
		}
	}
	if e = consumeFlow(ctx, tx, f.Hash); e != nil {
		return "", e
	}
	if _, e = tx.ExecContext(ctx, "UPDATE auth_grants SET consumed_at=? WHERE token_hash=?", s.now(), digest(grant)); e != nil {
		return "", e
	}
	raw, e := s.newFlow(ctx, tx, purpose, owner, version)
	if e != nil {
		return "", e
	}
	if e = s.audit(ctx, tx, purpose+"_grant_redeemed", request); e != nil {
		return "", e
	}
	if e = tx.Commit(); e != nil {
		return "", e
	}
	return raw, nil
}
func consumeFlow(ctx context.Context, tx *sql.Tx, hash string) error {
	r, e := tx.ExecContext(ctx, "UPDATE auth_flows SET consumed_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE token_hash=? AND consumed_at IS NULL", hash)
	if e != nil {
		return e
	}
	n, e := r.RowsAffected()
	if e != nil || n != 1 {
		return ErrAuth
	}
	return nil
}

type Session struct {
	ID            string `json:"id"`
	Owner         string `json:"-"`
	Method        string `json:"method"`
	Authenticated string `json:"authenticated_at"`
	Created       string `json:"created_at"`
	Expires       string `json:"expires_at"`
	Credential    string `json:"-"`
	Version       int64  `json:"-"`
}

func (s *Service) Session(ctx context.Context, raw string, fresh bool) (Session, error) {
	var out Session
	if !validSecret(raw) {
		return out, ErrAuth
	}
	e := s.DB.QueryRowContext(ctx, `SELECT s.id,s.owner_id,s.auth_method,s.authenticated_at,s.created_at,s.expires_at,coalesce(s.credential_id,''),s.auth_version FROM sessions s JOIN owner o ON o.id=s.owner_id AND o.auth_version=s.auth_version WHERE s.token_hash=? AND s.kind='owner' AND s.auth_method IN ('passkey','password_totp') AND s.revoked_at IS NULL AND s.expires_at>? AND coalesce(s.last_seen_at,s.created_at)>?`, digest(raw), s.now(), db.UTC(s.Now().Add(-2*time.Hour))).Scan(&out.ID, &out.Owner, &out.Method, &out.Authenticated, &out.Created, &out.Expires, &out.Credential, &out.Version)
	if e != nil {
		return Session{}, ErrAuth
	}
	if fresh && out.Authenticated < db.UTC(s.Now().Add(-30*time.Minute)) {
		return Session{}, ErrForbidden
	}
	result, e := s.DB.ExecContext(ctx, "UPDATE sessions SET last_seen_at=? WHERE id=? AND revoked_at IS NULL", s.now(), out.ID)
	if e != nil {
		return Session{}, e
	}
	n, e := result.RowsAffected()
	if e != nil || n != 1 {
		return Session{}, ErrAuth
	}
	return out, nil
}

// complete is unexported: only verified strong-factor ceremonies may call it.
func (s *Service) complete(ctx context.Context, tx *sql.Tx, owner, method, credential, old, request string) (string, error) {
	raw, e := secret()
	if e != nil {
		return "", e
	}
	id, e := db.NewID()
	if e != nil {
		return "", e
	}
	var version int64
	if e = tx.QueryRowContext(ctx, "SELECT auth_version FROM owner WHERE id=?", owner).Scan(&version); e != nil {
		return "", e
	}
	if old != "" {
		if _, e = tx.ExecContext(ctx, "UPDATE sessions SET revoked_at=? WHERE token_hash=?", s.now(), digest(old)); e != nil {
			return "", e
		}
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO sessions(id,owner_id,token_hash,kind,auth_method,auth_version,authenticated_at,created_at,expires_at,last_seen_at,credential_id) VALUES(?,?,?,'owner',?,?,?,?,?,?,?)`, id, owner, digest(raw), method, version, s.now(), s.now(), db.UTC(s.Now().Add(12*time.Hour)), s.now(), credential)
	if e != nil {
		return "", e
	}
	if e = s.audit(ctx, tx, "login_"+method, request); e != nil {
		return "", e
	}
	return raw, nil
}
func (s *Service) limit(ctx context.Context, key string, max int) error {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, "DELETE FROM auth_throttles WHERE expires_at<=?", s.now()); e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO auth_throttles(bucket,attempts,expires_at) VALUES(?,1,?) ON CONFLICT(bucket) DO UPDATE SET attempts=attempts+1`, digest(key), db.UTC(s.Now().Add(15*time.Minute)))
	if e != nil {
		return e
	}
	var n int
	if e = tx.QueryRowContext(ctx, "SELECT attempts FROM auth_throttles WHERE bucket=?", digest(key)).Scan(&n); e != nil {
		return e
	}
	// Table pressure cannot impose a global authentication ban. Retain the
	// caller and the singleton account counter while pruning old other buckets.
	if _, e = tx.ExecContext(ctx, `DELETE FROM auth_throttles WHERE bucket IN (SELECT bucket FROM auth_throttles WHERE bucket NOT IN (?,?) ORDER BY expires_at,bucket LIMIT max(0,(SELECT count(*) FROM auth_throttles)-10000))`, digest(key), digest("failed_account")); e != nil {
		return e
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	if n > max {
		return ErrRate
	}
	return nil
}

func (s *Service) nowIdle() string { return db.UTC(s.Now().Add(-2 * time.Hour)) }

func (s *Service) Authorized(r *http.Request) bool {
	_, e := s.Session(r.Context(), cookie(r, ownerCookie), false)
	return e == nil
}

func (s *Service) hashSlot() (func(), error) {
	select {
	case s.hashGate <- struct{}{}:
		return func() { <-s.hashGate }, nil
	default:
		return nil, ErrRate
	}
}
