// Package portability exports owner content, never authentication or learner data.
package portability

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/mbougarne/modawana/backend/internal/bundle"
	"github.com/mbougarne/modawana/backend/internal/content"
	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/media"
	"github.com/mbougarne/modawana/backend/internal/siteconfig"
	"github.com/mbougarne/modawana/backend/internal/storage"
	"io"
	"os"
	"path/filepath"
)

type Collection struct {
	Key     string                     `json:"key"`
	Kind    string                     `json:"kind"`
	Locales []content.CollectionLocale `json:"locales"`
	Members []content.PlanMember       `json:"members"`
	Gates   map[string]string          `json:"gates"`
}
type Upload struct {
	ID      string `json:"id"`
	Path    string `json:"path"`
	Name    string `json:"name"`
	MIME    string `json:"mime"`
	Purpose string `json:"purpose"`
}
type Data struct {
	Schema        int                        `json:"schema"`
	Revisions     []content.PortableRevision `json:"revisions"`
	Taxonomy      []content.Term             `json:"taxonomy"`
	Collections   []Collection               `json:"collections"`
	Configuration map[string]json.RawMessage `json:"configuration"`
	Media         []Upload                   `json:"media"`
}
type Service struct {
	DB    *sql.DB
	Files *storage.Store
}

func keys(ctx context.Context, d *sql.DB, q string) ([]string, error) {
	rows, e := d.QueryContext(ctx, q)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var s string
		if e = rows.Scan(&s); e != nil {
			return nil, e
		}
		out = append(out, s)
		if len(out) > bundle.MaxFiles {
			return nil, bundle.ErrInvalid
		}
	}
	return out, rows.Err()
}
func put(root, p string, b []byte) error {
	target := filepath.Join(root, filepath.FromSlash(p))
	if e := os.MkdirAll(filepath.Dir(target), 0700); e != nil {
		return e
	}
	return os.WriteFile(target, b, 0600)
}
func (s *Service) Export(ctx context.Context, out io.Writer) error {
	return s.Files.Exclusive(ctx, func() error {
		root, e := os.MkdirTemp("", "blog-export-")
		if e != nil {
			return e
		}
		defer os.RemoveAll(root)
		if e = db.Snapshot(ctx, s.DB, filepath.Join(root, "blog.sqlite")); e != nil {
			return e
		}
		snapshot, e := db.Open(ctx, root)
		if e != nil {
			return e
		}
		defer snapshot.Close()
		cs := &content.Service{DB: snapshot, Files: s.Files}
		cfg := &siteconfig.Service{DB: snapshot, Files: s.Files}
		data := Data{Schema: 1, Configuration: map[string]json.RawMessage{}, Taxonomy: []content.Term{}, Collections: []Collection{}, Media: []Upload{}}
		data.Revisions, e = cs.PortableRevisions(ctx)
		if e != nil {
			return e
		}
		paths := []string{"data.json"}
		for i := range data.Revisions {
			r := &data.Revisions[i]
			if e = put(root, r.BodyPath, []byte(r.Input.Markdown)); e != nil {
				return e
			}
			paths = append(paths, r.BodyPath)
			r.Input.Markdown = ""
		}
		for _, locale := range []string{"en", "ar", "fr"} {
			terms, e := cs.Taxonomy(ctx, locale, true)
			if e != nil {
				return e
			}
			data.Taxonomy = append(data.Taxonomy, terms...)
		}
		list, e := keys(ctx, snapshot, "SELECT key FROM collections WHERE removed=0 ORDER BY key")
		if e != nil {
			return e
		}
		for _, key := range list {
			c, e := cs.Collection(ctx, key)
			if e != nil {
				return e
			}
			v := Collection{Key: key, Kind: c.Kind, Locales: c.Locales, Members: c.DraftMembers, Gates: map[string]string{}}
			rows, e := snapshot.QueryContext(ctx, "SELECT i.key,g.assessment_id FROM collection_plan_gates g JOIN content_items i ON i.id=g.item_id WHERE g.plan_id=?", c.DraftPlanID)
			if e != nil {
				return e
			}
			for rows.Next() {
				var key, id string
				if e = rows.Scan(&key, &id); e != nil {
					rows.Close()
					return e
				}
				v.Gates[key] = id
			}
			e = rows.Err()
			rows.Close()
			if e != nil {
				return e
			}
			data.Collections = append(data.Collections, v)
		}
		for _, kind := range []string{"site", "profile", "labels"} {
			d, e := cfg.Get(ctx, kind)
			if e != nil {
				return e
			}
			data.Configuration[kind] = d.Snapshot
		}
		list, e = keys(ctx, snapshot, `SELECT media_id FROM revision_media UNION SELECT cm.media_id FROM config_revision_media cm JOIN config_documents d ON d.applied_revision_id=cm.revision_id ORDER BY media_id`)
		if e != nil {
			return e
		}
		for _, id := range list {
			m, e := media.Get(ctx, snapshot, id)
			if e != nil {
				return e
			}
			r, e := s.Files.OpenUpload(m.File)
			if e != nil {
				return e
			}
			p := "media/" + id + ".bin"
			b, e := io.ReadAll(r)
			r.Close()
			if e != nil {
				return e
			}
			if e = put(root, p, b); e != nil {
				return e
			}
			paths = append(paths, p)
			data.Media = append(data.Media, Upload{id, p, m.Filename, m.MIME, m.Purpose})
		}
		b, e := json.Marshal(data)
		if e != nil || len(b) > bundle.MaxManifest {
			return bundle.ErrInvalid
		}
		if e = put(root, "data.json", b); e != nil {
			return e
		}
		return bundle.Write(out, "content", root, paths)
	})
}
