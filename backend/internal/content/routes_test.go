package content

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mbougarne/modawana/backend/internal/access"
)

func TestPublicPageCanonicalMetadataAndCorruptBody(t *testing.T) {
	s, o := fixture(t)
	ctx := context.Background()
	in := input()
	in.Slug = "مقدمة"
	in.Title = `Title <script> & "quoted"`
	in.Markdown = strings.Repeat("word ", 401)
	r, e := s.Save(ctx, o, "article", "en", "", in)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ResolvePage(ctx, "/en/articles/مقدمة"); !errors.Is(e, sql.ErrNoRows) {
		t.Fatal("draft exposed", e)
	}
	if _, e = s.ChangePublication(ctx, o, "article", "en", "", "publish", Action{ExpectedVersion: number(r.Version), RevisionID: r.RevisionID}); e != nil {
		t.Fatal(e)
	}
	p, e := s.ResolvePage(ctx, "/en/articles/مقدمة")
	if e != nil {
		t.Fatal(e)
	}
	if p.URL != "/en/articles/"+url.PathEscape(in.Slug) || p.Content == nil || p.Content.ReadingMinutes != 3 || len(p.Alternatives) != 1 || p.Alternatives[0].URL != p.URL {
		t.Fatal(p)
	}
	for _, bad := range []string{"/fr/articles/مقدمة", "/en/doc/مقدمة", "/en/articles/مقدمة/", "/en/learn/مقدمة", "/en/articles/nope"} {
		if _, e = s.ResolvePage(ctx, bad); !errors.Is(e, sql.ErrNoRows) {
			t.Fatalf("%s: %v", bad, e)
		}
	}
	list, e := s.Discover(ctx, Filter{Locale: "en"}, access.Principal{})
	if e != nil || len(list.Items) != 1 || list.Items[0].URL != p.URL || list.Items[0].ReadingMinutes != 3 {
		t.Fatal(list, e)
	}
	// A new private revision cannot alter the public reader or discovery estimate.
	in.ExpectedVersion = number(2)
	in.Markdown = "PRIVATE DRAFT"
	in.Title = "PRIVATE TITLE"
	if _, e = s.Save(ctx, o, "article", "en", "", in); e != nil {
		t.Fatal(e)
	}
	p, e = s.ResolvePage(ctx, "/en/articles/مقدمة")
	if e != nil || strings.Contains(p.Content.Markdown, "PRIVATE") {
		t.Fatal(p, e)
	}
	translation := input()
	translation.Slug = "introduction"
	translation.SourceRevisionID = r.RevisionID
	translation.Title = "Introduction"
	translation.Markdown = "Version française"
	translated, e := s.Save(ctx, o, "article", "fr", "", translation)
	if e != nil {
		t.Fatal(e)
	}
	reviewed, e := s.ChangePublication(ctx, o, "article", "fr", "", "review", Action{ExpectedVersion: number(translated.Version), RevisionID: translated.RevisionID, SourceRevisionID: r.RevisionID})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ChangePublication(ctx, o, "article", "fr", "", "publish", Action{ExpectedVersion: number(reviewed.Version), RevisionID: translated.RevisionID}); e != nil {
		t.Fatal(e)
	}
	french, e := s.ResolvePage(ctx, "/fr/articles/introduction")
	if e != nil || len(french.Alternatives) != 2 || french.Content.Markdown != translation.Markdown {
		t.Fatal(french, e)
	}
	var file string
	if e = s.DB.QueryRow("SELECT markdown_path FROM revisions WHERE id=?", r.RevisionID).Scan(&file); e != nil {
		t.Fatal(e)
	}
	// Obtain the data root from the database filename without exposing it publicly.
	var seq int
	var name, dbFile string
	if e = s.DB.QueryRow("PRAGMA database_list").Scan(&seq, &name, &dbFile); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(filepath.Dir(dbFile), file), []byte("corrupt"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ResolvePage(ctx, "/en/articles/مقدمة"); e == nil || errors.Is(e, sql.ErrNoRows) {
		t.Fatal("corruption became success or missing content", e)
	}
}

func TestPublicCollectionPageAndLockedRoute(t *testing.T) {
	s, o := fixture(t)
	ctx := context.Background()
	s.Membership = s.AttachMember
	s.PublicationHook = s.CheckPublicationPlan
	s.Access.Gates = s.LessonAccess
	_, e := s.SaveCollection(ctx, "path", "en", CollectionInput{ExpectedVersion: number(0), Kind: "learning", Slug: "path", Title: "Path", Visible: true})
	if e != nil {
		t.Fatal(e)
	}
	var first Result
	for i, key := range []string{"first", "soon", "private"} {
		in := input()
		in.Kind = "lesson"
		in.Slug = key
		in.Title = key
		in.Markdown = "SECRET BODY"
		in.CollectionKey = "path"
		in.Position = i + 1
		r, e := s.Save(ctx, o, key, "en", "", in)
		if e != nil {
			t.Fatal(e)
		}
		if i == 0 {
			first = r
		}
	}
	state, _ := s.Collection(ctx, "path")
	plan, e := s.SavePlan(ctx, o, "path", PlanInput{ExpectedVersion: number(state.Version), Members: []PlanMember{{Key: "first"}, {Key: "soon", Announcements: map[string]string{"en": "Coming topic"}}, {Key: "private"}}})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ApplyPlan(ctx, "path", ApplyPlanInput{ExpectedVersion: number(plan.Version), PlanID: plan.PlanID}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ChangePublication(ctx, o, "first", "en", "", "publish", Action{ExpectedVersion: number(first.Version), RevisionID: first.RevisionID}); e != nil {
		t.Fatal(e)
	}
	index, e := s.PublicCollections(ctx, "en", "learning", "", 20)
	if e != nil || len(index.Items) != 1 || index.Items[0].PublishedCount != 1 || index.Items[0].AnnouncedCount != 1 {
		t.Fatal(index, e)
	}
	page, e := s.ResolvePage(ctx, "/en/learn/path/first")
	if e != nil || page.Collection == nil || page.Content == nil || page.Collection.Items[0].URL != page.URL {
		t.Fatal(page, e)
	}
	for _, path := range []string{"/en/articles/first", "/en/learn/path/soon", "/en/learn/path/private", "/fr/learn/path", "/en/docs/path", "/en/learn/other/first"} {
		if _, e = s.ResolvePage(ctx, path); !errors.Is(e, sql.ErrNoRows) {
			t.Fatal(path, e)
		}
	}
	// Gate-aware plan state remains server-authoritative, even before quiz UI ships.
	s.Access.Gates = func(context.Context, *sql.Tx, string, access.Principal) (bool, error) { return false, nil }
	page, e = s.ResolvePage(ctx, "/en/learn/path/first")
	if !errors.Is(e, ErrLocked) || page.Content == nil || !page.Content.Locked {
		t.Fatal(page, e)
	}
	raw, _ := json.Marshal(page)
	if strings.Contains(string(raw), "SECRET BODY") || strings.Contains(string(raw), "markdown") || strings.Contains(string(raw), "private") {
		t.Fatal("locked/private data leak", string(raw))
	}
	index, e = s.PublicCollections(ctx, "fr", "learning", "", 20)
	if e != nil || len(index.Items) != 0 {
		t.Fatal(index, e)
	}
}
