package portability

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/mbougarne/modawana/backend/internal/bundle"
	"github.com/mbougarne/modawana/backend/internal/content"
	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/media"
	"github.com/mbougarne/modawana/backend/internal/siteconfig"
	"github.com/mbougarne/modawana/backend/internal/storage"
	"os"
	"path/filepath"
	"strings"
)

var tables = []string{"media", "categories", "category_locales", "tags", "tag_locales", "content_items", "content_versions", "revisions", "revision_tags", "revision_media", "revision_media_presentations", "collections", "collection_locales", "collection_items", "collection_plans", "collection_plan_members", "collection_plan_state", "assessments", "quiz_bindings", "collection_plan_gates", "config_documents", "config_revisions", "config_revision_media"}

type ImportResult struct {
	Revisions      int  `json:"revisions"`
	Media          int  `json:"media"`
	Collections    int  `json:"collections"`
	ReviewRequired bool `json:"review_required"`
}

func remap(id string, m map[string]string) (string, error) {
	if id == "" {
		return "", nil
	}
	if m[id] == "" {
		return "", bundle.ErrInvalid
	}
	return m[id], nil
}
func (s *Service) Import(ctx context.Context, owner, source string, authorize func(*sql.Tx) error) (ImportResult, error) {
	var result ImportResult
	extracted, manifest, e := bundle.Read(source, "content")
	if e != nil {
		return result, e
	}
	defer os.RemoveAll(extracted)
	raw, e := os.ReadFile(filepath.Join(extracted, "data.json"))
	if e != nil || len(raw) > bundle.MaxManifest {
		return result, bundle.ErrInvalid
	}
	var data Data
	if bundle.Decode(raw, &data) != nil || data.Schema != 1 || len(data.Revisions) > 2000 || len(data.Media) > 10000 || len(data.Collections) > 1000 || len(data.Taxonomy) > 3000 || len(data.Configuration) != 3 {
		return result, bundle.ErrInvalid
	}
	expected := map[string]bool{"data.json": true}
	ids := map[string]bool{}
	for _, r := range data.Revisions {
		if !media.ID.MatchString(r.ID) || ids[r.ID] || r.BodyPath != "content/"+r.ID+".md" || r.Input.Markdown != "" {
			return result, bundle.ErrInvalid
		}
		ids[r.ID] = true
		expected[r.BodyPath] = true
	}
	for _, m := range data.Media {
		if !media.ID.MatchString(m.ID) || ids[m.ID] || m.Path != "media/"+m.ID+".bin" {
			return result, bundle.ErrInvalid
		}
		ids[m.ID] = true
		expected[m.Path] = true
	}
	if len(expected) != len(manifest.Files) {
		return result, bundle.ErrInvalid
	}
	for _, f := range manifest.Files {
		if !expected[f.Path] {
			return result, bundle.ErrInvalid
		}
	}
	stage, e := os.MkdirTemp("", "blog-import-")
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
	cfg := &siteconfig.Service{DB: staged, Files: files}
	mediaIDs := map[string]string{}
	revisionIDs := map[string]string{}
	assessmentIDs := map[string]string{}
	for _, m := range data.Media {
		f, e := os.Open(filepath.Join(extracted, m.Path))
		if e != nil {
			return result, e
		}
		v, e := ms.Upload(ctx, owner, m.Name, m.MIME, m.Purpose, f)
		f.Close()
		if e != nil {
			return result, e
		}
		mediaIDs[m.ID] = v.ID
	}
	versions := map[string]int64{}
	for _, t := range data.Taxonomy {
		k := t.Kind + "/" + t.Key
		v := versions[k]
		out, e := cs.SaveTerm(ctx, t.Kind, t.Key, t.Locale, content.TaxonomyInput{ExpectedVersion: &v, Label: t.Label, Slug: t.Slug})
		if e != nil {
			return result, e
		}
		versions[k] = out.Version
	}
	for _, c := range data.Collections {
		if len(c.Locales) == 0 {
			return result, bundle.ErrInvalid
		}
		var version int64
		for _, l := range c.Locales {
			v, e := cs.SaveCollection(ctx, c.Key, l.Locale, content.CollectionInput{ExpectedVersion: &version, Kind: c.Kind, Slug: l.Slug, Title: l.Title, Summary: l.Summary, Visible: false})
			if e != nil {
				return result, e
			}
			version = v.Version
		}
	}
	versions = map[string]int64{}
	current := map[string]string{}
	pending := append([]content.PortableRevision{}, data.Revisions...)
	for len(pending) > 0 {
		progress := false
		next := []content.PortableRevision{}
		for _, r := range pending {
			in := r.Input
			if in.SourceRevisionID != "" && revisionIDs[in.SourceRevisionID] == "" {
				next = append(next, r)
				continue
			}
			in.SourceRevisionID = revisionIDs[in.SourceRevisionID]
			body, e := os.ReadFile(filepath.Join(extracted, r.BodyPath))
			if e != nil {
				return result, e
			}
			in.Markdown = string(body)
			for old, id := range mediaIDs {
				in.Markdown = strings.ReplaceAll(in.Markdown, "/media/"+old, "/media/"+id)
			}
			if in.CoverMediaID, e = remap(in.CoverMediaID, mediaIDs); e != nil {
				return result, e
			}
			for i := range in.Presentations {
				p := &in.Presentations[i]
				if p.MediaID, e = remap(p.MediaID, mediaIDs); e != nil {
					return result, e
				}
				if p.PosterID, e = remap(p.PosterID, mediaIDs); e != nil {
					return result, e
				}
				if p.TrackID, e = remap(p.TrackID, mediaIDs); e != nil {
					return result, e
				}
			}
			oldAssessment := ""
			if in.Quiz != nil {
				q := *in.Quiz
				oldAssessment = q.AssessmentID
				q.AssessmentID = assessmentIDs[oldAssessment]
				if r.Locale != in.SourceLocale && q.AssessmentID == "" {
					return result, bundle.ErrInvalid
				}
				in.Quiz = &q
			}
			key := r.Key + "/" + r.Locale
			version := versions[key]
			in.ExpectedVersion = &version
			out, e := cs.StagePortableRevision(ctx, owner, r.Key, r.Locale, in)
			if e != nil {
				return result, fmt.Errorf("revision import rejected: %w", e)
			}
			versions[key] = out.Version
			revisionIDs[r.ID] = out.RevisionID
			if in.Quiz != nil {
				var id string
				if e = staged.QueryRowContext(ctx, "SELECT assessment_id FROM quiz_bindings WHERE revision_id=?", out.RevisionID).Scan(&id); e != nil {
					return result, e
				}
				assessmentIDs[oldAssessment] = id
			}
			if r.Current {
				if current[key] != "" {
					return result, bundle.ErrInvalid
				}
				current[key] = out.RevisionID
			}
			progress = true
		}
		if !progress {
			return result, bundle.ErrInvalid
		}
		pending = next
	}
	if len(current) != len(versions) {
		return result, bundle.ErrInvalid
	}
	for key, id := range current {
		parts := strings.Split(key, "/")
		if len(parts) != 2 {
			return result, bundle.ErrInvalid
		}
		if _, e = staged.ExecContext(ctx, "UPDATE content_versions SET slug=(SELECT slug FROM revisions WHERE id=?),draft_revision_id=? WHERE item_id=(SELECT id FROM content_items WHERE key=?) AND locale=?", id, id, parts[0], parts[1]); e != nil {
			return result, e
		}
	}
	for _, c := range data.Collections {
		d, e := cs.Collection(ctx, c.Key)
		if e != nil {
			return result, e
		}
		if len(c.Members) == 0 {
			if len(c.Gates) > 0 {
				return result, bundle.ErrInvalid
			}
			continue
		}
		if _, e = cs.SavePlan(ctx, owner, c.Key, content.PlanInput{ExpectedVersion: &d.Version, Members: c.Members}); e != nil {
			return result, e
		}
		gates := map[string]string{}
		for key, old := range c.Gates {
			if gates[key], e = remap(old, assessmentIDs); e != nil {
				return result, e
			}
		}
		if e = cs.StagePortableGates(ctx, owner, c.Key, gates); e != nil {
			return result, e
		}
	}
	for _, kind := range []string{"site", "profile", "labels"} {
		b := data.Configuration[kind]
		if kind == "site" {
			var v siteconfig.Site
			if bundle.Decode(b, &v) != nil {
				return result, bundle.ErrInvalid
			}
			if v.LogoID, e = remap(v.LogoID, mediaIDs); e != nil {
				return result, e
			}
			if v.FaviconID, e = remap(v.FaviconID, mediaIDs); e != nil {
				return result, e
			}
			b, _ = json.Marshal(v)
		} else if kind == "profile" {
			var v siteconfig.Profile
			if bundle.Decode(b, &v) != nil {
				return result, bundle.ErrInvalid
			}
			if v.PortraitID, e = remap(v.PortraitID, mediaIDs); e != nil {
				return result, e
			}
			if v.BannerID, e = remap(v.BannerID, mediaIDs); e != nil {
				return result, e
			}
			b, _ = json.Marshal(v)
		}
		if e = cfg.StageImport(ctx, owner, kind, b); e != nil {
			return result, e
		}
	}
	e = s.Files.Exclusive(ctx, func() error {
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
		for _, table := range tables {
			var count int
			if e = tx.QueryRowContext(ctx, "SELECT count(*) FROM "+table).Scan(&count); e != nil {
				return e
			}
			if count != 0 {
				return fmt.Errorf("%w: import requires an empty content/configuration installation", db.ErrVersionConflict)
			}
		}
		rows, e := staged.QueryContext(ctx, "SELECT markdown_path,sha256,byte_size FROM revisions UNION ALL SELECT storage_key,sha256,byte_size FROM media")
		if e != nil {
			return e
		}
		for rows.Next() {
			var f storage.File
			if e = rows.Scan(&f.Key, &f.SHA256, &f.Size); e != nil {
				rows.Close()
				return e
			}
			r, e := os.Open(filepath.Join(stage, f.Key))
			if e != nil {
				rows.Close()
				return e
			}
			e = s.Files.InstallLocked(f, r)
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
			if e != nil {
				return e
			}
		}
		return tx.Commit()
	})
	if e != nil {
		return result, e
	}
	return ImportResult{len(data.Revisions), len(data.Media), len(data.Collections), true}, nil
}
