package operations

import (
	"context"
	"database/sql"
	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/learning"
	"github.com/mbougarne/modawana/backend/internal/storage"
	"time"
)

type MaintenanceResult struct {
	Rows  int64 `json:"rows"`
	Files int   `json:"files"`
}

func Cleanup(ctx context.Context, d *sql.DB, files *storage.Store) (MaintenanceResult, error) {
	var result MaintenanceResult
	ids := learning.NewIdentity(d, 180, 10000)
	n, e := ids.Cleanup(ctx)
	result.Rows = n
	if e != nil {
		return result, e
	}
	now := db.UTC(time.Now())
	cutoff := db.UTC(time.Now().Add(-180 * 24 * time.Hour))
	for _, v := range []struct{ table, key, condition, value string }{{"oauth_requests", "id", "expires_at<=?", now}, {"oauth_grants", "id", "expires_at<=?", now}, {"oauth_clients", "id", "expires_at<=?", now}, {"auth_flows", "token_hash", "expires_at<=?", now}, {"auth_grants", "token_hash", "expires_at<=?", now}, {"auth_throttles", "bucket", "expires_at<=?", now}, {"sessions", "id", "expires_at<=?", now}, {"security_events", "id", "created_at<?", cutoff}, {"config_audit", "id", "created_at<?", cutoff}, {"idempotency_records", "rowid", "expires_at<=?", now}} {
		r, e := d.ExecContext(ctx, "DELETE FROM "+v.table+" WHERE "+v.key+" IN (SELECT "+v.key+" FROM "+v.table+" WHERE "+v.condition+" LIMIT 500)", v.value)
		if e != nil {
			return result, e
		}
		n, _ := r.RowsAffected()
		result.Rows += n
	}
	e = files.Exclusive(ctx, func() error {
		refs, e := References(ctx, d)
		if e != nil {
			return e
		}
		result.Files, e = files.CleanupOrphansLocked(ctx, refs, time.Now().Add(-24*time.Hour))
		return e
	})
	return result, e
}
