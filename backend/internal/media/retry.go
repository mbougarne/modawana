package media

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/mbougarne/modawana/backend/internal/db"
	"regexp"
	"time"
)

var ErrIdempotency = errors.New("media idempotency conflict")
var retryKeyPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)

func requestDigest(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func uploadRetry(ctx context.Context, tx *sql.Tx, actor, key, hash string) (Record, bool, error) {
	var out Record
	if key == "" {
		return out, false, nil
	}
	if !retryKeyPattern.MatchString(key) {
		return out, false, ErrInvalid
	}
	var previous, id string
	e := tx.QueryRowContext(ctx, "SELECT request_hash,result_id FROM idempotency_records WHERE actor_id=? AND operation='media' AND key_hash=? AND expires_at>?", actor, requestDigest(key), db.UTC(time.Now())).Scan(&previous, &id)
	if errors.Is(e, sql.ErrNoRows) {
		return out, false, nil
	}
	if e != nil {
		return out, false, e
	}
	if previous != hash {
		return out, false, ErrIdempotency
	}
	out, e = Get(ctx, tx, id)
	return out, true, e
}
func rememberUpload(ctx context.Context, tx *sql.Tx, actor, key, hash, id string) error {
	if key == "" {
		return nil
	}
	_, e := tx.ExecContext(ctx, `INSERT INTO idempotency_records(actor_id,operation,key_hash,request_hash,http_status,result_id,result_version,expires_at) VALUES(?,'media',?,?,201,?,1,?) ON CONFLICT(actor_id,operation,key_hash) DO UPDATE SET request_hash=excluded.request_hash,result_id=excluded.result_id,expires_at=excluded.expires_at`, actor, requestDigest(key), hash, id, db.UTC(time.Now().Add(7*24*time.Hour)))
	return e
}
