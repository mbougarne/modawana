package portability

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/mbougarne/modawana/backend/internal/bundle"
	"github.com/mbougarne/modawana/backend/internal/content"
	"github.com/mbougarne/modawana/backend/internal/media"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

const notionMaxBytes = 128 << 20

var notionID = regexp.MustCompile(`(?i) ([0-9a-f]{32})$`)
var notionNumber = regexp.MustCompile(`^(\d+)([a-zA-Z]*)`)

type NotionOptions struct {
	Locale string
	Kind   string
}

type NotionPage struct {
	Path     string `json:"path"`
	Title    string `json:"title"`
	Key      string `json:"key"`
	Slug     string `json:"slug"`
	Kind     string `json:"kind"`
	Existing bool   `json:"existing"`
	Markdown string `json:"-"`
	URL      string `json:"-"`
}

type NotionIssue struct {
	Path string `json:"path"`
	Code string `json:"code"`
}

type NotionPreview struct {
	Pages           []NotionPage  `json:"pages"`
	Issues          []NotionIssue `json:"issues"`
	Media           int           `json:"media"`
	Ignored         int           `json:"ignored"`
	CollectionTitle string        `json:"collection_title"`
	CollectionKey   string        `json:"collection_key"`
	CollectionSlug  string        `json:"-"`
	Skipped         int           `json:"skipped"`
}

type notionArchive struct {
	zip    *zip.ReadCloser
	files  map[string]*zip.File
	pages  map[string]*NotionPage
	assets map[string]bool
	view   NotionPreview
}

func notionPath(name string) bool {
	return name != "" && len(name) <= 2000 && utf8.ValidString(name) && !strings.ContainsAny(name, "\\\x00\r\n") && !strings.HasPrefix(name, "/") && !strings.HasPrefix(name, "../") && name != ".." && path.Clean(name) == name && !(len(name) > 1 && name[1] == ':')
}

func notionIdentity(name string) string {
	base := strings.TrimSuffix(path.Base(name), path.Ext(name))
	if id := notionID.FindStringSubmatch(base); id != nil {
		return strings.ToLower(id[1])
	}
	hash := sha256.Sum256([]byte(name))
	return hex.EncodeToString(hash[:16])
}

func notionSlug(title, id string) string {
	var out []rune
	for _, r := range strings.ToLower(title) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			out = append(out, r)
		} else if len(out) > 0 && out[len(out)-1] != '-' {
			out = append(out, '-')
		}
		if len(out) >= 110 {
			break
		}
	}
	base := strings.Trim(string(out), "-")
	if base == "" {
		base = "page"
	}
	return base + "-" + id
}

func readNotionFile(f *zip.File, max int64) ([]byte, error) {
	if f.UncompressedSize64 > uint64(max) {
		return nil, bundle.ErrInvalid
	}
	r, e := f.Open()
	if e != nil {
		return nil, bundle.ErrInvalid
	}
	defer r.Close()
	b, e := io.ReadAll(io.LimitReader(r, max+1))
	if e != nil || int64(len(b)) > max || uint64(len(b)) != f.UncompressedSize64 {
		return nil, bundle.ErrInvalid
	}
	return b, nil
}

