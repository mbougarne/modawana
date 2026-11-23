package dashboard

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/mbougarne/modawana/backend/internal/db"
)

type Service struct{ DB *sql.DB }

type Revision struct {
	Version int64   `json:"version"`
	Layout  *Layout `json:"layout"`
}

type State struct {
	Site     Revision `json:"site"`
	Personal Revision `json:"personal"`
	Registry Registry `json:"registry"`
}

type Input struct {
	Scope    string  `json:"scope"`
	Expected *int64  `json:"expected_version"`
	Reset    bool    `json:"reset,omitempty"`
	Layout   *Layout `json:"layout"`
}

type rowQuery interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func read(ctx context.Context, q rowQuery, scope, owner string) (Revision, error) {
	var record Revision
	var raw sql.NullString
	var err error
	if scope == "site" {
		err = q.QueryRowContext(ctx, "SELECT version,snapshot FROM dashboard_defaults WHERE id=1").Scan(&record.Version, &raw)
	} else {
		err = q.QueryRowContext(ctx, "SELECT version,snapshot FROM dashboard_preferences WHERE owner_id=?", owner).Scan(&record.Version, &raw)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return record, nil
	}
	if err == nil && raw.Valid {
		err = json.Unmarshal([]byte(raw.String), &record.Layout)
	}
	return record, err
}

func (s *Service) Get(ctx context.Context, owner string, registry Registry) (State, error) {
	out := State{Registry: registry}
	var err error
	out.Site, err = read(ctx, s.DB, "site", owner)
	if err != nil {
		return out, err
	}
	out.Personal, err = read(ctx, s.DB, "personal", owner)
	return out, err
}

func (s *Service) Save(ctx context.Context, owner string, in Input) (Revision, error) {
	if (in.Scope != "site" && in.Scope != "personal") || in.Expected == nil || *in.Expected < 0 || (in.Layout == nil) != in.Reset || Validate(in.Layout) != nil {
		return Revision{}, ErrInvalid
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Revision{}, err
	}
	defer tx.Rollback()
	current, err := read(ctx, tx, in.Scope, owner)
	if err != nil {
		return current, err
	}
	if current.Version != *in.Expected || current.Version == int64(^uint64(0)>>1) {
		return current, db.ErrVersionConflict
	}
	out := Revision{Version: current.Version + 1, Layout: in.Layout}
	var snapshot any
	if in.Layout != nil {
		raw, err := json.Marshal(in.Layout)
		if err != nil {
			return current, err
		}
		snapshot = string(raw)
	}
	if in.Scope == "site" {
		_, err = tx.ExecContext(ctx, `INSERT INTO dashboard_defaults VALUES (1,?,?)
			ON CONFLICT(id) DO UPDATE SET version=excluded.version,snapshot=excluded.snapshot`, out.Version, snapshot)
	} else {
		_, err = tx.ExecContext(ctx, `INSERT INTO dashboard_preferences VALUES (?,?,?)
			ON CONFLICT(owner_id) DO UPDATE SET version=excluded.version,snapshot=excluded.snapshot`, owner, out.Version, snapshot)
	}
	if err != nil {
		return current, err
	}
	return out, tx.Commit()
}
