package content

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/mbougarne/modawana/backend/internal/access"
	"github.com/mbougarne/modawana/backend/internal/media"
	"github.com/mbougarne/modawana/backend/internal/storage"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	goldtext "github.com/yuin/goldmark/text"
)

type EditorialSource struct {
	Key      string `json:"key"`
	Locale   string `json:"locale"`
	Revision string `json:"revision_id"`
	Title    string `json:"title"`
	Summary  string `json:"summary"`
	Excerpt  string `json:"excerpt,omitempty"`
	Cover    string `json:"cover,omitempty"`
	CoverID  string `json:"cover_id,omitempty"`
}

// EditorialContext exposes only selected fields from an exact, public and
// listed publication. Page-builder payloads and assessments are never read.
func (s *Service) EditorialContext(ctx context.Context, key, locale, expected string, excerpt, cover bool) (EditorialSource, error) {
	var out EditorialSource
	if !keyPattern.MatchString(key) || !localeOK(locale) || expected != "" && !media.ID.MatchString(expected) {
		return out, ErrInvalid
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	v, err := loadVersion(ctx, tx, key, locale)
	if err != nil {
		return out, err
	}
	if v.Published == "" {
		return out, sql.ErrNoRows
	}
	if expected != "" && v.Published != expected {
		return out, ErrReview
	}
	if err = s.CheckEditorialSource(ctx, tx, key, locale, v.Published, ""); err != nil {
		return out, err
	}
	var file storage.File
	var coverID string
	err = tx.QueryRowContext(ctx, `SELECT title,summary,markdown_path,sha256,byte_size,coalesce(cover_media_id,'')
		FROM revisions WHERE id=?`, v.Published).Scan(&out.Title, &out.Summary, &file.Key, &file.SHA256, &file.Size, &coverID)
	if err != nil {
		return EditorialSource{}, err
	}
	out.Key, out.Locale, out.Revision = key, locale, v.Published
	if excerpt && !file.HasPageBlocks() {
		raw, err := s.Files.Read(file)
		if err != nil {
			return EditorialSource{}, err
		}
		out.Excerpt = editorialExcerpt(raw)
	}
	if cover && media.ID.MatchString(coverID) {
		id, err := s.editorialImage(ctx, tx, coverID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return EditorialSource{}, err
		}
		if err == nil {
			out.CoverID, out.Cover = id, "/media/"+id
		}
	}
	return out, nil
}

// CheckEditorialSource rechecks the publication immediately before reserving a
// provider request, including the anonymous media policy for a selected image.
func (s *Service) CheckEditorialSource(ctx context.Context, tx *sql.Tx, key, locale, revision, cover string) error {
	if _, err := s.PublicRevisionPath(ctx, tx, key, locale, revision); err != nil {
		return err
	}
	var unlisted bool
	if err := tx.QueryRowContext(ctx, `SELECT coalesce(json_extract(seo,'$.no_index'),0) FROM revisions WHERE id=?`, revision).Scan(&unlisted); err != nil {
		return err
	}
	if unlisted {
		return sql.ErrNoRows
	}
	if cover != "" {
		_, err := s.editorialImage(ctx, tx, cover)
		return err
	}
	return nil
}

func (s *Service) editorialImage(ctx context.Context, tx *sql.Tx, id string) (string, error) {
	decision, err := s.Access.ResolveIn(ctx, tx, access.Request{Principal: access.Principal{}, Surface: access.Media, ResourceID: id, Method: "GET"})
	if err != nil {
		return "", err
	}
	if !decision.AllowsBody() {
		return "", sql.ErrNoRows
	}
	var file storage.File
	err = tx.QueryRowContext(ctx, `SELECT storage_key,sha256,byte_size FROM media WHERE id=? AND kind='image'
		AND mime_type IN ('image/jpeg','image/png','image/webp') AND byte_size<=5242880`, id).Scan(&file.Key, &file.SHA256, &file.Size)
	if err != nil {
		return "", err
	}
	if !s.Files.UploadPresent(file) {
		return "", sql.ErrNoRows
	}
	return id, nil
}

const maxEditorialExcerpt = 1800

func editorialExcerpt(raw []byte) string {
	tree := goldmark.DefaultParser().Parse(goldtext.NewReader(raw))
	var out strings.Builder
	count := 0
	space := false
	add := func(part []byte) {
		for len(part) > 0 && count < maxEditorialExcerpt {
			r, size := utf8.DecodeRune(part)
			part = part[size:]
			if unicode.IsSpace(r) {
				space = out.Len() > 0
				continue
			}
			if unicode.IsControl(r) {
				continue
			}
			if space && count+1 < maxEditorialExcerpt {
				out.WriteByte(' ')
				count++
			}
			space = false
			out.WriteRune(r)
			count++
		}
	}
	_ = ast.Walk(tree, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if count >= maxEditorialExcerpt {
			return ast.WalkStop, nil
		}
		if !entering {
			if n.Type() == ast.TypeBlock {
				space = out.Len() > 0
			}
			return ast.WalkContinue, nil
		}
		switch x := n.(type) {
		case *ast.Image, *ast.CodeBlock, *ast.FencedCodeBlock, *ast.HTMLBlock, *ast.RawHTML, *ast.AutoLink:
			return ast.WalkSkipChildren, nil
		case *ast.Text:
			add(x.Segment.Value(raw))
			if x.SoftLineBreak() || x.HardLineBreak() {
				space = out.Len() > 0
			}
		case *ast.String:
			add(x.Value)
		}
		return ast.WalkContinue, nil
	})
	return strings.TrimSpace(out.String())
}
