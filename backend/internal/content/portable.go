package content

import (
	"context"
	"github.com/mbougarne/modawana/backend/internal/db"
)

type PortableRevision struct {
	ID       string     `json:"id"`
	Key      string     `json:"key"`
	Locale   string     `json:"locale"`
	BodyPath string     `json:"body_path"`
	Current  bool       `json:"current"`
	Input    DraftInput `json:"input"`
}

// PortableRevisions exports all retained revisions, with exact source linkage.
func (s *Service) PortableRevisions(ctx context.Context) ([]PortableRevision, error) {
	rows, e := s.DB.QueryContext(ctx, `SELECT r.id,v.id,i.key,v.locale,i.kind,i.source_locale,coalesce(c.key,''),coalesce(m.position,0),r.id=v.draft_revision_id FROM revisions r JOIN content_versions v ON v.id=r.version_id JOIN content_items i ON i.id=v.item_id LEFT JOIN collection_items m ON m.item_id=i.id LEFT JOIN collections c ON c.id=m.collection_id ORDER BY (v.locale!=i.source_locale),r.created_at,r.id`)
	if e != nil {
		return nil, e
	}
	type row struct {
		id, version, key, locale, kind, source, collection string
		position                                           int
		current                                            bool
	}
	var records []row
	for rows.Next() {
		var r row
		if e = rows.Scan(&r.id, &r.version, &r.key, &r.locale, &r.kind, &r.source, &r.collection, &r.position, &r.current); e != nil {
			rows.Close()
			return nil, e
		}
		records = append(records, r)
		if len(records) > 2000 {
			rows.Close()
			return nil, ErrInvalid
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	out := []PortableRevision{}
	for _, r := range records {
		v, e := s.revision(ctx, s.DB, r.id, r.version)
		if e != nil {
			return nil, e
		}
		in := DraftInput{Kind: r.kind, SourceLocale: r.source, Slug: v.Slug, Title: v.Title, Summary: v.Summary, Markdown: v.Markdown, SourceRevisionID: v.SourceID, CategoryKey: v.CategoryKey, TagKeys: v.Tags, CoverMediaID: v.CoverID, Presentations: v.Presentations, CollectionKey: r.collection, Position: r.position, Quiz: v.Quiz}
		out = append(out, PortableRevision{ID: r.id, Key: r.key, Locale: r.locale, BodyPath: "content/" + r.id + ".md", Current: r.current, Input: in})
	}
	return out, nil
}

// StagePortableGates retains intended gates in a draft only. No reviews are copied.
func (s *Service) StagePortableGates(ctx context.Context, owner, key string, gates map[string]string) error {
	if len(gates) == 0 {
		return nil
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var collection, old string
	if e = tx.QueryRowContext(ctx, "SELECT c.id,st.draft_plan_id FROM collections c JOIN collection_plan_state st ON st.collection_id=c.id WHERE c.key=? AND c.kind='learning'", key).Scan(&collection, &old); e != nil {
		return e
	}
	id, e := db.NewID()
	if e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, "INSERT INTO collection_plans(id,collection_id,created_by,gate_policy_version) VALUES(?,?,?,1)", id, collection, owner); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, "INSERT INTO collection_plan_members SELECT ?,item_id,position,announcements_json FROM collection_plan_members WHERE plan_id=?", id, old); e != nil {
		return e
	}
	for key, assessment := range gates {
		var item string
		if e = tx.QueryRowContext(ctx, "SELECT id FROM content_items WHERE key=?", key).Scan(&item); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, "INSERT INTO collection_plan_gates VALUES(?,?,?)", id, item, assessment); e != nil {
			return e
		}
	}
	if _, e = tx.ExecContext(ctx, "UPDATE collection_plan_state SET draft_plan_id=? WHERE collection_id=?", id, collection); e != nil {
		return e
	}
	return tx.Commit()
}

// StagePortableRevision validates the original draft, while reserving a temporary
// slug so historical slug reuse cannot collide with another imported version.
// This method is only for an isolated, unpublished import staging database.
func (s *Service) StagePortableRevision(ctx context.Context, owner, key, locale string, in DraftInput) (Result, error) {
	return s.save(ctx, owner, key, locale, "", "import-stage", in)
}
