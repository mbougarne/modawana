package store

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/locale"
	"github.com/mbougarne/modawana/backend/internal/media"
)

type Category struct {
	Key     string `json:"key"`
	Locale  string `json:"locale"`
	Title   string `json:"title"`
	Version int64  `json:"version"`
}

type Image struct {
	ID  string `json:"id"`
	Alt string `json:"alt"`
}

type DraftInput struct {
	Version     int64   `json:"version"`
	Slug        string  `json:"slug"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Category    string  `json:"category"`
	Images      []Image `json:"images"`
}

type Draft struct {
	DraftInput
	Key              string    `json:"key"`
	Locale           string    `json:"locale"`
	Published        bool      `json:"published"`
	PublishedVersion int64     `json:"published_version"`
	PublicURL        string    `json:"public_url,omitempty"`
	Variants         []Variant `json:"variants"`
}

type Entry struct {
	Key       string `json:"key"`
	Title     string `json:"title"`
	Localized bool   `json:"localized"`
	Published bool   `json:"published"`
}

type Workspace struct {
	Settings   Settings   `json:"settings"`
	Categories []Category `json:"categories"`
	Products   []Entry    `json:"products"`
	Next       string     `json:"next,omitempty"`
}

func validDraft(key, language string, in DraftInput) bool {
	if !keyPattern.MatchString(key) || !locale.Valid(language) || in.Version < 0 || !slugPattern.MatchString(in.Slug) ||
		!plain(in.Title, 160, true, false) || !plain(in.Description, 4096, true, true) ||
		(in.Category != "" && !keyPattern.MatchString(in.Category)) || len(in.Images) > 8 {
		return false
	}
	ids := map[string]bool{}
	for _, image := range in.Images {
		if !media.ID.MatchString(image.ID) || !plain(image.Alt, 300, true, false) || ids[image.ID] {
			return false
		}
		ids[image.ID] = true
	}
	return true
}

func (s *Service) SaveDraft(ctx context.Context, key, language string, in DraftInput) (Draft, error) {
	if !validDraft(key, language, in) {
		return Draft{}, ErrInvalid
	}
	var out Draft
	err := s.write(ctx, key, func(tx *sql.Tx) error {
		if in.Version == 0 {
			if _, err := tx.ExecContext(ctx, "INSERT INTO store_products(key) VALUES(?) ON CONFLICT DO NOTHING", key); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx, `INSERT INTO store_localizations(product,locale,version,slug,title,description,category)
				VALUES(?,?,1,?,?,?,?)`, key, language, in.Slug, strings.TrimSpace(in.Title), strings.TrimSpace(in.Description), nullable(in.Category))
			if err != nil {
				return err
			}
		} else {
			res, err := tx.ExecContext(ctx, `UPDATE store_localizations SET version=version+1,slug=?,title=?,description=?,category=?
				WHERE product=? AND locale=? AND version=?`, in.Slug, strings.TrimSpace(in.Title), strings.TrimSpace(in.Description), nullable(in.Category), key, language, in.Version)
			if err = changed(res, err); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM store_draft_images WHERE product=? AND locale=?", key, language); err != nil {
			return err
		}
		for position, image := range in.Images {
			if _, err := tx.ExecContext(ctx, "INSERT INTO store_draft_images VALUES(?,?,?,?,?)", key, language, position, image.ID, image.Alt); err != nil {
				return err
			}
		}
		var err error
		out, err = draftIn(ctx, tx, key, language)
		return err
	})
	return out, err
}

func changed(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	count, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return db.ErrVersionConflict
	}
	return nil
}

func (s *Service) Publication(ctx context.Context, key, language string, expected int64, publish bool) (Draft, error) {
	if !keyPattern.MatchString(key) || !locale.Valid(language) || expected < 1 {
		return Draft{}, ErrInvalid
	}
	var out Draft
	err := s.write(ctx, key, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE store_localizations SET version=version+1 WHERE product=? AND locale=? AND version=?`, key, language, expected)
		if err = changed(res, err); err != nil {
			return err
		}
		if !publish {
			if _, err = tx.ExecContext(ctx, "DELETE FROM store_publications WHERE product=? AND locale=?", key, language); err != nil {
				return err
			}
			out, err = draftIn(ctx, tx, key, language)
			return err
		}
		var active bool
		if err = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM store_variants WHERE product=? AND active=1)", key).Scan(&active); err != nil {
			return err
		}
		if !active {
			return ErrVariant
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO store_publications(product,locale,version,slug,title,description,category,published_at)
			SELECT product,locale,version,slug,title,description,category,? FROM store_localizations WHERE product=? AND locale=?
			ON CONFLICT(product,locale) DO UPDATE SET version=excluded.version,slug=excluded.slug,title=excluded.title,
			 description=excluded.description,category=excluded.category,published_at=excluded.published_at`, db.UTC(time.Now()), key, language); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM store_published_images WHERE product=? AND locale=?", key, language); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO store_published_images SELECT * FROM store_draft_images WHERE product=? AND locale=?`, key, language); err != nil {
			return err
		}
		out, err = draftIn(ctx, tx, key, language)
		return err
	})
	return out, err
}

