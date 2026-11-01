package access

import (
	"context"
	"database/sql"
	"errors"
)

// GateCheck evaluates an active lesson plan in the same snapshot as publication.
// A missing implementation fails closed for lesson bodies, including their media.
type GateCheck func(context.Context, *sql.Tx, string, Principal) (bool, error)
type Live struct {
	DB    *sql.DB
	Gates GateCheck
}

func (s *Live) Resolve(ctx context.Context, r Request) (Decision, error) {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return Decision{}, e
	}
	defer tx.Rollback()
	return s.ResolveIn(ctx, tx, r)
}
func (s *Live) ResolveIn(ctx context.Context, tx *sql.Tx, r Request) (Decision, error) {
	if r.Principal.Audience == Owner {
		return Decision{Visibility: Full, RevisionID: r.ResourceID}, nil
	}
	if r.Surface == Media {
		var n int
		e := tx.QueryRowContext(ctx, `SELECT count(*) FROM config_revision_media m JOIN config_documents d ON d.applied_revision_id=m.revision_id WHERE m.media_id=? AND m.public=1`, r.ResourceID).Scan(&n)
		if e != nil {
			return Decision{}, e
		}
		if n > 0 {
			return Decision{Visibility: Full, RevisionID: r.ResourceID}, nil
		}
		rows, e := tx.QueryContext(ctx, `SELECT DISTINCT v.id FROM revision_media m JOIN content_versions v ON v.published_revision_id=m.revision_id WHERE m.media_id=?`, r.ResourceID)
		if e != nil {
			return Decision{}, e
		}
		ids := []string{}
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				rows.Close()
				return Decision{}, e
			}
			ids = append(ids, id)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return Decision{}, e
		}
		for _, id := range ids {
			d, e := s.ResolveIn(ctx, tx, Request{Principal: r.Principal, Surface: Content, ResourceID: id})
			if e != nil {
				return Decision{}, e
			}
			if d.AllowsBody() {
				return Decision{Visibility: Full, RevisionID: r.ResourceID}, nil
			}
		}
		return Decision{}, nil
	}
	var id, kind, revision, locale string
	e := tx.QueryRowContext(ctx, `SELECT i.id,i.kind,v.published_revision_id,v.locale FROM content_versions v JOIN content_items i ON i.id=v.item_id WHERE v.id=? AND v.published_revision_id IS NOT NULL`, r.ResourceID).Scan(&id, &kind, &revision, &locale)
	if errors.Is(e, sql.ErrNoRows) {
		return Decision{}, nil
	}
	if e != nil {
		return Decision{}, e
	}
	if kind == "lesson" {
		var visible int
		if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM collection_items m JOIN collection_plan_state st ON st.collection_id=m.collection_id JOIN collection_plan_members pm ON pm.plan_id=st.active_plan_id AND pm.item_id=m.item_id JOIN collection_locales l ON l.collection_id=m.collection_id AND l.locale=? AND l.visible=1 WHERE m.item_id=?`, locale, id).Scan(&visible); e != nil {
			return Decision{}, e
		}
		if visible != 1 {
			return Decision{}, nil
		}
		if s.Gates == nil {
			return Decision{Visibility: PublicTeaser}, nil
		}
		ok, e := s.Gates(ctx, tx, id, r.Principal)
		if e != nil {
			return Decision{}, e
		}
		if !ok {
			return Decision{Visibility: PublicTeaser}, nil
		}
	}
	return Decision{Visibility: Full, RevisionID: revision}, nil
}
