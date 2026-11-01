package siteconfig

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/media"
	"github.com/mbougarne/modawana/backend/internal/storage"
	"sort"
)

type Service struct {
	DB    *sql.DB
	Files *storage.Store
}
type query interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func get(ctx context.Context, q query, kind string) (Document, error) {
	out := Document{Kind: kind, Snapshot: defaults(kind)}
	if out.Snapshot == nil {
		return out, ErrInvalid
	}
	var snapshot sql.NullString
	e := q.QueryRowContext(ctx, `SELECT d.version,coalesce(d.applied_revision_id,''),r.snapshot_json FROM config_documents d LEFT JOIN config_revisions r ON r.id=d.applied_revision_id WHERE d.kind=?`, kind).Scan(&out.Version, &out.RevisionID, &snapshot)
	if errors.Is(e, sql.ErrNoRows) {
		return out, nil
	}
	if snapshot.Valid {
		out.Snapshot = json.RawMessage(snapshot.String)
	}
	return out, e
}
func (s *Service) Get(ctx context.Context, kind string) (Document, error) {
	return get(ctx, s.DB, kind)
}
func validate(kind string, b json.RawMessage) (json.RawMessage, []mediaRef, error) {
	if len(b) > 262144 {
		return nil, nil, ErrInvalid
	}
	if kind == "site" || kind == "profile" {
		var required, supplied map[string]json.RawMessage
		_ = json.Unmarshal(defaults(kind), &required)
		if json.Unmarshal(b, &supplied) != nil {
			return nil, nil, ErrInvalid
		}
		for key := range required {
			if value, ok := supplied[key]; !ok || (key != "main_action" && string(value) == "null") {
				return nil, nil, ErrInvalid
			}
		}
	}
	switch kind {
	case "site":
		var v Site
		if decode(b, &v) != nil {
			return nil, nil, ErrInvalid
		}
		refs, e := validateSite(v)
		return raw(v), refs, e
	case "profile":
		var v Profile
		if decode(b, &v) != nil {
			return nil, nil, ErrInvalid
		}
		refs, e := validateProfile(v)
		return raw(v), refs, e
	case "labels":
		var v Labels
		if decode(b, &v) != nil {
			return nil, nil, ErrInvalid
		}
		e := validateLabels(v)
		return raw(v), nil, e
	}
	return nil, nil, ErrInvalid
}
func reviewKey(r Review) string { return r.Path + "/" + r.Locale }
func reviews(ctx context.Context, q query, id string) (map[string]Review, error) {
	out := map[string]Review{}
	rows, e := q.QueryContext(ctx, `SELECT field_path,locale,source_hash,value_hash FROM config_translation_reviews WHERE revision_id=?`, id)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	for rows.Next() {
		var r Review
		if e = rows.Scan(&r.Path, &r.Locale, &r.SourceHash, &r.ValueHash); e != nil {
			return nil, e
		}
		out[reviewKey(r)] = r
	}
	return out, rows.Err()
}
func requirements(kind string, b json.RawMessage, prior map[string]Review) ([]Requirement, []Review, []Requirement) {
	stale := []Requirement{}
	required := []Requirement{}
	retained := []Review{}
	for path, locales := range translations(kind, b) {
		for _, l := range []string{"ar", "fr"} {
			v := locales[l]
			if len(v) == 0 || string(v) == `""` {
				continue
			}
			r := Review{Path: path, Locale: l, SourceHash: digest(locales["en"]), ValueHash: digest(v)}
			if old, ok := prior[reviewKey(r)]; ok && old.ValueHash == r.ValueHash {
				retained = append(retained, old)
				if old.SourceHash != r.SourceHash {
					stale = append(stale, Requirement{r, v})
				}
			} else {
				required = append(required, Requirement{r, v})
			}
		}
	}
	sort.Slice(required, func(i, j int) bool { return reviewKey(required[i].Review) < reviewKey(required[j].Review) })
	sort.Slice(stale, func(i, j int) bool { return reviewKey(stale[i].Review) < reviewKey(stale[j].Review) })
	return required, retained, stale
}
func (s *Service) validateRefs(ctx context.Context, q query, kind string, b json.RawMessage, refs []mediaRef) error {
	for _, r := range refs {
		invalidMedia := func() error {
			return invalidField(r.Purpose+"_media_id", "media")
		}
		if r.ID == "" {
			continue
		}
		if !media.ID.MatchString(r.ID) {
			return invalidMedia()
		}
		m, e := media.Get(ctx, q, r.ID)
		if e != nil {
			if errors.Is(e, sql.ErrNoRows) {
				return invalidMedia()
			}
			return e
		}
		h, e := s.Files.OpenUpload(m.File)
		if e != nil {
			return e
		}
		h.Close()
		max := int64(1 << 20)
		if r.Purpose == "portrait" {
			max = 2 << 20
		} else if r.Purpose == "banner" {
			max = 10 << 20
		}
		if m.Kind != "image" || m.File.Size > max || (r.Purpose == "portrait" && (m.Width < 600 || m.Width != m.Height)) {
			return invalidMedia()
		}
	}
	if kind == "profile" {
		var p Profile
		_ = json.Unmarshal(b, &p)
		if p.AboutKey != "" {
			var n int
			e := q.QueryRowContext(ctx, `SELECT count(*) FROM content_items i JOIN content_versions v ON v.item_id=i.id WHERE i.key=? AND i.kind='doc' AND v.published_revision_id IS NOT NULL`, p.AboutKey).Scan(&n)
			if e != nil {
				return e
			}
			if n == 0 {
				return invalidField("about_key", "reference")
			}
		}
	}
	return nil
}
func (s *Service) Preview(ctx context.Context, kind string, in Input) (Preview, error) {
	var out Preview
	b, refs, e := validate(kind, in.Snapshot)
	if e != nil {
		return out, e
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	d, e := get(ctx, tx, kind)
	if e != nil {
		return out, e
	}
	if in.ExpectedVersion == nil || d.Version != *in.ExpectedVersion {
		return out, db.ErrVersionConflict
	}
	if e = s.validateRefs(ctx, tx, kind, b, refs); e != nil {
		return out, e
	}
	old, e := reviews(ctx, tx, d.RevisionID)
	if e != nil {
		return out, e
	}
	req, _, stale := requirements(kind, b, old)
	d.Snapshot = b
	return Preview{Document: d, Required: req, Stale: stale}, nil
}
func (s *Service) Apply(ctx context.Context, owner, kind string, in Input) (Document, error) {
	out := Document{}
	b, refs, e := validate(kind, in.Snapshot)
	if e != nil {
		return out, e
	}
	if in.ExpectedVersion == nil || *in.ExpectedVersion < 0 || len(in.Reviews) > 3000 {
		return out, ErrInvalid
	}
	e = s.Files.Exclusive(ctx, func() error {
		tx, e := s.DB.BeginTx(ctx, nil)
		if e != nil {
			return e
		}
		defer tx.Rollback()
		d, e := get(ctx, tx, kind)
		if e != nil {
			return e
		}
		if d.Version != *in.ExpectedVersion {
			return db.ErrVersionConflict
		}
		if e = s.validateRefs(ctx, tx, kind, b, refs); e != nil {
			return e
		}
		old, e := reviews(ctx, tx, d.RevisionID)
		if e != nil {
			return e
		}
		req, retain, stale := requirements(kind, b, old)
		confirmed := map[string]Review{}
		for _, r := range in.Reviews {
			if _, exists := confirmed[reviewKey(r)]; exists {
				return ErrInvalid
			}
			confirmed[reviewKey(r)] = r
		}
		for _, r := range req {
			if confirmed[reviewKey(r.Review)] != r.Review {
				return ErrReview
			}
			retain = append(retain, r.Review)
		}
		for _, r := range stale {
			if supplied, ok := confirmed[reviewKey(r.Review)]; ok {
				if supplied != r.Review {
					return ErrReview
				}
				for i, old := range retain {
					if reviewKey(old) == reviewKey(r.Review) {
						retain[i] = r.Review
					}
				}
			}
		}
		var docID string
		if d.Version == 0 {
			docID, e = db.NewID()
			if e != nil {
				return e
			}
			_, e = tx.ExecContext(ctx, "INSERT INTO config_documents(id,kind) VALUES(?,?)", docID, kind)
			if e != nil {
				return e
			}
		} else if e = tx.QueryRowContext(ctx, "SELECT id FROM config_documents WHERE kind=?", kind).Scan(&docID); e != nil {
			return e
		}
		id, e := db.NewID()
		if e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO config_revisions(id,document_id,snapshot_json,schema_version,colour_algorithm_version,created_by) VALUES(?,?,?,1,1,?)`, id, docID, string(b), owner)
		if e != nil {
			return e
		}
		for _, r := range refs {
			if r.ID == "" {
				continue
			}
			if _, e = tx.ExecContext(ctx, "INSERT INTO config_revision_media(revision_id,media_id,role,public) VALUES(?,?,?,?)", id, r.ID, r.Purpose, r.Public); e != nil {
				return e
			}
		}
		for _, r := range retain {
			if _, e = tx.ExecContext(ctx, `INSERT INTO config_translation_reviews(revision_id,field_path,locale,source_hash,value_hash,reviewed_by) VALUES(?,?,?,?,?,?)`, id, r.Path, r.Locale, r.SourceHash, r.ValueHash, owner); e != nil {
				return e
			}
		}
		if _, e = tx.ExecContext(ctx, "UPDATE config_documents SET version=?,applied_revision_id=? WHERE id=?", d.Version+1, id, docID); e != nil {
			return e
		}
		audit, e := db.NewID()
		if e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, "INSERT INTO config_audit(id,revision_id,actor_id) VALUES(?,?,?)", audit, id, owner); e != nil {
			return e
		}
		if e = tx.Commit(); e != nil {
			return e
		}
		out = Document{kind, d.Version + 1, id, b}
		return nil
	})
	return out, e
}

type HistoryEntry struct {
	ID      string `json:"revision_id"`
	Created string `json:"created_at"`
}

func (s *Service) History(ctx context.Context, kind, cursor string) ([]HistoryEntry, string, error) {
	if defaults(kind) == nil || (cursor != "" && !media.ID.MatchString(cursor)) {
		return nil, "", ErrInvalid
	}
	date := ""
	if cursor != "" {
		if e := s.DB.QueryRowContext(ctx, `SELECT r.created_at FROM config_revisions r JOIN config_documents d ON d.id=r.document_id WHERE r.id=? AND d.kind=?`, cursor, kind).Scan(&date); e != nil {
			return nil, "", ErrInvalid
		}
	}
	rows, e := s.DB.QueryContext(ctx, `SELECT r.id,r.created_at FROM config_revisions r JOIN config_documents d ON d.id=r.document_id WHERE d.kind=? AND (?='' OR r.created_at<? OR (r.created_at=? AND r.id<?)) ORDER BY r.created_at DESC,r.id DESC LIMIT 51`, kind, cursor, date, date, cursor)
	if e != nil {
		return nil, "", e
	}
	defer rows.Close()
	out := []HistoryEntry{}
	for rows.Next() {
		var h HistoryEntry
		if e = rows.Scan(&h.ID, &h.Created); e != nil {
			return nil, "", e
		}
		out = append(out, h)
	}
	next := ""
	if len(out) > 50 {
		out = out[:50]
		next = out[49].ID
	}
	return out, next, rows.Err()
}
func (s *Service) Restore(ctx context.Context, kind, id string, expected int64) (Document, error) {
	d, e := s.Get(ctx, kind)
	if e != nil {
		return d, e
	}
	if d.Version != expected {
		return d, db.ErrVersionConflict
	}
	var b string
	e = s.DB.QueryRowContext(ctx, `SELECT r.snapshot_json FROM config_revisions r JOIN config_documents d ON d.id=r.document_id WHERE r.id=? AND d.kind=?`, id, kind).Scan(&b)
	if e != nil {
		return d, e
	}
	d.Snapshot = json.RawMessage(b)
	d.RevisionID = ""
	return d, nil
}

func (s *Service) Defaults(ctx context.Context, kind string) (Document, error) {
	d, e := s.Get(ctx, kind)
	d.Snapshot = defaults(kind)
	d.RevisionID = ""
	return d, e
}
