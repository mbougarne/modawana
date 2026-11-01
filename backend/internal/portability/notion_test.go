package portability

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"errors"
	"image"
	"image/png"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mbougarne/modawana/backend/internal/bundle"
	"github.com/mbougarne/modawana/backend/internal/content"
	"github.com/mbougarne/modawana/backend/internal/siteconfig"
)

func notionZIP(t *testing.T, entries map[string][]byte) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "notion.zip")
	f, e := os.Create(file)
	if e != nil {
		t.Fatal(e)
	}
	z := zip.NewWriter(f)
	for name, body := range entries {
		w, e := z.Create(name)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = w.Write(body); e != nil {
			t.Fatal(e)
		}
	}
	if e = z.Close(); e != nil {
		t.Fatal(e)
	}
	f.Close()
	return file
}

func TestNotionAdditiveArticlesMediaAndRetry(t *testing.T) {
	s, owner := fixture(t)
	populate(t, s, owner)
	ctx := context.Background()
	var picture bytes.Buffer
	png.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 4, 4)))
	other := "Child page 22222222222222222222222222222222.md"
	body := "# Parent\n\n[Child](" + url.PathEscape(other) + "#example)\n\n![Photo](images/photo.png)\n\n[Reference][child]\n\n[child]: <" + url.PathEscape(other) + ">\n\n`[Code](" + url.PathEscape(other) + ")`\n\n```md\n[Code](" + url.PathEscape(other) + ")\n```\n"
	archive := notionZIP(t, map[string][]byte{
		"Parent 11111111111111111111111111111111.md": []byte(body),
		other:              []byte("# Child\n\nUnchanged source."),
		"images/photo.png": picture.Bytes(),
		"database.csv":     []byte("Name,Value\nExample,1"),
	})
	options := NotionOptions{"en", "article"}
	preview, e := s.PreviewNotion(ctx, archive, options)
	if e != nil || len(preview.Pages) != 2 || len(preview.Issues) != 0 || preview.Media != 1 {
		t.Fatal(preview, e)
	}
	var before int
	s.DB.QueryRow("SELECT count(*) FROM content_items").Scan(&before)
	result, e := s.ImportNotion(ctx, owner, archive, options, nil)
	if e != nil || result.Revisions != 2 || result.Media != 1 || !result.ReviewRequired {
		t.Fatal(result, e)
	}
	cs := &content.Service{DB: s.DB, Files: s.Files}
	draft, e := cs.Draft(ctx, "notion-11111111111111111111111111111111-en", "en")
	if e != nil {
		t.Fatal(e)
	}
	markdown := draft.Revision.Markdown
	if !strings.Contains(markdown, "(/en/articles/child-22222222222222222222222222222222#example)") || !strings.Contains(markdown, "[child]: </en/articles/") || !strings.Contains(markdown, "![Photo](/media/") || !strings.Contains(markdown, "`[Code]("+url.PathEscape(other)+")`") || !strings.Contains(markdown, "```md\n[Code]("+url.PathEscape(other)+")\n```") {
		t.Fatal("links/media were not rewritten correctly or code was changed")
	}
	var published, after int
	s.DB.QueryRow("SELECT count(*) FROM content_versions WHERE published_revision_id IS NOT NULL AND item_id IN (SELECT id FROM content_items WHERE key LIKE 'notion-%')").Scan(&published)
	s.DB.QueryRow("SELECT count(*) FROM content_items").Scan(&after)
	if published != 0 || after != before+2 {
		t.Fatal("import published content or replaced existing items")
	}
	configuration, e := (&siteconfig.Service{DB: s.DB, Files: s.Files}).Get(ctx, "site")
	if e != nil || !strings.Contains(string(configuration.Snapshot), "Second owner") {
		t.Fatal("existing configuration was not preserved", e)
	}
	result, e = s.ImportNotion(ctx, owner, archive, options, nil)
	if e != nil || result.Revisions != 0 || result.Skipped != 2 || result.Media != 0 {
		t.Fatal("retry created duplicates", result, e)
	}
}