func openNotion(source string, options NotionOptions) (*notionArchive, error) {
	if (options.Locale != "en" && options.Locale != "ar" && options.Locale != "fr") || (options.Kind != "article" && options.Kind != "learning") {
		return nil, bundle.ErrInvalid
	}
	z, e := zip.OpenReader(source)
	if e != nil {
		return nil, bundle.ErrInvalid
	}
	a := &notionArchive{zip: z, files: map[string]*zip.File{}, pages: map[string]*NotionPage{}, assets: map[string]bool{}, view: NotionPreview{Pages: []NotionPage{}, Issues: []NotionIssue{}}}
	good := false
	defer func() {
		if !good {
			z.Close()
		}
	}()
	if len(z.File) > bundle.MaxFiles {
		return nil, bundle.ErrInvalid
	}
	var total uint64
	seen := map[string]bool{}
	keys := map[string]bool{}
	for _, f := range z.File {
		name := strings.TrimSuffix(f.Name, "/")
		if !notionPath(name) || seen[name] || (!f.Mode().IsRegular() && !f.FileInfo().IsDir()) || f.UncompressedSize64 > 50<<20 {
			return nil, bundle.ErrInvalid
		}
		seen[name] = true
		total += f.UncompressedSize64
		if total > notionMaxBytes {
			return nil, bundle.ErrInvalid
		}
		if f.FileInfo().IsDir() {
			continue
		}
		a.files[name] = f
		if strings.HasPrefix(name, "__MACOSX/") || path.Base(name) == ".DS_Store" || strings.ToLower(path.Ext(name)) != ".md" {
			a.view.Ignored++
			continue
		}
		body, e := readNotionFile(f, content.MaxBody)
		if e != nil || !utf8.Valid(body) || strings.ContainsRune(string(body), 0) {
			return nil, bundle.ErrInvalid
		}
		id := notionIdentity(name)
		key := "notion-" + id + "-" + options.Locale
		if keys[key] {
			return nil, bundle.ErrInvalid
		}
		keys[key] = true
		title := notionID.ReplaceAllString(strings.TrimSuffix(path.Base(name), path.Ext(name)), "")
		markdown := strings.TrimPrefix(string(body), "\ufeff")
		root := goldmark.DefaultParser().Parse(text.NewReader([]byte(markdown)))
		if heading, ok := root.FirstChild().(*ast.Heading); ok && heading.Level == 1 {
			title = string(heading.Text([]byte(markdown)))
			if first, rest, ok := strings.Cut(markdown, "\n"); ok && strings.HasPrefix(first, "# ") {
				markdown = strings.TrimLeft(rest, "\r\n")
			}
		}
		if strings.TrimSpace(title) == "" || utf8.RuneCountInString(title) > 200 {
			return nil, bundle.ErrInvalid
		}
		p := NotionPage{Path: name, Title: title, Key: key, Slug: notionSlug(title, id), Kind: "article", Markdown: markdown}
		a.view.Pages = append(a.view.Pages, p)
	}
	if len(a.view.Pages) == 0 || len(a.view.Pages) > 2000 || options.Kind == "learning" && len(a.view.Pages) > 500 {
		return nil, bundle.ErrInvalid
	}
	// Sort numbered siblings naturally: 20, 20A, 20B, 21. ZIP order is not lesson order.
	sort.Slice(a.view.Pages, func(i, j int) bool {
		left, right := a.view.Pages[i], a.view.Pages[j]
		if path.Dir(left.Path) != path.Dir(right.Path) {
			if path.Dir(left.Path) == "." {
				return true
			}
			if path.Dir(right.Path) == "." {
				return false
			}
			return left.Path < right.Path
		}
		l, r := notionNumber.FindStringSubmatch(path.Base(left.Path)), notionNumber.FindStringSubmatch(path.Base(right.Path))
		if l != nil && r != nil {
			ln, le := strconv.ParseUint(l[1], 10, 64)
			rn, re := strconv.ParseUint(r[1], 10, 64)
			if le == nil && re == nil && ln != rn {
				return ln < rn
			}
			if l[2] != r[2] {
				return l[2] < r[2]
			}
		}
		return left.Path < right.Path
	})
	if options.Kind == "learning" {
		root := a.view.Pages[0]
		a.view.CollectionKey = "notion-path-" + notionIdentity(root.Path) + "-" + options.Locale
		a.view.CollectionTitle = root.Title
		a.view.CollectionSlug = notionSlug(root.Title, notionIdentity(root.Path))
		for i := range a.view.Pages {
			// A single top-level page is the course overview; other pages become lessons.
			if len(a.view.Pages) > 1 && i == 0 && path.Dir(root.Path) == "." && path.Dir(a.view.Pages[1].Path) != "." {
				continue
			}
			a.view.Pages[i].Kind = "lesson"
		}
	}
	for i := range a.view.Pages {
		p := &a.view.Pages[i]
		p.URL = "/" + options.Locale + "/articles/" + url.PathEscape(p.Slug)
		if p.Kind == "lesson" {
			p.URL = "/" + options.Locale + "/learn/" + url.PathEscape(a.view.CollectionSlug) + "/" + url.PathEscape(p.Slug)
		}
		a.pages[p.Path] = p
	}
	good = true
	return a, nil
}

func (s *Service) notionPreview(ctx context.Context, source string, options NotionOptions) (*notionArchive, error) {
	a, e := openNotion(source, options)
	if e != nil {
		return nil, e
	}
	for i := range a.view.Pages {
		p := &a.view.Pages[i]
		var kind, slug, collection string
		e = s.DB.QueryRowContext(ctx, `SELECT i.kind,v.slug,coalesce(l.slug,'') FROM content_items i JOIN content_versions v ON v.item_id=i.id LEFT JOIN collection_items m ON m.item_id=i.id LEFT JOIN collection_locales l ON l.collection_id=m.collection_id AND l.locale=v.locale WHERE i.key=? AND v.locale=?`, p.Key, options.Locale).Scan(&kind, &slug, &collection)
		if e == nil {
			p.Existing = true
			a.view.Skipped++
			p.URL = "/" + options.Locale + "/articles/" + url.PathEscape(slug)
			if kind == "lesson" {
				p.URL = "/" + options.Locale + "/learn/" + url.PathEscape(collection) + "/" + url.PathEscape(slug)
			} else if kind != "article" {
				a.view.Issues = append(a.view.Issues, NotionIssue{p.Path, "existing_kind"})
			}
		} else if !errors.Is(e, sql.ErrNoRows) {
			a.zip.Close()
			return nil, e
		}
	}
	for i := range a.view.Pages {
		p := &a.view.Pages[i]
		if p.Existing {
			continue
		}
		_, e = a.rewrite(p, nil)
		if e != nil {
			a.view.Issues = append(a.view.Issues, NotionIssue{p.Path, "unsupported_content"})
		}
	}
	a.view.Media = len(a.assets)
	a.view.Ignored -= len(a.assets)
	for name := range a.assets {
		body, e := readNotionFile(a.files[name], 50<<20)
		if e != nil {
			a.zip.Close()
			return nil, e
		}
		declared := http.DetectContentType(body)
		if strings.EqualFold(path.Ext(name), ".vtt") {
			declared = "text/vtt"
		}
		if _, e = media.Validate(body, declared, "content"); e != nil {
			a.view.Issues = append(a.view.Issues, NotionIssue{name, "unsupported_media"})
		}
	}
	sort.Slice(a.view.Issues, func(i, j int) bool { return a.view.Issues[i].Path < a.view.Issues[j].Path })
	return a, nil
}

func (s *Service) PreviewNotion(ctx context.Context, source string, options NotionOptions) (NotionPreview, error) {
	a, e := s.notionPreview(ctx, source, options)
	if e != nil {
		return NotionPreview{}, e
	}
	defer a.zip.Close()
	return a.view, nil
}
