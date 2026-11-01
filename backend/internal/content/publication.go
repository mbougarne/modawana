package content

import (
	"context"
	"database/sql"
	"errors"
	"github.com/mbougarne/modawana/backend/internal/learning"
	"time"

	"github.com/mbougarne/modawana/backend/internal/access"
	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/media"
)

// ChangePublication binds review/publication to an exact revision and version.
// The hook runs inside this transaction for later quiz/active-plan validation.
func (s *Service) ChangePublication(ctx context.Context, owner, key, locale, retryKey, op string, a Action) (Result, error) {
	var out Result
	if (op != "publish" && op != "unpublish" && op != "review") || a.ExpectedVersion == nil || *a.ExpectedVersion < 1 || !media.ID.MatchString(a.RevisionID) {
		return out, ErrInvalid
	}
	request := hash(struct {
		Key, Locale string
		Action      Action
	}{key, locale, a})
	e := s.Files.Exclusive(ctx, func() error {
		tx, e := s.DB.BeginTx(ctx, nil)
		if e != nil {
			return e
		}
		defer tx.Rollback()
		if e = access.CheckMutation(ctx, tx, ""); e != nil {
			return e
		}
		prior, ok, e := retry(ctx, tx, access.Actor(ctx, owner), op, retryKey, request)
		if e != nil || ok {
			out = prior
			return e
		}
		out, e = s.changePublicationIn(ctx, tx, owner, key, locale, op, a)
		if e != nil {
			return e
		}
		if e = remember(ctx, tx, access.Actor(ctx, owner), op, retryKey, request, out); e != nil {
			return e
		}
		return tx.Commit()
	})
	return out, e
}

// changePublicationIn is shared by single-item and atomic bulk publication.
func (s *Service) changePublicationIn(ctx context.Context, tx *sql.Tx, owner, key, locale, op string, a Action) (Result, error) {
	v, e := loadVersion(ctx, tx, key, locale)
	if e != nil {
		return Result{}, e
	}
	if v.Number != *a.ExpectedVersion {
		return Result{}, db.ErrVersionConflict
	}
	if op == "unpublish" {
		if v.Published != a.RevisionID {
			return Result{}, db.ErrVersionConflict
		}
	} else if v.Draft != a.RevisionID {
		return Result{}, db.ErrVersionConflict
	}
	rev := Revision{ID: a.RevisionID}
	if op != "unpublish" {
		rev, e = s.revision(ctx, tx, a.RevisionID, v.ID)
		if e != nil {
			return Result{}, e
		}
	}
	if op != "unpublish" {
		for _, m := range rev.Media {
			h, e := s.Files.OpenUpload(m.File)
			if e != nil {
				return Result{}, e
			}
			h.Close()
		}
	}
	if op == "review" || op == "publish" && v.Source != locale {
		if v.Source == locale {
			return Result{}, ErrInvalid
		}
		var current string
		e = tx.QueryRowContext(ctx, "SELECT coalesce(published_revision_id,'') FROM content_versions WHERE item_id=? AND locale=?", v.Item, v.Source).Scan(&current)
		if e != nil {
			return Result{}, e
		}
		if current == "" || rev.SourceID != current {
			return Result{}, ErrReview
		}
		if op == "review" {
			if a.SourceRevisionID != current {
				return Result{}, ErrReview
			}
			_, e = tx.ExecContext(ctx, "INSERT INTO translation_reviews(revision_id,source_revision_id,reviewed_by) VALUES(?,?,?) ON CONFLICT DO NOTHING", rev.ID, current, owner)
			if e != nil {
				return Result{}, e
			}
		} else {
			var n int
			e = tx.QueryRowContext(ctx, "SELECT count(*) FROM translation_reviews WHERE revision_id=? AND source_revision_id=?", rev.ID, current).Scan(&n)
			if e != nil {
				return Result{}, e
			}
			if n != 1 {
				return Result{}, ErrReview
			}
		}
	}
	if e = learning.ReviewPublication(ctx, tx, owner, v.Item, rev.ID, locale, v.Source, op); e != nil {
		return Result{}, e
	}
	if a.Plan != nil && (op != "publish" || v.Kind != "lesson") {
		return Result{}, ErrInvalid
	}
	if op != "review" && s.PublicationHook != nil && a.Plan == nil {
		if e = s.PublicationHook(ctx, tx, v.Item, rev.ID, op == "publish"); e != nil {
			return Result{}, e
		}
	}
	if op == "publish" {
		_, e = tx.ExecContext(ctx, "UPDATE content_versions SET published_revision_id=?,first_published_at=coalesce(first_published_at,?),last_published_at=?,version=version+1 WHERE id=?", rev.ID, db.UTC(time.Now()), db.UTC(time.Now()), v.ID)
	} else if op == "unpublish" {
		_, e = tx.ExecContext(ctx, "UPDATE content_versions SET published_revision_id=NULL,version=version+1 WHERE id=?", v.ID)
	} else {
		_, e = tx.ExecContext(ctx, "UPDATE content_versions SET version=version+1 WHERE id=?", v.ID)
	}
	if e != nil {
		return Result{}, e
	}
	if a.Plan != nil {
		var collectionKey string
		if e = tx.QueryRowContext(ctx, "SELECT c.key FROM collections c JOIN collection_items m ON m.collection_id=c.id WHERE m.item_id=?", v.Item).Scan(&collectionKey); e != nil {
			return Result{}, e
		}
		if _, e = s.applyPlanIn(ctx, tx, collectionKey, *a.Plan); e != nil {
			return Result{}, e
		}
		// Validate the published locale against the resulting plan, including a
		// translated revision published atomically with plan activation.
		if s.PublicationHook != nil {
			if e = s.PublicationHook(ctx, tx, v.Item, rev.ID, true); e != nil {
				return Result{}, e
			}
		}
	}
	return Result{RevisionID: rev.ID, Version: v.Number + 1}, nil
}