func TestNotionLearningOrderAndNoLivePlan(t *testing.T) {
	s, owner := fixture(t)
	archive := notionZIP(t, map[string][]byte{
		"Course 11111111111111111111111111111111.md":           []byte("# Course\n\nOverview."),
		"Course/21 Last 22222222222222222222222222222222.md":   []byte("# Last\n\nEnd."),
		"Course/20B Types 33333333333333333333333333333333.md": []byte("# Types\n\nTypes."),
		"Course/20 Base 44444444444444444444444444444444.md":   []byte("# Base\n\nBase."),
		"Course/20A Setup 55555555555555555555555555555555.md": []byte("# Setup\n\nSetup."),
	})
	ctx := context.Background()
	preview, e := s.PreviewNotion(ctx, archive, NotionOptions{"en", "learning"})
	if e != nil || len(preview.Issues) != 0 || preview.Pages[0].Kind != "article" || preview.Pages[1].Title != "Base" || preview.Pages[2].Title != "Setup" || preview.Pages[3].Title != "Types" || preview.Pages[4].Title != "Last" {
		t.Fatal(preview, e)
	}
	result, e := s.ImportNotion(ctx, owner, archive, NotionOptions{"en", "learning"}, nil)
	if e != nil || result.Collections != 1 || result.Revisions != 5 {
		t.Fatal(result, e)
	}
	cs := &content.Service{DB: s.DB, Files: s.Files}
	collection, e := cs.Collection(ctx, preview.CollectionKey)
	if e != nil || len(collection.DraftMembers) != 4 || collection.ActivePlanID != "" || collection.Locales[0].Visible {
		t.Fatal("learning plan became live", collection, e)
	}
}

func TestNotionInvalidArchiveAndContentLeaveTargetUnchanged(t *testing.T) {
	for _, entries := range []map[string][]byte{
		{"../escape.md": []byte("# Nope")},
		{"C:/escape.md": []byte("# Nope")},
		{"A 11111111111111111111111111111111.md": []byte("# One"), "B 11111111111111111111111111111111.md": []byte("# Two")},
		{"page.md": []byte("# Test\n\n[Missing](missing.md)")},
		{"page.md": []byte("# Test\n\n![Remote](https://example.invalid/image.png)")},
		{"page.md": []byte("# Test\n\n<aside>Unsupported</aside>")},
		{"page.md": []byte("# Test\n\n![Corrupt](image.png)"), "image.png": []byte("not an image")},
	} {
		s, owner := fixture(t)
		archive := notionZIP(t, entries)
		if _, e := s.ImportNotion(context.Background(), owner, archive, NotionOptions{"en", "article"}, nil); e == nil {
			t.Fatal("accepted invalid archive")
		}
		var count int
		s.DB.QueryRow("SELECT (SELECT count(*) FROM content_items)+(SELECT count(*) FROM media)").Scan(&count)
		if count != 0 {
			t.Fatal("failed import changed live metadata")
		}
	}
}

func TestNotionRechecksAuthorizationAtCommit(t *testing.T) {
	s, owner := fixture(t)
	archive := notionZIP(t, map[string][]byte{"page.md": []byte("# Test\n\nA draft.")})
	denied := errors.New("owner session was revoked")
	_, e := s.ImportNotion(context.Background(), owner, archive, NotionOptions{"ar", "article"}, func(*sql.Tx) error { return denied })
	if !errors.Is(e, denied) {
		t.Fatal(e)
	}
	var count int
	s.DB.QueryRow("SELECT count(*) FROM content_items").Scan(&count)
	if count != 0 {
		t.Fatal("revoked session imported content")
	}
	if _, e = s.PreviewNotion(context.Background(), archive, NotionOptions{"de", "article"}); !errors.Is(e, bundle.ErrInvalid) {
		t.Fatal("accepted invalid locale", e)
	}
}

