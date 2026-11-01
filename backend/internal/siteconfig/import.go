package siteconfig

import (
	"context"
	"encoding/json"
	"github.com/mbougarne/modawana/backend/internal/db"
)

// StageImport saves a validated history revision without applying or reviewing it.
// The About document may still be a draft; Apply requires it to be published later.
func (s *Service) StageImport(ctx context.Context, owner, kind string, input json.RawMessage) error {
	b, refs, e := validate(kind, input)
	if e != nil {
		return e
	}
	check := b
	if kind == "profile" {
		var p Profile
		if json.Unmarshal(b, &p) != nil {
			return ErrInvalid
		}
		if p.AboutKey != "" {
			var n int
			if e = s.DB.QueryRowContext(ctx, "SELECT count(*) FROM content_items WHERE key=? AND kind='doc'", p.AboutKey).Scan(&n); e != nil || n != 1 {
				return ErrInvalid
			}
			p.AboutKey = ""
			check = raw(p)
		}
	}
	if e = s.validateRefs(ctx, s.DB, kind, check, refs); e != nil {
		return e
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	doc, e := db.NewID()
	if e != nil {
		return e
	}
	id, e := db.NewID()
	if e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, "INSERT INTO config_documents(id,kind) VALUES(?,?)", doc, kind); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, "INSERT INTO config_revisions(id,document_id,snapshot_json,schema_version,colour_algorithm_version,created_by) VALUES(?,?,?,1,1,?)", id, doc, string(b), owner); e != nil {
		return e
	}
	for _, r := range refs {
		if r.ID != "" {
			if _, e = tx.ExecContext(ctx, "INSERT INTO config_revision_media(revision_id,media_id,role,public) VALUES(?,?,?,0)", id, r.ID, r.Purpose); e != nil {
				return e
			}
		}
	}
	return tx.Commit()
}