type Alternative struct {
	Locale string `json:"locale"`
	Slug   string `json:"slug"`
	URL    string `json:"url"`
}
type PublicMedia struct {
	ID     string `json:"id"`
	URL    string `json:"url"`
	Kind   string `json:"kind"`
	MIME   string `json:"mime_type"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}
type PublicContent struct {
	Learning       *ReaderLearning `json:"learning,omitempty"`
	ReadingMinutes int             `json:"reading_minutes,omitempty"`
	Key            string          `json:"key"`
	Kind           string          `json:"kind"`
	Locale         string          `json:"locale"`
	Slug           string          `json:"slug"`
	Title          string          `json:"title"`
	Summary        string          `json:"summary"`
	RevisionID     string          `json:"revision_id"`
	Published      string          `json:"published_at"`
	Locked         bool            `json:"locked"`
	Stale          bool            `json:"translation_stale"`
	Markdown       string          `json:"markdown,omitempty"`
	Media          []PublicMedia   `json:"media,omitempty"`
	Presentations  []Presentation  `json:"media_presentations,omitempty"`
	CoverID        string          `json:"cover_media_id,omitempty"`
	CategoryKey    string          `json:"category_key,omitempty"`
	Tags           []string        `json:"tag_keys,omitempty"`
	Alternatives   []Alternative   `json:"alternatives"`
}

func alternatives(ctx context.Context, q query, item string) ([]Alternative, error) {
	out := []Alternative{}
	rows, e := q.QueryContext(ctx, "SELECT v.locale,v.slug,i.key,i.kind FROM content_versions v JOIN content_items i ON i.id=v.item_id WHERE item_id=? AND published_revision_id IS NOT NULL ORDER BY locale", item)
	if e != nil {
		return out, e
	}
	type candidate struct {
		alt       Alternative
		key, kind string
	}
	candidates := []candidate{}
	for rows.Next() {
		var c candidate
		if e = rows.Scan(&c.alt.Locale, &c.alt.Slug, &c.key, &c.kind); e != nil {
			rows.Close()
			return out, e
		}
		candidates = append(candidates, c)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	for _, c := range candidates {
		c.alt.URL, e = canonical(ctx, q, c.key, c.alt.Locale, c.kind, c.alt.Slug)
		if e != nil {
			return out, e
		}
		if c.alt.URL != "" {
			out = append(out, c.alt)
		}
	}
	return out, nil
}
func (s *Service) Alternatives(ctx context.Context, key string) ([]Alternative, error) {
	var item string
	e := s.DB.QueryRowContext(ctx, "SELECT id FROM content_items WHERE key=?", key).Scan(&item)
	if e != nil {
		return nil, e
	}
	a, e := alternatives(ctx, s.DB, item)
	if e != nil {
		return nil, e
	}
	if len(a) == 0 {
		return nil, sql.ErrNoRows
	}
	return a, nil
}
func (s *Service) Public(ctx context.Context, locale, slug string, p access.Principal) (PublicContent, error) {
	var out PublicContent
	if !localeOK(locale) || !slugPattern.MatchString(slug) {
		return out, sql.ErrNoRows
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	var key string
	e = tx.QueryRowContext(ctx, "SELECT i.key FROM content_versions v JOIN content_items i ON i.id=v.item_id WHERE v.locale=? AND v.slug=? AND v.published_revision_id IS NOT NULL", locale, slug).Scan(&key)
	if e != nil {
		return out, e
	}
	v, e := loadVersion(ctx, tx, key, locale)
	if e != nil {
		return out, e
	}
	path, e := canonical(ctx, tx, key, locale, v.Kind, v.Slug)
	if e != nil {
		return out, e
	}
	if path == "" {
		return out, sql.ErrNoRows
	}
	d, e := s.Access.ResolveIn(ctx, tx, access.Request{Principal: p, Surface: access.Content, ResourceID: v.ID, Locale: locale})
	if e != nil {
		return out, e
	}
	if d.Visibility == access.Hidden {
		return out, sql.ErrNoRows
	}
	out = PublicContent{Key: key, Kind: v.Kind, Locale: locale, Slug: slug, RevisionID: v.Published, Locked: !d.AllowsBody()}
	var source string
	e = tx.QueryRowContext(ctx, "SELECT r.title,r.summary,coalesce(r.source_revision_id,''),v.last_published_at FROM revisions r JOIN content_versions v ON v.published_revision_id=r.id WHERE v.id=?", v.ID).Scan(&out.Title, &out.Summary, &source, &out.Published)
	if e != nil {
		return out, e
	}
	out.Alternatives, e = alternatives(ctx, tx, v.Item)
	if e != nil {
		return out, e
	}
	if v.Source != locale {
		var current string
		e = tx.QueryRowContext(ctx, "SELECT coalesce(published_revision_id,'') FROM content_versions WHERE item_id=? AND locale=?", v.Item, v.Source).Scan(&current)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return out, e
		}
		out.Stale = current == "" || current != source
	}
	if v.Kind == "lesson" || v.Kind == "article" {
		state, e := s.readerLearning(ctx, tx, v, p)
		if e != nil {
			return out, e
		}
		out.Learning = &state
	}
	if out.Locked {
		return out, ErrLocked
	}
	r, e := s.revision(ctx, tx, v.Published, v.ID)
	if e != nil {
		return out, e
	}
	out.ReadingMinutes = readingMinutes(r.Markdown)
	out.Markdown = r.Markdown
	out.Presentations = r.Presentations
	out.CoverID = r.CoverID
	out.CategoryKey = r.CategoryKey
	out.Tags = r.Tags
	for _, m := range r.Media {
		out.Media = append(out.Media, PublicMedia{m.ID, m.URL, m.Kind, m.MIME, m.Width, m.Height})
	}
	return out, nil
}
