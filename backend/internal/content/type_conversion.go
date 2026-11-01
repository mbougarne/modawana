package content

import (
	"context"
	"database/sql"
	"errors"
	"net/url"

	"github.com/mbougarne/modawana/backend/internal/access"
	"github.com/mbougarne/modawana/backend/internal/db"
)

type TypeChange struct {
	ExpectedVersion           *int64 `json:"expected_version"`
	FromKind                  string `json:"from_kind"`
	Kind                      string `json:"kind"`
	CollectionKey             string `json:"collection_key"`
	ExpectedCollectionVersion *int64 `json:"expected_collection_version"`
	Position                  int    `json:"position"`
}
type TypeChangeError struct {
	Path, Code string
	Cause      error
}

func (e *TypeChangeError) Error() string { return ErrInvalid.Error() }
func (e *TypeChangeError) Unwrap() error {
	if e.Cause != nil {
		return e.Cause
	}
	return ErrInvalid
}
func typeIssue(path, code string) error { return &TypeChangeError{Path: path, Code: code} }

// ChangeType remains compatible; normal owner saves use the same transaction helper.
func (s *Service) ChangeType(ctx context.Context, owner, key, locale, retryKey string, in TypeChange) (Result, error) {
	var out Result
	request := hash(struct {
		Key, Locale string
		Input       TypeChange
	}{key, locale, in})
	err := s.Files.Exclusive(ctx, func() error {
		tx, err := s.DB.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if err = access.CheckMutation(ctx, tx, ""); err != nil {
			return err
		}
		prior, ok, err := retry(ctx, tx, access.Actor(ctx, owner), "type", retryKey, request)
		if err != nil || ok {
			out = prior
			return err
		}
		out, err = s.changeTypeIn(ctx, tx, owner, key, locale, in, false)
		if err != nil {
			return err
		}
		if err = remember(ctx, tx, access.Actor(ctx, owner), "type", retryKey, request, out); err != nil {
			return err
		}
		return tx.Commit()
	})
	return out, err
}
func (s *Service) changeTypeIn(ctx context.Context, tx *sql.Tx, owner, key, locale string, in TypeChange, saving bool) (Result, error) {
	var out Result
	if access.Actor(ctx, owner) != owner {
		return out, typeIssue("kind", "conversion_owner")
	}
	if !keyPattern.MatchString(key) || !localeOK(locale) || in.ExpectedVersion == nil || *in.ExpectedVersion < 1 {
		return out, ErrInvalid
	}
	if (in.Kind != "article" && in.Kind != "lesson" && in.Kind != "doc") || in.Kind == in.FromKind {
		return out, typeIssue("kind", "conversion_kind")
	}
	err := func() error {
		v, err := loadVersion(ctx, tx, key, locale)
		if err != nil {
			return err
		}
		if v.Number != *in.ExpectedVersion || v.Kind != in.FromKind {
			return db.ErrVersionConflict
		}
		if err = access.CheckMutation(ctx, tx, v.Item); err != nil {
			return err
		}
		var count int
		err = tx.QueryRowContext(ctx, `SELECT count(*) FROM collection_plan_members m JOIN collection_plan_state st ON m.plan_id=st.active_plan_id OR m.plan_id=st.draft_plan_id WHERE m.item_id=?`, v.Item).Scan(&count)
		if err != nil {
			return err
		}
		if count > 0 {
			return typeIssue("kind", "conversion_plan")
		}
		if in.Kind == "doc" {
			err = tx.QueryRowContext(ctx, `SELECT count(*) FROM content_versions v JOIN quiz_bindings b ON b.revision_id=v.draft_revision_id OR b.revision_id=v.published_revision_id WHERE v.item_id=?`, v.Item).Scan(&count)
			if err != nil {
				return err
			}
			if count > 0 {
				return typeIssue("kind", "conversion_quiz")
			}
		}
		if v.Kind == "doc" {
			err = tx.QueryRowContext(ctx, `SELECT count(*) FROM config_documents d JOIN config_revisions r ON r.id=d.applied_revision_id WHERE d.kind='profile' AND json_extract(r.snapshot_json,'$.about_key')=?`, key).Scan(&count)
			if err != nil {
				return err
			}
			if count > 0 {
				return typeIssue("kind", "conversion_about")
			}
		}
		position := in.Position
		if in.Kind != "article" {
			if !keyPattern.MatchString(in.CollectionKey) || in.ExpectedCollectionVersion == nil {
				return typeIssue("collection_key", "conversion_collection")
			}
			var collectionKind, collectionID string
			var collectionVersion int64
			err = tx.QueryRowContext(ctx, "SELECT id,kind,version FROM collections WHERE key=?", in.CollectionKey).Scan(&collectionID, &collectionKind, &collectionVersion)
			if errors.Is(err, sql.ErrNoRows) {
				return typeIssue("collection_key", "conversion_collection")
			}
			if err != nil {
				return err
			}
			if collectionVersion != *in.ExpectedCollectionVersion {
				return db.ErrVersionConflict
			}
			if (in.Kind == "lesson" && collectionKind != "learning") || (in.Kind == "doc" && collectionKind != "docs") {
				return typeIssue("collection_key", "conversion_collection")
			}
			if position == 0 {
				if err = tx.QueryRowContext(ctx, "SELECT coalesce(max(position),0)+1 FROM collection_items WHERE collection_id=?", collectionID).Scan(&position); err != nil {
					return err
				}
			}
			if position < 1 || position > 500 {
				return typeIssue("position", "conversion_position")
			}
			if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM collection_items WHERE collection_id=? AND position=?", collectionID, position).Scan(&count); err != nil {
				return err
			}
			if count > 0 {
				return typeIssue("position", "conversion_position")
			}
		} else if in.CollectionKey != "" || position != 0 {
			return typeIssue("collection_key", "conversion_article")
		}
		// Record only formerly published canonical URLs. Aliases still use current gates.
		rows, err := tx.QueryContext(ctx, "SELECT locale,slug FROM content_versions WHERE item_id=? AND published_revision_id IS NOT NULL", v.Item)
		if err != nil {
			return err
		}
		type route struct{ locale, slug string }
		routes := []route{}
		for rows.Next() {
			var r route
			if err = rows.Scan(&r.locale, &r.slug); err != nil {
				rows.Close()
				return err
			}
			routes = append(routes, r)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, r := range routes {
			path, err := canonical(ctx, tx, key, r.locale, v.Kind, r.slug)
			if err != nil {
				return err
			}
			if path != "" {
				path, err = url.PathUnescape(path)
				if err != nil {
					return err
				}
				if _, err = tx.ExecContext(ctx, "INSERT INTO content_route_aliases(path,item_id,locale) VALUES(?,?,?) ON CONFLICT(path) DO NOTHING", path, v.Item, r.locale); err != nil {
					return err
				}
			}
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO content_type_conversion_guard VALUES(?,?,?)", v.Item, v.Kind, in.Kind); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "UPDATE collections SET version=version+1 WHERE id IN (SELECT collection_id FROM collection_items WHERE item_id=?)", v.Item); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM collection_items WHERE item_id=?", v.Item); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "UPDATE content_items SET kind=? WHERE id=?", in.Kind, v.Item); err != nil {
			return err
		}
		if in.Kind != "article" {
			if err = s.AttachMember(ctx, tx, v.Item, in.Kind, in.CollectionKey, position); err != nil {
				return err
			}
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM content_type_conversion_guard WHERE item_id=?", v.Item); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "UPDATE content_versions SET version=version+1 WHERE item_id=? AND (?=0 OR locale!=?)", v.Item, saving, locale); err != nil {
			return err
		}
		event, err := db.NewID()
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO content_type_events(id,item_id,from_kind,to_kind,created_by) VALUES(?,?,?,?,?)", event, v.Item, v.Kind, in.Kind, owner); err != nil {
			return err
		}
		out = Result{RevisionID: v.Draft, Version: v.Number + 1}
		return nil
	}()
	return out, err
}