// Personal source stays outside Git. This opt-in check exercises the supplied ZIP
// without shipping its pages as fixtures or seeding the running development site.
func TestNotionSuppliedArchive(t *testing.T) {
	archive := os.Getenv("BLOG_NOTION_TEST_ARCHIVE")
	if archive == "" {
		t.Skip("optional personal archive not supplied")
	}
	s, owner := fixture(t)
	ctx := context.Background()
	options := NotionOptions{"en", "learning"}
	preview, e := s.PreviewNotion(ctx, archive, options)
	if e != nil || len(preview.Pages) != 29 || len(preview.Issues) != 0 || preview.Media != 0 {
		t.Fatal("supplied archive preview failed", preview, e)
	}
	result, e := s.ImportNotion(ctx, owner, archive, options, nil)
	if e != nil || result.Revisions != 29 || result.Collections != 1 {
		t.Fatal(result, e)
	}
	collection, e := (&content.Service{DB: s.DB, Files: s.Files}).Collection(ctx, preview.CollectionKey)
	if e != nil || len(collection.DraftMembers) != 28 || collection.ActivePlanID != "" {
		t.Fatal("wrong supplied path members", e)
	}
}

func TestNotionCalloutMarkdownKeepsCodeAndValidatesContents(t *testing.T) {
	s, owner := fixture(t)
	body := "# Lesson\n\n<aside>\n\nRemember **this**.\n\n</aside>\n\n```html\n<aside>\nexample\n</aside>\n```\n"
	archive := notionZIP(t, map[string][]byte{"Lesson.md": []byte(body)})
	preview, err := s.PreviewNotion(context.Background(), archive, NotionOptions{"en", "article"})
	if err != nil || len(preview.Issues) != 0 {
		t.Fatal(preview, err)
	}
	if _, err = s.ImportNotion(context.Background(), owner, archive, NotionOptions{"en", "article"}, nil); err != nil {
		t.Fatal(err)
	}
	cs := &content.Service{DB: s.DB, Files: s.Files}
	draft, err := cs.Draft(context.Background(), preview.Pages[0].Key, "en")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(draft.Revision.Markdown, "Remember **this**.") || !strings.Contains(draft.Revision.Markdown, "```html\n<aside>\nexample\n</aside>\n```") {
		t.Fatal("callout text or code changed")
	}
	for _, invalid := range []string{"<aside onclick=\"evil()\">\n\nText\n\n</aside>", "<aside>\n\n<script>alert(1)</script>\n\n</aside>", "<aside>\n\n![remote](https://example.test/image.png)\n\n</aside>"} {
		file := notionZIP(t, map[string][]byte{"Unsafe.md": []byte(invalid)})
		view, err := s.PreviewNotion(context.Background(), file, NotionOptions{"en", "article"})
		if err != nil || len(view.Issues) == 0 {
			t.Fatal("unsafe callout accepted", view, err)
		}
	}
}

// Optional operator-supplied archives are read only; imports use isolated test data.
func TestNotionLocalReviewArchives(t *testing.T) {
	paths := os.Getenv("BLOG_TEST_NOTION_ARCHIVES")
	if paths == "" {
		t.Skip("no local review archives supplied")
	}
	for _, path := range filepath.SplitList(paths) {
		s, owner := fixture(t)
		options := NotionOptions{"en", "learning"}
		preview, err := s.PreviewNotion(context.Background(), path, options)
		if err != nil || len(preview.Issues) > 0 {
			t.Fatalf("preview: %v, issues: %v", err, preview.Issues)
		}
		result, err := s.ImportNotion(context.Background(), owner, path, options, nil)
		if err != nil || result.Revisions != len(preview.Pages) {
			t.Fatalf("import %d pages: %+v %v", len(preview.Pages), result, err)
		}
		t.Logf("Verified %d imported draft pages", result.Revisions)
	}
}

func TestNotionUnquotedCPPTemplateText(t *testing.T) {
	source := "Implement assemble(std::span<const Statement>) -> std::vector<std::uint8_t>.\n\n`std::vector<int>`\n\n```cpp\nstd::vector<int> values;\n```"
	got := notionCallouts(source)
	if !strings.Contains(got, "std::span&lt;const Statement&gt;") || !strings.Contains(got, "std::vector<std::uint8_t>") || !strings.Contains(got, "`std::vector<int>`") || !strings.Contains(got, "std::vector<int> values;") {
		t.Fatal(got)
	}
}
