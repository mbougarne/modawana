package learning

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"errors"
	"github.com/mbougarne/modawana/backend/internal/db"
	"sync"
	"time"
)

var ErrIdentity = errors.New("learning identity unavailable")
var ErrLimited = errors.New("learning creation limited")

const RetainedAttempts = 100
const AttemptDays = 30

type IdentityService struct {
	DB            *sql.DB
	IdleDays      int
	MaxIdentities int
	Now           func() time.Time
	mu            sync.Mutex
	buckets       map[string]bucket
}
type bucket struct {
	start time.Time
	count int
}
type Identity struct {
	ID      string
	Expires string
}

func NewIdentity(d *sql.DB, days, max int) *IdentityService {
	if days <= 0 {
		days = 180
	}
	if max <= 0 {
		max = 10000
	}
	return &IdentityService{DB: d, IdleDays: days, MaxIdentities: max, Now: time.Now, buckets: map[string]bucket{}}
}
func validToken(raw string) bool {
	b, e := base64.RawURLEncoding.DecodeString(raw)
	return e == nil && len(b) == 32 && len(raw) == 43
}
func LearningCSRF(raw string) string { return digest([]byte("learning-csrf:" + raw)) }
func CheckCSRF(raw, csrf string) bool {
	return validToken(raw) && subtle.ConstantTimeCompare([]byte(LearningCSRF(raw)), []byte(csrf)) == 1
}
func (s *IdentityService) allow(ip string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.Now()
	for key, b := range s.buckets {
		if now.Sub(b.start) >= time.Minute {
			delete(s.buckets, key)
		}
	}
	key := digest([]byte(ip))
	b := s.buckets[key]
	if b.count >= 10 || b.count == 0 && len(s.buckets) >= 4096 {
		return false
	}
	if b.count == 0 {
		b.start = now
	}
	b.count++
	s.buckets[key] = b
	return true
}
func (s *IdentityService) Create(ctx context.Context, ip string) (string, Identity, error) {
	if !s.allow(ip) {
		return "", Identity{}, ErrLimited
	}

	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return "", Identity{}, e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, "DELETE FROM learning_identities WHERE id IN (SELECT id FROM learning_identities WHERE expires_at<=? ORDER BY expires_at LIMIT 100)", db.UTC(s.Now())); e != nil {
		return "", Identity{}, e
	}
	var n int
	if e = tx.QueryRowContext(ctx, "SELECT count(*) FROM learning_identities").Scan(&n); e != nil {
		return "", Identity{}, e
	}
	if n >= s.MaxIdentities {
		return "", Identity{}, ErrLimited
	}
	bytes := make([]byte, 32)
	if _, e = rand.Read(bytes); e != nil {
		return "", Identity{}, e
	}
	raw := base64.RawURLEncoding.EncodeToString(bytes)
	id, e := db.NewID()
	if e != nil {
		return "", Identity{}, e
	}
	now := s.Now()
	out := Identity{ID: id, Expires: db.UTC(now.Add(time.Duration(s.IdleDays) * 24 * time.Hour))}
	_, e = tx.ExecContext(ctx, "INSERT INTO learning_identities(id,token_hash,last_seen_at,expires_at,created_at) VALUES(?,?,?,?,?)", id, digest([]byte(raw)), db.UTC(now), out.Expires, db.UTC(now))
	if e != nil {
		return "", Identity{}, e
	}
	return raw, out, tx.Commit()
}
func (s *IdentityService) Current(ctx context.Context, raw string, touch bool) (Identity, error) {
	var out Identity
	if !validToken(raw) {
		return out, ErrIdentity
	}
	now := s.Now()
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	e = tx.QueryRowContext(ctx, "SELECT id,expires_at FROM learning_identities WHERE token_hash=? AND expires_at>?", digest([]byte(raw)), db.UTC(now)).Scan(&out.ID, &out.Expires)
	if errors.Is(e, sql.ErrNoRows) {
		return out, ErrIdentity
	}
	if e != nil {
		return out, e
	}
	if touch {
		out.Expires = db.UTC(now.Add(time.Duration(s.IdleDays) * 24 * time.Hour))
		if _, e = tx.ExecContext(ctx, "UPDATE learning_identities SET last_seen_at=?,expires_at=? WHERE id=?", db.UTC(now), out.Expires, out.ID); e != nil {
			return out, e
		}
	}
	return out, tx.Commit()
}
func (s *IdentityService) Reset(ctx context.Context, raw string) error {
	if !validToken(raw) {
		return ErrIdentity
	}
	result, e := s.DB.ExecContext(ctx, "DELETE FROM learning_identities WHERE token_hash=? AND expires_at>?", digest([]byte(raw)), db.UTC(s.Now()))
	if e != nil {
		return e
	}
	n, e := result.RowsAffected()
	if e == nil && n != 1 {
		return ErrIdentity
	}
	return e
}

// Cleanup batches expiry and attempt pruning; identity deletion cascades retained results.
func (s *IdentityService) Cleanup(ctx context.Context) (int64, error) {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return 0, e
	}
	defer tx.Rollback()
	now := s.Now()
	var total int64
	for _, statement := range []struct {
		sql string
		arg string
	}{
		{"DELETE FROM learning_identities WHERE id IN (SELECT id FROM learning_identities WHERE expires_at<=? ORDER BY expires_at LIMIT 500)", db.UTC(now)},
		{"DELETE FROM learning_attempts WHERE id IN (SELECT id FROM learning_attempts WHERE created_at<? ORDER BY created_at LIMIT 500)", db.UTC(now.Add(-AttemptDays * 24 * time.Hour))},
		{"DELETE FROM learning_attempts WHERE id IN (SELECT id FROM (SELECT id,row_number() OVER(PARTITION BY identity_id ORDER BY created_at DESC,id DESC) AS n FROM learning_attempts) WHERE n>100 LIMIT 500)", ""},
	} {
		var result sql.Result
		if statement.arg == "" {
			result, e = tx.ExecContext(ctx, statement.sql)
		} else {
			result, e = tx.ExecContext(ctx, statement.sql, statement.arg)
		}
		if e != nil {
			return total, e
		}
		n, e := result.RowsAffected()
		if e != nil {
			return total, e
		}
		total += n
	}
	return total, tx.Commit()
}
