package content

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mbougarne/modawana/backend/internal/access"
	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/media"
)

type TaxonomyInput struct {
	ExpectedVersion *int64 `json:"expected_version"`
	Label           string `json:"label"`
	Slug            string `json:"slug"`
}
type Term struct {
	ID      string `json:"id"`
	Key     string `json:"key"`
	Kind    string `json:"kind"`
	Locale  string `json:"locale"`
	Label   string `json:"label"`
	Slug    string `json:"slug"`
	Version int64  `json:"version,omitempty"`
	Count   int    `json:"count"`
}

func taxTables(kind string) (string, string, string, error) {
	switch kind {
	case "category":
		return "categories", "category_locales", "category_id", nil
	case "tag":
		return "tags", "tag_locales", "tag_id", nil
	default:
		return "", "", "", ErrInvalid
	}
}
func (s *Service) SaveTerm(ctx context.Context, kind, key, locale string, in TaxonomyInput) (Term, error) {
	var out Term
	table, locales, idCol, e := taxTables(kind)
	if e != nil {
		return out, e
	}
	if in.ExpectedVersion == nil || *in.ExpectedVersion < 0 || !keyPattern.MatchString(key) || len(key) > 80 || !localeOK(locale) || !strOK(in.Label, 120, false) || !slugPattern.MatchString(in.Slug) {
		return out, ErrInvalid
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	var id string
	var version int64
	e = tx.QueryRowContext(ctx, "SELECT id,version FROM "+table+" WHERE key=?", key).Scan(&id, &version)
	if errors.Is(e, sql.ErrNoRows) {
		if *in.ExpectedVersion != 0 {
			return out, db.ErrVersionConflict
		}
		var count int
		if e = tx.QueryRowContext(ctx, "SELECT count(*) FROM "+table).Scan(&count); e != nil {
			return out, e
		}
		if count >= 500 {
			return out, ErrInvalid
		}
		id, e = db.NewID()
		if e != nil {
			return out, e
		}
		_, e = tx.ExecContext(ctx, "INSERT INTO "+table+"(id,key) VALUES(?,?)", id, key)
		version = 1
	} else if e == nil {
		if version != *in.ExpectedVersion {
			return out, db.ErrVersionConflict
		}
		_, e = tx.ExecContext(ctx, "UPDATE "+table+" SET version=version+1 WHERE id=?", id)
		version++
	}
	if e != nil {
		return out, e
	}
	_, e = tx.ExecContext(ctx, "INSERT INTO "+locales+"("+idCol+",locale,label,slug) VALUES(?,?,?,?) ON CONFLICT("+idCol+",locale) DO UPDATE SET label=excluded.label,slug=excluded.slug", id, locale, in.Label, in.Slug)
	if e != nil {
		return out, constraint(e)
	}
	if e = tx.Commit(); e != nil {
		return out, e
	}
	return Term{ID: id, Key: key, Kind: kind, Locale: locale, Label: in.Label, Slug: in.Slug, Version: version}, nil
}
func (s *Service) Taxonomy(ctx context.Context, locale string, private bool) ([]Term, error) {
	if !localeOK(locale) {
		return nil, ErrInvalid
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	out := []Term{}
	for _, kind := range []string{"category", "tag"} {
		table, locales, idCol, _ := taxTables(kind)
		condition := "r.category_id=t.id"
		if kind == "tag" {
			condition = "EXISTS(SELECT 1 FROM revision_tags rt WHERE rt.revision_id=r.id AND rt.tag_id=t.id)"
		}
		q := `SELECT t.id,t.key,l.label,l.slug,t.version,(SELECT count(*) FROM content_versions v JOIN revisions r ON r.id=v.published_revision_id WHERE v.locale=l.locale AND ` + condition + `) AS published_count FROM ` + table + ` t JOIN ` + locales + ` l ON l.` + idCol + `=t.id WHERE l.locale=?`
		if !private {
			q += " AND published_count>0"
		}
		q += " ORDER BY t.key LIMIT 501"
		rows, e := tx.QueryContext(ctx, q, locale)
		if e != nil {
			return nil, e
		}
		n := 0
		for rows.Next() {
			var t Term
			t.Kind = kind
			t.Locale = locale
			if e = rows.Scan(&t.ID, &t.Key, &t.Label, &t.Slug, &t.Version, &t.Count); e != nil {
				rows.Close()
				return nil, e
			}
			if !private {
				t.Version = 0
			}
			out = append(out, t)
			n++
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return nil, e
		}
		if n > 500 {
			return nil, ErrInvalid
		}
	}
	return out, nil
}

type Filter struct {
	Locale   string `json:"locale"`
	Kind     string `json:"kind"`
	Category string `json:"category"`
	Tag      string `json:"tag"`
	Query    string `json:"q"`
	Limit    int    `json:"limit"`
	Cursor   string `json:"cursor"`
}
type ListEntry struct {
	URL            string   `json:"url"`
	CategoryKey    string   `json:"category_key,omitempty"`
	Tags           []string `json:"tag_keys"`
	ReadingMinutes int      `json:"reading_minutes,omitempty"`
	Key            string   `json:"key"`
	Kind           string   `json:"kind"`
	Locale         string   `json:"locale"`
	Slug           string   `json:"slug"`
	Title          string   `json:"title"`
	Summary        string   `json:"summary"`
	Published      string   `json:"published_at"`
	Locked         bool     `json:"locked"`
}
type Page struct {
	Items []ListEntry `json:"items"`
	Total int         `json:"total"`
	Next  string      `json:"next_cursor"`
}
type pageCursor struct {
	Date   string `json:"d"`
	ID     string `json:"i"`
	Filter string `json:"f"`
}

func (s *Service) Discover(ctx context.Context, f Filter, p access.Principal) (Page, error) {
	out := Page{Items: []ListEntry{}}
	if f.Limit == 0 {
		f.Limit = 20
	}
	if !localeOK(f.Locale) || f.Limit < 1 || f.Limit > 50 || !strOK(f.Query, 200, true) || !utf8.ValidString(f.Query) || (f.Kind != "" && f.Kind != "article" && f.Kind != "lesson" && f.Kind != "doc") || f.Category != "" && !slugPattern.MatchString(f.Category) || f.Tag != "" && !slugPattern.MatchString(f.Tag) {
		return out, ErrInvalid
	}
	cursor := f.Cursor
	f.Cursor = ""
	signature := hash(f)
	var c pageCursor
	if cursor != "" {
		if len(cursor) > 512 {
			return out, ErrInvalid
		}
		b, e := base64.RawURLEncoding.DecodeString(cursor)
		if e != nil || json.Unmarshal(b, &c) != nil || !media.ID.MatchString(c.ID) || c.Filter != signature {
			return out, ErrInvalid
		}
		if _, e = time.Parse(db.TimestampLayout, c.Date); e != nil {
			return out, ErrInvalid
		}
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	from := ` FROM content_versions v JOIN content_items i ON i.id=v.item_id JOIN revisions r ON r.id=v.published_revision_id WHERE v.locale=?`
	from += ` AND (i.kind!='lesson' OR EXISTS(SELECT 1 FROM collection_items m JOIN collection_plan_state st ON st.collection_id=m.collection_id JOIN collection_plan_members pm ON pm.plan_id=st.active_plan_id AND pm.item_id=m.item_id JOIN collection_locales l ON l.collection_id=m.collection_id AND l.locale=v.locale AND l.visible=1 WHERE m.item_id=i.id))`
	args := []any{f.Locale}
	if f.Kind != "" {
		from += " AND i.kind=?"
		args = append(args, f.Kind)
	}
	if f.Category != "" {
		from += ` AND EXISTS(SELECT 1 FROM category_locales c WHERE c.category_id=r.category_id AND c.locale=v.locale AND c.slug=?)`
		args = append(args, f.Category)
	}
	if f.Tag != "" {
		from += ` AND EXISTS(SELECT 1 FROM revision_tags rt JOIN tag_locales t ON t.tag_id=rt.tag_id WHERE rt.revision_id=r.id AND t.locale=v.locale AND t.slug=?)`
		args = append(args, f.Tag)
	}
	if f.Query != "" {
		from += " AND (instr(lower(r.title),lower(?))>0 OR instr(lower(r.summary),lower(?))>0)"
		args = append(args, strings.TrimSpace(f.Query), strings.TrimSpace(f.Query))
	}
	if e = tx.QueryRowContext(ctx, "SELECT count(*)"+from, args...).Scan(&out.Total); e != nil {
		return out, e
	}
	if cursor != "" {
		from += " AND (v.last_published_at<? OR (v.last_published_at=? AND v.id>?))"
		args = append(args, c.Date, c.Date, c.ID)
	}
	args = append(args, f.Limit+1)
	rows, e := tx.QueryContext(ctx, "SELECT v.id,i.key,i.kind,v.locale,v.slug,r.title,r.summary,v.last_published_at,r.reading_minutes,coalesce((SELECT key FROM categories WHERE id=r.category_id),''),r.id"+from+" ORDER BY v.last_published_at DESC,v.id LIMIT ?", args...)
	if e != nil {
		return out, e
	}
	type row struct {
		id       string
		revision string
		entry    ListEntry
	}
	items := []row{}
	for rows.Next() {
		var a row
		if e = rows.Scan(&a.id, &a.entry.Key, &a.entry.Kind, &a.entry.Locale, &a.entry.Slug, &a.entry.Title, &a.entry.Summary, &a.entry.Published, &a.entry.ReadingMinutes, &a.entry.CategoryKey, &a.revision); e != nil {
			rows.Close()
			return out, e
		}
		items = append(items, a)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	more := len(items) > f.Limit
	if more {
		items = items[:f.Limit]
	}
	for _, a := range items {
		d, e := s.Access.ResolveIn(ctx, tx, access.Request{Principal: p, Surface: access.Search, ResourceID: a.id, Locale: f.Locale})
		if e != nil {
			return out, e
		}
		if d.Visibility == access.Hidden {
			continue
		}
		a.entry.Locked = !d.AllowsBody()
		a.entry.URL, e = canonical(ctx, tx, a.entry.Key, a.entry.Locale, a.entry.Kind, a.entry.Slug)
		if e != nil {
			return out, e
		}
		if a.entry.URL == "" {
			continue
		}
		if a.entry.Locked {
			a.entry.ReadingMinutes = 0
		}
		a.entry.Tags = []string{}
		tags, e := tx.QueryContext(ctx, "SELECT t.key FROM revision_tags rt JOIN tags t ON t.id=rt.tag_id WHERE rt.revision_id=? ORDER BY t.key", a.revision)
		if e != nil {
			return out, e
		}
		for tags.Next() {
			var key string
			if e = tags.Scan(&key); e != nil {
				tags.Close()
				return out, e
			}
			a.entry.Tags = append(a.entry.Tags, key)
		}
		e = tags.Err()
		tags.Close()
		if e != nil {
			return out, e
		}
		out.Items = append(out.Items, a.entry)
	}
	if more {
		last := items[len(items)-1]
		b, _ := json.Marshal(pageCursor{Date: last.entry.Published, ID: last.id, Filter: signature})
		out.Next = base64.RawURLEncoding.EncodeToString(b)
	}
	return out, nil
}
