package portability

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mbougarne/modawana/backend/internal/bundle"
	"github.com/mbougarne/modawana/backend/internal/content"
	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/media"
	"github.com/mbougarne/modawana/backend/internal/storage"
)

type NotionResult struct {
	ImportResult
	Skipped int `json:"skipped"`
}

func (s *Service) ImportNotion(ctx context.Context, owner, source string, options NotionOptions, authorize func(*sql.Tx) error) (NotionResult, error) {
	result := NotionResult{ImportResult: ImportResult{ReviewRequired: true}}
	a, e := s.notionPreview(ctx, source, options)
	if e != nil {
		return result, e
	}
	defer a.zip.Close()
	result.Skipped = a.view.Skipped
	if len(a.view.Issues) != 0 {
		return result, bundle.ErrInvalid
	}
	stage, e := os.MkdirTemp("", "blog-notion-stage-")
	if e != nil {
		return result, e
	}
	defer os.RemoveAll(stage)
	staged, e := db.Open(ctx, stage)
	if e != nil {
		return result, e
	}
	defer staged.Close()
	if _, e = staged.ExecContext(ctx, "INSERT INTO owner(id) VALUES(?)", owner); e != nil {
		return result, e
	}
	files, e := storage.Open(stage)
	if e != nil {
		return result, e
	}
	defer files.Close()
	cs := &content.Service{DB: staged, Files: files}
	cs.Membership = cs.AttachMember
	ms := media.New(staged, files, nil)
	mediaIDs := map[string]string{}
	assetPaths := make([]string, 0, len(a.assets))
	for name := range a.assets {
		assetPaths = append(assetPaths, name)
	}
	sort.Strings(assetPaths)
	for _, name := range assetPaths {
		body, e := readNotionFile(a.files[name], 50<<20)
		if e != nil {
			return result, e
		}
		declared := http.DetectContentType(body)
		if strings.EqualFold(path.Ext(name), ".vtt") {
			declared = "text/vtt"
		}
		m, e := ms.Upload(ctx, owner, path.Base(name), declared, "content", bytes.NewReader(body))
		if e != nil {
			return result, e
		}
		mediaIDs[name] = m.ID
		result.Media++
	}
	lessons := 0
	for _, p := range a.view.Pages {
		if !p.Existing && p.Kind == "lesson" {
			lessons++
		}
	}
	zero := int64(0)
	if lessons > 0 {
		var count int
		if e = s.DB.QueryRowContext(ctx, "SELECT count(*) FROM collections WHERE key=?", a.view.CollectionKey).Scan(&count); e != nil {
			return result, e
		}
		// Never replace a manually edited or already imported collection plan.
		if count != 0 {
			return result, db.ErrVersionConflict
		}
		if _, e = cs.SaveCollection(ctx, a.view.CollectionKey, options.Locale, content.CollectionInput{ExpectedVersion: &zero, Kind: "learning", Slug: a.view.CollectionSlug, Title: a.view.CollectionTitle, Visible: false}); e != nil {
			return result, e
		}
		result.Collections = 1
	}
	members := []content.PlanMember{}
	for i := range a.view.Pages {
		p := &a.view.Pages[i]
		if p.Existing {
			continue
		}
		body, e := a.rewrite(p, mediaIDs)
		if e != nil {
			return result, e
		}
		in := content.DraftInput{ExpectedVersion: &zero, Kind: p.Kind, SourceLocale: options.Locale, Slug: p.Slug, Title: p.Title, Markdown: body}
		if p.Kind == "lesson" {
			members = append(members, content.PlanMember{Key: p.Key})
			in.CollectionKey, in.Position = a.view.CollectionKey, len(members)
		}
		if _, e = cs.Save(ctx, owner, p.Key, options.Locale, "", in); e != nil {
			return result, e
		}
		result.Revisions++
	}
	if len(members) > 0 {
		collection, e := cs.Collection(ctx, a.view.CollectionKey)
		if e != nil {
			return result, e
		}
		if _, e = cs.SavePlan(ctx, owner, a.view.CollectionKey, content.PlanInput{ExpectedVersion: &collection.Version, Members: members}); e != nil {
			return result, e
		}
	}
	if e = s.installNotion(ctx, staged, stage, authorize); e != nil {
		return NotionResult{}, e
	}
	return result, nil
}

// Only staged content/media/collection rows are copied. Existing records, owner
// credentials, configuration, publication pointers and active plans are untouched.
func (s *Service) installNotion(ctx context.Context, staged *sql.DB, stage string, authorize func(*sql.Tx) error) error {
	return s.Files.Exclusive(ctx, func() error {
		tx, e := s.DB.BeginTx(ctx, nil)
		if e != nil {
			return e
		}
		defer tx.Rollback()
		if authorize != nil {
			if e = authorize(tx); e != nil {
				return e
			}
		}
		if _, e = tx.ExecContext(ctx, "PRAGMA defer_foreign_keys=ON"); e != nil {
			return e
		}
		// Check stable identities and route collisions again inside the commit
		// transaction, including another import winning after the preview.
		for _, query := range []struct{ source, target string }{
			{"SELECT key FROM content_items", "SELECT count(*) FROM content_items WHERE key=?"},
			{"SELECT key FROM collections", "SELECT count(*) FROM collections WHERE key=?"},
			{"SELECT locale || '/' || slug FROM content_versions", "SELECT count(*) FROM content_versions WHERE locale || '/' || slug=?"},
			{"SELECT locale || '/' || slug FROM collection_locales", "SELECT count(*) FROM collection_locales WHERE locale || '/' || slug=?"},
		} {
			values, e := keys(ctx, staged, query.source)
			if e != nil {
				return e
			}
			for _, value := range values {
				var count int
				if e = tx.QueryRowContext(ctx, query.target, value).Scan(&count); e != nil {
					return e
				}
				if count != 0 {
					return db.ErrVersionConflict
				}
			}
		}
		rows, e := staged.QueryContext(ctx, "SELECT markdown_path,sha256,byte_size FROM revisions UNION ALL SELECT storage_key,sha256,byte_size FROM media")
		if e != nil {
			return e
		}
		for rows.Next() {
			var file storage.File
			if e = rows.Scan(&file.Key, &file.SHA256, &file.Size); e != nil {
				rows.Close()
				return e
			}
			r, e := os.Open(filepath.Join(stage, file.Key))
			if e != nil {
				rows.Close()
				return e
			}
			e = s.Files.InstallLocked(file, r)
			r.Close()
			if e != nil {
				rows.Close()
				return e
			}
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		for _, table := range tables {
			rows, e := staged.QueryContext(ctx, "SELECT * FROM "+table)
			if e != nil {
				return e
			}
			columns, e := rows.Columns()
			if e != nil {
				rows.Close()
				return e
			}
			holders := make([]string, len(columns))
			for i := range holders {
				holders[i] = "?"
			}
			query := "INSERT INTO " + table + " (" + strings.Join(columns, ",") + ") VALUES (" + strings.Join(holders, ",") + ")"
			for rows.Next() {
				values := make([]any, len(columns))
				dest := make([]any, len(columns))
				for i := range values {
					dest[i] = &values[i]
				}
				if e = rows.Scan(dest...); e != nil {
					rows.Close()
					return e
				}
				if _, e = tx.ExecContext(ctx, query, values...); e != nil {
					rows.Close()
					return e
				}
			}
			e = rows.Err()
			rows.Close()
			if e != nil && !errors.Is(e, sql.ErrNoRows) {
				return e
			}
		}
		return tx.Commit()
	})
}
