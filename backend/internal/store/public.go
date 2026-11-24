package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/mbougarne/modawana/backend/internal/locale"
	"github.com/mbougarne/modawana/backend/internal/media"
	"github.com/mbougarne/modawana/backend/internal/siteconfig"
)

type PublicCategory struct {
	Key   string `json:"key"`
	Title string `json:"title"`
}

type PublicVariant struct {
	SKU        string `json:"sku"`
	Label      string `json:"label"`
	PriceMinor int64  `json:"price_minor"`
	InStock    bool   `json:"in_stock"`
}

type PublicProduct struct {
	Key         string          `json:"key"`
	Locale      string          `json:"locale"`
	Slug        string          `json:"slug"`
	URL         string          `json:"url"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	Category    string          `json:"category,omitempty"`
	Published   string          `json:"published_at"`
	PriceMinor  int64           `json:"price_minor"`
	Images      []Image         `json:"images"`
	Variants    []PublicVariant `json:"variants,omitempty"`
}

type Alternate struct {
	Locale string `json:"locale"`
	URL    string `json:"url"`
}

type PublicPage struct {
	Currency     string           `json:"currency"`
	Digits       int              `json:"digits"`
	Product      *PublicProduct   `json:"product,omitempty"`
	Products     []PublicProduct  `json:"products,omitempty"`
	Categories   []PublicCategory `json:"categories,omitempty"`
	Alternatives []Alternate      `json:"alternatives,omitempty"`
	Total        int              `json:"total"`
	Next         string           `json:"next_cursor,omitempty"`
}

type Filter struct {
	Locale, Query, Category, Cursor string
	Limit                           int
}

const publicRows = ` FROM store_publications p WHERE p.locale=?
	AND EXISTS(SELECT 1 FROM store_variants v WHERE v.product=p.product AND v.active=1)`
const publicColumns = `p.product,p.locale,p.slug,p.title,p.description,coalesce(p.category,''),p.published_at,
	(SELECT min(v.price_minor) FROM store_variants v WHERE v.product=p.product AND v.active=1)`

func storefront(ctx context.Context, tx *sql.Tx, language string) (Settings, error) {
	if !locale.Valid(language) {
		return Settings{}, sql.ErrNoRows
	}
	cfg, err := readSettings(ctx, tx)
	if err != nil || !cfg.Enabled {
		if err == nil {
			err = sql.ErrNoRows
		}
		return Settings{}, err
	}
	visible, err := siteconfig.LocaleVisibleIn(ctx, tx, language)
	if err == nil && !visible {
		err = sql.ErrNoRows
	}
	return cfg, err
}

func productFrom(row scanner) (PublicProduct, error) {
	var out PublicProduct
	err := row.Scan(&out.Key, &out.Locale, &out.Slug, &out.Title, &out.Description, &out.Category, &out.Published, &out.PriceMinor)
	if err == nil {
		out.URL = ProductPath(out.Locale, out.Slug)
	}
	return out, err
}

func (s *Service) Catalog(ctx context.Context, f Filter) (PublicPage, error) {
	out := PublicPage{Products: []PublicProduct{}, Categories: []PublicCategory{}}
	if !plain(f.Query, 200, false, false) || f.Limit < 1 || f.Limit > 50 || f.Cursor != "" && !slugPattern.MatchString(f.Cursor) ||
		f.Category != "" && !keyPattern.MatchString(f.Category) {
		return out, ErrInvalid
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	cfg, err := storefront(ctx, tx, f.Locale)
	if err != nil {
		return out, err
	}
	out.Currency, out.Digits = cfg.Currency, cfg.Digits
	if err = alternates(ctx, tx, &out, ""); err != nil {
		return out, err
	}
	categories, err := readCategories(ctx, tx, f.Locale, true)
	if err != nil {
		return out, err
	}
	for _, item := range categories {
		out.Categories = append(out.Categories, PublicCategory{item.Key, item.Title})
	}
	base := publicRows
	args := []any{f.Locale}
	if f.Category != "" {
		base += " AND p.category=?"
		args = append(args, f.Category)
	}
	if query := strings.TrimSpace(f.Query); query != "" {
		base += " AND (instr(lower(p.title),lower(?))>0 OR instr(lower(p.description),lower(?))>0)"
		args = append(args, query, query)
	}
	if err = tx.QueryRowContext(ctx, "SELECT count(*)"+base, args...).Scan(&out.Total); err != nil {
		return out, err
	}
	args = append(args, f.Cursor, f.Limit+1)
	rows, err := tx.QueryContext(ctx, "SELECT "+publicColumns+base+" AND p.slug>? ORDER BY p.slug LIMIT ?", args...)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		if len(out.Products) == f.Limit {
			out.Next = out.Products[f.Limit-1].Slug
			break
		}
		item, err := productFrom(rows)
		if err != nil {
			rows.Close()
			return out, err
		}
		out.Products = append(out.Products, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	for i := range out.Products {
		product := &out.Products[i]
		product.Images, err = readImages(ctx, tx, product.Key, f.Locale, true)
		if err != nil {
			return out, err
		}
	}
	return out, nil
}

func (s *Service) PublicProduct(ctx context.Context, language, slug string) (PublicPage, error) {
	var out PublicPage
	if !slugPattern.MatchString(slug) {
		return out, sql.ErrNoRows
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	cfg, err := storefront(ctx, tx, language)
	if err != nil {
		return out, err
	}
	out.Currency, out.Digits = cfg.Currency, cfg.Digits
	product, err := productFrom(tx.QueryRowContext(ctx, "SELECT "+publicColumns+publicRows+" AND p.slug=?", language, slug))
	if err != nil {
		return out, err
	}
	product.Images, err = readImages(ctx, tx, product.Key, language, true)
	if err != nil {
		return out, err
	}
	variants, err := readVariants(ctx, tx, product.Key, true)
	if err != nil {
		return out, err
	}
	for _, v := range variants {
		label := v.Labels[language]
		if label == "" {
			label = v.SKU
		}
		product.Variants = append(product.Variants, PublicVariant{v.SKU, label, v.PriceMinor, v.Stock > 0})
	}
	out.Product = &product
	if err = alternates(ctx, tx, &out, product.Key); err != nil {
		return PublicPage{}, err
	}
	return out, nil
}

func alternates(ctx context.Context, tx *sql.Tx, out *PublicPage, product string) error {
	for _, language := range locale.Codes() {
		visible, err := siteconfig.LocaleVisibleIn(ctx, tx, language)
		if err != nil {
			return err
		}
		if !visible {
			continue
		}
		path := "/" + language + "/store"
		if product != "" {
			var slug string
			err = tx.QueryRowContext(ctx, "SELECT slug FROM store_publications WHERE product=? AND locale=?", product, language).Scan(&slug)
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return err
			}
			path = ProductPath(language, slug)
		}
		out.Alternatives = append(out.Alternatives, Alternate{language, path})
	}
	return nil
}

func (s *Service) OwnsImage(ctx context.Context, id string) (bool, error) {
	if !media.ID.MatchString(id) {
		return false, nil
	}
	var ok bool
	err := s.DB.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM store_media WHERE media_id=?)", id).Scan(&ok)
	return ok, err
}

func (s *Service) ImageVisible(ctx context.Context, tx *sql.Tx, id string) (bool, error) {
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT i.locale FROM store_published_images i
		WHERE i.media_id=? AND EXISTS(SELECT 1 FROM store_settings WHERE id=1 AND enabled=1)
		AND EXISTS(SELECT 1 FROM store_variants v WHERE v.product=i.product AND v.active=1)`, id)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	var languages []string
	for rows.Next() {
		var language string
		if err = rows.Scan(&language); err != nil {
			return false, err
		}
		languages = append(languages, language)
	}
	if err = rows.Err(); err != nil {
		return false, err
	}
	rows.Close()
	for _, language := range languages {
		ok, err := siteconfig.LocaleVisibleIn(ctx, tx, language)
		if ok || err != nil {
			return ok, err
		}
	}
	return false, nil
}
