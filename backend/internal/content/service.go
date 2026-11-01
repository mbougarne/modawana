package content

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/mbougarne/modawana/backend/internal/learning"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mbougarne/modawana/backend/internal/access"
	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/media"
	"github.com/mbougarne/modawana/backend/internal/storage"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

var ErrInvalid = errors.New("invalid content fields")
var ErrIdempotency = errors.New("idempotency conflict")
var ErrReview = errors.New("translation review required")
var ErrLocked = errors.New("lesson locked")
var keyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,119}$`)
var slugPattern = regexp.MustCompile(`^[\p{L}\p{N}][\p{L}\p{N}_-]{0,159}$`)
var retryPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)

const MaxBody = 1 << 20

func localeOK(v string) bool { return v == "en" || v == "ar" || v == "fr" }
func strOK(v string, max int, empty bool) bool {
	return utf8.ValidString(v) && !strings.ContainsRune(v, 0) && utf8.RuneCountInString(v) <= max && (empty || strings.TrimSpace(v) != "")
}
func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}
func hash(v any) string {
	b, _ := json.Marshal(v)
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

type Presentation struct {
	MediaID    string `json:"media_id"`
	PosterID   string `json:"poster_media_id,omitempty"`
	TrackID    string `json:"caption_track_media_id,omitempty"`
	Caption    string `json:"caption"`
	Transcript string `json:"transcript"`
}
type DraftInput struct {
	ExpectedCollectionVersion *int64               `json:"expected_collection_version,omitempty"`
	Quiz                      *learning.Definition `json:"quiz,omitempty"`
	ExpectedVersion           *int64               `json:"expected_version"`
	Kind                      string               `json:"kind"`
	SourceLocale              string               `json:"source_locale"`
	Slug                      string               `json:"slug"`
	Title                     string               `json:"title"`
	Summary                   string               `json:"summary"`
	Markdown                  string               `json:"markdown"`
	SourceRevisionID          string               `json:"source_revision_id"`
	CategoryKey               string               `json:"category_key"`
	TagKeys                   []string             `json:"tag_keys"`
	CoverMediaID              string               `json:"cover_media_id"`
	Presentations             []Presentation       `json:"media_presentations"`
	CollectionKey             string               `json:"collection_key"`
	Position                  int                  `json:"position"`
}
type Result struct {
	RevisionID string `json:"revision_id"`
	Version    int64  `json:"version"`
	Created    bool   `json:"created"`
}
type Service struct {
	DB              *sql.DB
	Files           *storage.Store
	Access          *access.Live
	PublicationHook func(context.Context, *sql.Tx, string, string, bool) error
	// Membership is installed by 015; earlier saves reject unsupported collection inputs.
	Membership func(context.Context, *sql.Tx, string, string, string, int) error
}
type version struct {
	ID, Item, Key, Kind, Source, Locale, Slug, Draft, Published, First string
	Number                                                             int64
}
type query interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func loadVersion(ctx context.Context, q query, key, locale string) (version, error) {
	var v version
	e := q.QueryRowContext(ctx, `SELECT v.id,i.id,i.key,i.kind,i.source_locale,v.locale,v.slug,v.draft_revision_id,coalesce(v.published_revision_id,''),coalesce(v.first_published_at,''),v.version FROM content_versions v JOIN content_items i ON i.id=v.item_id WHERE i.key=? AND v.locale=?`, key, locale).Scan(&v.ID, &v.Item, &v.Key, &v.Kind, &v.Source, &v.Locale, &v.Slug, &v.Draft, &v.Published, &v.First, &v.Number)
	return v, e
}
func retry(ctx context.Context, q query, actor, op, key, request string) (Result, bool, error) {
	var r Result
	if key == "" {
		return r, false, nil
	}
	if !retryPattern.MatchString(key) {
		return r, false, ErrInvalid
	}
	var prior string
	var status int
	e := q.QueryRowContext(ctx, `SELECT request_hash,result_id,result_version,http_status FROM idempotency_records WHERE actor_id=? AND operation=? AND key_hash=? AND expires_at>?`, actor, op, hash(key), db.UTC(time.Now())).Scan(&prior, &r.RevisionID, &r.Version, &status)
	if errors.Is(e, sql.ErrNoRows) {
		return r, false, nil
	}
	if e != nil {
		return r, false, e
	}
	if prior != request {
		return r, false, ErrIdempotency
	}
	r.Created = status == 201
	return r, true, nil
}
func remember(ctx context.Context, tx *sql.Tx, actor, op, key, request string, r Result) error {
	if key == "" {
		return nil
	}
	status := 200
	if r.Created {
		status = 201
	}
	_, e := tx.ExecContext(ctx, `INSERT INTO idempotency_records(actor_id,operation,key_hash,request_hash,http_status,result_id,result_version,expires_at) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(actor_id,operation,key_hash) DO UPDATE SET request_hash=excluded.request_hash,http_status=excluded.http_status,result_id=excluded.result_id,result_version=excluded.result_version,created_at=excluded.created_at,expires_at=excluded.expires_at`, actor, op, hash(key), request, status, r.RevisionID, r.Version, db.UTC(time.Now().Add(7*24*time.Hour)))
	return e
}
func refs(in DraftInput) ([]string, error) {
	if !slugPattern.MatchString(in.Slug) {
		return nil, typeIssue("slug", "content_slug")
	}
	if in.ExpectedVersion == nil || *in.ExpectedVersion < 0 || !localeOK(in.SourceLocale) || !slugPattern.MatchString(in.Slug) || !strOK(in.Title, 200, false) || !strOK(in.Summary, 500, true) || !utf8.ValidString(in.Markdown) || len(in.Markdown) > MaxBody || strings.ContainsRune(in.Markdown, 0) || len(in.TagKeys) > 8 || len(in.Presentations) > 100 {
		return nil, ErrInvalid
	}
	if in.Kind != "article" && in.Kind != "lesson" && in.Kind != "doc" {
		return nil, ErrInvalid
	}
	if in.CategoryKey != "" && !keyPattern.MatchString(in.CategoryKey) {
		return nil, ErrInvalid
	}
	seenTags := map[string]bool{}
	for _, k := range in.TagKeys {
		if !keyPattern.MatchString(k) || seenTags[k] {
			return nil, ErrInvalid
		}
		seenTags[k] = true
	}
	found := map[string]bool{}
	source := []byte(in.Markdown)
	root := goldmark.DefaultParser().Parse(text.NewReader(source))
	e := ast.Walk(root, func(n ast.Node, enter bool) (ast.WalkStatus, error) {
		if !enter {
			return ast.WalkContinue, nil
		}
		var dest []byte
		img := false
		switch n := n.(type) {
		case *ast.Image:
			dest = n.Destination
			img = true
		case *ast.Link:
			dest = n.Destination
		case *ast.RawHTML, *ast.HTMLBlock:
			return ast.WalkStop, ErrInvalid
		}
		if dest != nil {
			if strings.Contains(string(dest), "\\") || strings.HasPrefix(string(dest), "//") {
				return ast.WalkStop, ErrInvalid
			}
			u, e := url.Parse(string(dest))
			if e != nil || u.Scheme == "" && (u.Host != "" || strings.HasPrefix(u.Path, "//") || strings.Contains(u.Path, "\\")) {
				return ast.WalkStop, ErrInvalid
			}
			if strings.HasPrefix(u.Path, "/media/") && u.Scheme == "" && u.Host == "" {
				id := strings.TrimPrefix(u.Path, "/media/")
				if !media.ID.MatchString(id) || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" {
					return ast.WalkStop, ErrInvalid
				}
				found[id] = true
			} else if img || u.Scheme != "" && u.Scheme != "https" && u.Scheme != "http" && u.Scheme != "mailto" || u.Scheme == "" && !strings.HasPrefix(string(dest), "#") && !strings.HasPrefix(string(dest), "/") {
				return ast.WalkStop, ErrInvalid
			}
		}
		return ast.WalkContinue, nil
	})
	if e != nil {
		return nil, e
	}
	if in.CoverMediaID != "" {
		if !media.ID.MatchString(in.CoverMediaID) {
			return nil, ErrInvalid
		}
		found[in.CoverMediaID] = true
	}
	seen := map[string]bool{}
	for _, p := range in.Presentations {
		if !found[p.MediaID] || seen[p.MediaID] || !strOK(p.Caption, 2000, true) || !strOK(p.Transcript, 20000, true) {
			return nil, ErrInvalid
		}
		seen[p.MediaID] = true
		for _, id := range []string{p.PosterID, p.TrackID} {
			if id != "" {
				if !media.ID.MatchString(id) {
					return nil, ErrInvalid
				}
				found[id] = true
			}
		}
	}
	if len(found) > 256 {
		return nil, ErrInvalid
	}
	ids := []string{}
	for id := range found {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, nil
}
func (s *Service) Save(ctx context.Context, owner, key, locale, retryKey string, in DraftInput) (Result, error) {
	return s.save(ctx, owner, key, locale, retryKey, "draft", in)
}
func (s *Service) save(ctx context.Context, owner, key, locale, retryKey, op string, in DraftInput) (Result, error) {
	var out Result
	if !keyPattern.MatchString(key) || !localeOK(locale) {
		return out, ErrInvalid
	}
	ids, e := refs(in)
	if e != nil {
		return out, e
	}
	request := hash(struct {
		Key, Locale string
		Input       DraftInput
	}{key, locale, in})
	e = s.Files.Exclusive(ctx, func() error {
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
		converted := false
		var item, kind, source string
		e = tx.QueryRowContext(ctx, "SELECT id,kind,source_locale FROM content_items WHERE key=?", key).Scan(&item, &kind, &source)
		fresh := errors.Is(e, sql.ErrNoRows)
		if e != nil && !fresh {
			return e
		}
		if fresh {
			item, e = db.NewID()
			if e != nil {
				return e
			}
			if locale != in.SourceLocale {
				return ErrInvalid
			}
		} else if source != in.SourceLocale {
			return ErrInvalid
		} else if kind != in.Kind {
			if op != "draft" {
				return typeIssue("kind", "conversion_owner")
			}
			if _, e = s.changeTypeIn(ctx, tx, owner, key, locale, TypeChange{ExpectedVersion: in.ExpectedVersion, FromKind: kind, Kind: in.Kind, CollectionKey: in.CollectionKey, ExpectedCollectionVersion: in.ExpectedCollectionVersion, Position: in.Position}, true); e != nil {
				return e
			}
			converted = true
		}

		file, revision, e := s.Files.WriteLocked(item, locale, []byte(in.Markdown))
		if e != nil {
			return e
		}
		if fresh {
			if _, e = tx.ExecContext(ctx, "INSERT INTO content_items(id,key,kind,source_locale) VALUES(?,?,?,?)", item, key, in.Kind, in.SourceLocale); e != nil {
				return e
			}
		}
		v, e := loadVersion(ctx, tx, key, locale)
		create := errors.Is(e, sql.ErrNoRows)
		if e != nil && !create {
			return e
		}
		if op == "draft" && (create || v.First == "") {
			base := []rune(in.Slug)
			for n := 1; ; n++ {
				candidate := in.Slug
				if n > 1 {
					suffix := "-" + strconv.Itoa(n)
					keep := len(base)
					if keep+len(suffix) > 160 {
						keep = 160 - len(suffix)
					}
					candidate = string(base[:keep]) + suffix
				}
				var occupied int
				if e = tx.QueryRowContext(ctx, "SELECT count(*) FROM content_versions WHERE locale=? AND slug=? AND id<>?", locale, candidate, v.ID).Scan(&occupied); e != nil {
					return e
				}
				if occupied == 0 {
					in.Slug = candidate
					break
				}
			}
		}
		versionSlug := in.Slug
		if op == "import-stage" {
			versionSlug = "import-" + revision
		}
		if create {
			if *in.ExpectedVersion != 0 {
				return db.ErrVersionConflict
			}
			v.ID, e = db.NewID()
			if e != nil {
				return e
			}
			_, e = tx.ExecContext(ctx, "INSERT INTO content_versions(id,item_id,locale,slug,draft_revision_id) VALUES(?,?,?,?,?)", v.ID, item, locale, versionSlug, revision)
		} else {
			if v.Number != *in.ExpectedVersion {
				return db.ErrVersionConflict
			}
			if v.First != "" && v.Slug != in.Slug {
				return ErrInvalid
			}
			_, e = tx.ExecContext(ctx, "UPDATE content_versions SET slug=?,draft_revision_id=?,version=version+1 WHERE id=?", versionSlug, revision, v.ID)
		}
		if e != nil {
			return constraint(e)
		}
		if in.SourceLocale != locale {
			var sourceItem, sourceLocale string
			e = tx.QueryRowContext(ctx, "SELECT v.item_id,v.locale FROM revisions r JOIN content_versions v ON v.id=r.version_id WHERE r.id=?", in.SourceRevisionID).Scan(&sourceItem, &sourceLocale)
			if e != nil || sourceItem != item || sourceLocale != in.SourceLocale {
				return ErrInvalid
			}
		} else if in.SourceRevisionID != "" {
			return ErrInvalid
		}
		category := ""
		if in.CategoryKey != "" {
			if e = tx.QueryRowContext(ctx, "SELECT id FROM categories WHERE key=?", in.CategoryKey).Scan(&category); e != nil {
				return ErrInvalid
			}
		}
		if !converted && (in.CollectionKey != "" || in.Position != 0) {
			if s.Membership == nil {
				return ErrInvalid
			}
			if e = s.Membership(ctx, tx, item, in.Kind, in.CollectionKey, in.Position); e != nil {
				return e
			}
		}
		formats := map[string]media.Record{}
		for _, id := range ids {
			m, e := media.Get(ctx, tx, id)
			if e != nil {
				return ErrInvalid
			}
			formats[id] = m
		}
		if in.CoverMediaID != "" && formats[in.CoverMediaID].Kind != "image" {
			return ErrInvalid
		}
		for _, p := range in.Presentations {
			if formats[p.MediaID].Kind != "video" || p.PosterID != "" && formats[p.PosterID].Kind != "image" || p.TrackID != "" && formats[p.TrackID].Kind != "captions" {
				return ErrInvalid
			}
		}
		created := time.Now()
		// Preserve revision order through fast imports and later edits, even if
		// multiple writes share a millisecond or the wall clock moves backwards.
		var previous string
		if e = tx.QueryRowContext(ctx, "SELECT coalesce(max(created_at),'') FROM revisions WHERE version_id=?", v.ID).Scan(&previous); e != nil {
			return e
		}
		if previous != "" {
			prior, err := time.Parse(db.TimestampLayout, previous)
			if err != nil {
				return err
			}
			if !created.Truncate(time.Millisecond).After(prior) {
				created = prior.Add(time.Millisecond)
			}
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO revisions(id,version_id,title,summary,markdown_path,sha256,byte_size,created_by,source_revision_id,category_id,cover_media_id,slug,reading_minutes,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, revision, v.ID, in.Title, in.Summary, file.Key, file.SHA256, file.Size, owner, nullable(in.SourceRevisionID), nullable(category), nullable(in.CoverMediaID), in.Slug, readingMinutes(in.Markdown), db.UTC(created))
		if e != nil {
			return e
		}
		if e = learning.Bind(ctx, tx, item, revision, locale, in.SourceLocale, in.SourceRevisionID, in.Kind, in.Quiz); e != nil {
			return e
		}
		for _, id := range ids {
			if _, e = tx.ExecContext(ctx, "INSERT INTO revision_media VALUES(?,?)", revision, id); e != nil {
				return e
			}
		}
		for _, tag := range in.TagKeys {
			var id string
			if e = tx.QueryRowContext(ctx, "SELECT id FROM tags WHERE key=?", tag).Scan(&id); e != nil {
				return ErrInvalid
			}
			if _, e = tx.ExecContext(ctx, "INSERT INTO revision_tags VALUES(?,?)", revision, id); e != nil {
				return e
			}
		}
		for _, p := range in.Presentations {
			if _, e = tx.ExecContext(ctx, "INSERT INTO revision_media_presentations VALUES(?,?,?,?,?,?)", revision, p.MediaID, nullable(p.PosterID), nullable(p.TrackID), p.Caption, p.Transcript); e != nil {
				return e
			}
		}
		if _, e = tx.ExecContext(ctx, "UPDATE content_items SET updated_at=? WHERE id=?", db.UTC(time.Now()), item); e != nil {
			return e
		}
		out = Result{revision, *in.ExpectedVersion + 1, create}
		if e = access.CheckMutation(ctx, tx, revision); e != nil {
			return e
		}
		if e = remember(ctx, tx, access.Actor(ctx, owner), op, retryKey, request, out); e != nil {
			return e
		}
		return tx.Commit()
	})
	return out, e
}

// Constraint details remain internal and never expose SQL or filesystem paths.
func constraint(e error) error {
	if e != nil && strings.Contains(e.Error(), "constraint") {
		return ErrInvalid
	}
	return e
}