func (s *Service) Draft(ctx context.Context, key, language string) (Draft, error) {
	out := Draft{Key: key, Locale: language, DraftInput: DraftInput{Images: []Image{}}, Variants: []Variant{}}
	if !keyPattern.MatchString(key) || !locale.Valid(language) {
		return out, ErrInvalid
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	return draftIn(ctx, tx, key, language)
}

func draftIn(ctx context.Context, tx *sql.Tx, key, language string) (Draft, error) {
	out := Draft{Key: key, Locale: language, DraftInput: DraftInput{Images: []Image{}}, Variants: []Variant{}}
	var publicSlug string
	err := tx.QueryRowContext(ctx, `SELECT d.version,d.slug,d.title,d.description,coalesce(d.category,''),coalesce(p.version,0),coalesce(p.slug,'')
		FROM store_localizations d LEFT JOIN store_publications p ON p.product=d.product AND p.locale=d.locale
		WHERE d.product=? AND d.locale=?`, key, language).
		Scan(&out.Version, &out.Slug, &out.Title, &out.Description, &out.Category, &out.PublishedVersion, &publicSlug)
	if errors.Is(err, sql.ErrNoRows) {
		if err = tx.QueryRowContext(ctx, "SELECT key FROM store_products WHERE key=?", key).Scan(&out.Key); err != nil {
			return out, err
		}
	} else if err != nil {
		return out, err
	}
	if out.PublishedVersion > 0 {
		out.Published = true
		out.PublicURL = ProductPath(language, publicSlug)
	}
	out.Images, err = readImages(ctx, tx, key, language, false)
	if err != nil {
		return out, err
	}
	out.Variants, err = readVariants(ctx, tx, key, false)
	return out, err
}

func ProductPath(language, slug string) string {
	return "/" + language + "/store/" + url.PathEscape(slug)
}

func readImages(ctx context.Context, tx *sql.Tx, key, language string, public bool) ([]Image, error) {
	table := "store_draft_images"
	if public {
		table = "store_published_images"
	}
	rows, err := tx.QueryContext(ctx, "SELECT media_id,alt FROM "+table+" WHERE product=? AND locale=? ORDER BY position", key, language)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Image{}
	for rows.Next() {
		var image Image
		if err = rows.Scan(&image.ID, &image.Alt); err != nil {
			return nil, err
		}
		out = append(out, image)
	}
	return out, rows.Err()
}

func (s *Service) SaveCategory(ctx context.Context, in Category) (Category, error) {
	if !keyPattern.MatchString(in.Key) || !locale.Valid(in.Locale) || !plain(in.Title, 120, true, false) || in.Version < 0 {
		return Category{}, ErrInvalid
	}
	in.Title = strings.TrimSpace(in.Title)
	err := s.write(ctx, in.Key, func(tx *sql.Tx) error {
		if in.Version == 0 {
			_, err := tx.ExecContext(ctx, "INSERT INTO store_categories VALUES(?,?,1,?)", in.Key, in.Locale, in.Title)
			return err
		}
		res, err := tx.ExecContext(ctx, "UPDATE store_categories SET version=version+1,title=? WHERE key=? AND locale=? AND version=?",
			in.Title, in.Key, in.Locale, in.Version)
		return changed(res, err)
	})
	if err != nil {
		return Category{}, err
	}
	in.Version++
	return in, nil
}

func readCategories(ctx context.Context, tx *sql.Tx, language string, public bool) ([]Category, error) {
	query := "SELECT key,locale,title,version FROM store_categories c WHERE locale=?"
	if public {
		query += ` AND EXISTS(SELECT 1 FROM store_publications p WHERE p.locale=c.locale AND p.category=c.key)`
	}
	rows, err := tx.QueryContext(ctx, query+" ORDER BY title,key", language)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Category{}
	for rows.Next() {
		var item Category
		if err = rows.Scan(&item.Key, &item.Locale, &item.Title, &item.Version); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Service) Workspace(ctx context.Context, language, cursor string) (Workspace, error) {
	out := Workspace{Products: []Entry{}}
	if !locale.Valid(language) || cursor != "" && !keyPattern.MatchString(cursor) {
		return out, ErrInvalid
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	out.Settings, err = readSettings(ctx, tx)
	if err != nil {
		return out, err
	}
	out.Categories, err = readCategories(ctx, tx, language, false)
	if err != nil {
		return out, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT p.key,coalesce(d.title,p.key),d.product IS NOT NULL,l.product IS NOT NULL
		FROM store_products p LEFT JOIN store_localizations d ON d.product=p.key AND d.locale=?
		LEFT JOIN store_publications l ON l.product=p.key AND l.locale=? WHERE p.key>? ORDER BY p.key LIMIT 31`, language, language, cursor)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		if len(out.Products) == 30 {
			out.Next = out.Products[29].Key
			break
		}
		var item Entry
		if err = rows.Scan(&item.Key, &item.Title, &item.Localized, &item.Published); err != nil {
			return out, err
		}
		out.Products = append(out.Products, item)
	}
	return out, rows.Err()
}
