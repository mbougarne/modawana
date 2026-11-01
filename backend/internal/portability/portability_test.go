package portability

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/mbougarne/modawana/backend/internal/bundle"
	"github.com/mbougarne/modawana/backend/internal/content"
	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/learning"
	"github.com/mbougarne/modawana/backend/internal/media"
	"github.com/mbougarne/modawana/backend/internal/siteconfig"
	"github.com/mbougarne/modawana/backend/internal/storage"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T) (*Service, string) {
	t.Helper()
	root := t.TempDir()
	os.Chmod(root, 0700)
	d, e := db.Open(context.Background(), root)
	if e != nil {
		t.Fatal(e)
	}
	f, e := storage.Open(root)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { f.Close(); d.Close() })
	owner, _ := db.NewID()
	if _, e = d.Exec("INSERT INTO owner(id,password_hash) VALUES(?,?)", owner, "PRIVATE-AUTH-MARKER"); e != nil {
		t.Fatal(e)
	}
	return &Service{d, f}, owner
}
func populate(t *testing.T, s *Service, owner string) string {
	t.Helper()
	ctx := context.Background()
	cs := &content.Service{DB: s.DB, Files: s.Files}
	cs.Membership = cs.AttachMember
	zero := int64(0)
	if _, e := cs.SaveCollection(ctx, "course", "en", content.CollectionInput{ExpectedVersion: &zero, Kind: "learning", Slug: "course", Title: "Course", Visible: true}); e != nil {
		t.Fatal(e)
	}
	var b bytes.Buffer
	png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 10, 10)))
	m, e := media.New(s.DB, s.Files, nil).Upload(ctx, owner, "image.png", "image/png", "content", &b)
	if e != nil {
		t.Fatal(e)
	}
	q := &learning.Definition{PassMark: 1, Questions: []learning.PrivateQuestion{{Question: learning.Question{ID: "one", Type: "single", Text: "Question", Options: []learning.Option{{ID: "a", Text: "A"}, {ID: "b", Text: "B"}}}, Correct: []string{"a"}, Explanation: "Private answer"}}}
	in := content.DraftInput{ExpectedVersion: &zero, Kind: "lesson", SourceLocale: "en", Slug: "lesson", Title: "Lesson", Markdown: "![Photo](/media/" + m.ID + ")", CollectionKey: "course", Position: 1, Quiz: q}
	rev, e := cs.Save(ctx, owner, "lesson", "en", "", in)
	if e != nil {
		t.Fatal(e)
	}
	sourceDraft, _ := cs.Draft(ctx, "lesson", "en")
	in.Quiz = sourceDraft.Revision.Quiz
	in.SourceRevisionID = rev.RevisionID
	in.Slug = "lecon"
	in.Title = "Leçon"
	if _, e = cs.Save(ctx, owner, "lesson", "fr", "", in); e != nil {
		t.Fatal(e)
	}
	c, _ := cs.Collection(ctx, "course")
	if _, e = cs.SavePlan(ctx, owner, "course", content.PlanInput{ExpectedVersion: &c.Version, Members: []content.PlanMember{{Key: "lesson"}}}); e != nil {
		t.Fatal(e)
	}
	d, _ := cs.Draft(ctx, "lesson", "en")
	if e = cs.StagePortableGates(ctx, owner, "course", map[string]string{"lesson": d.Revision.Quiz.AssessmentID}); e != nil {
		t.Fatal(e)
	}
	cfg := &siteconfig.Service{DB: s.DB, Files: s.Files}
	doc, _ := cfg.Get(ctx, "site")
	var site siteconfig.Site
	json.Unmarshal(doc.Snapshot, &site)
	site.Wordmark = "Second owner"
	raw, _ := json.Marshal(site)
	if _, e = cfg.Apply(ctx, owner, "site", siteconfig.Input{ExpectedVersion: &zero, Snapshot: raw}); e != nil {
		t.Fatal(e)
	}
	if _, _, e = learning.NewIdentity(s.DB, 180, 10).Create(ctx, "reader"); e != nil {
		t.Fatal(e)
	}
	return m.ID
}
func TestRoundTripDraftsRemapsAndExcludesSecrets(t *testing.T) {
	source, owner := fixture(t)
	oldMedia := populate(t, source, owner)
	archive := filepath.Join(t.TempDir(), "content.zip")
	f, _ := os.Create(archive)
	if e := source.Export(context.Background(), f); e != nil {
		t.Fatal(e)
	}
	f.Close()
	root, _, e := bundle.Read(archive, "content")
	if e != nil {
		t.Fatal(e)
	}
	defer os.RemoveAll(root)
	raw, _ := os.ReadFile(filepath.Join(root, "data.json"))
	if strings.Contains(string(raw), "PRIVATE-AUTH-MARKER") || strings.Contains(string(raw), "learning_identities") {
		t.Fatal("private data exported")
	}
	target, newOwner := fixture(t)
	result, e := target.Import(context.Background(), newOwner, archive, nil)
	if e != nil {
		t.Fatal(e)
	}
	if result.Revisions != 2 || !result.ReviewRequired {
		t.Fatal(result)
	}
	cs := &content.Service{DB: target.DB, Files: target.Files}
	d, e := cs.Draft(context.Background(), "lesson", "en")
	if e != nil {
		t.Fatal(e)
	}
	if d.PublishedID != "" || d.Revision.Quiz == nil || strings.Contains(d.Revision.Markdown, oldMedia) {
		t.Fatal("not a remapped quiz draft", d)
	}
	tr, e := cs.Draft(context.Background(), "lesson", "fr")
	if e != nil || tr.Revision.SourceID != d.Revision.ID || tr.Reviewed {
		t.Fatal("source linkage/review", tr, e)
	}
	for _, q := range []string{"SELECT count(*) FROM learning_identities", "SELECT count(*) FROM sessions", "SELECT count(*) FROM api_tokens", "SELECT count(*) FROM assessment_reviews", "SELECT count(*) FROM translation_reviews", "SELECT count(*) FROM collection_plan_state WHERE active_plan_id IS NOT NULL", "SELECT count(*) FROM config_documents WHERE applied_revision_id IS NOT NULL"} {
		var n int
		if e = target.DB.QueryRow(q).Scan(&n); e != nil || n != 0 {
			t.Fatal(q, n, e)
		}
	}
	var n int
	if e = target.DB.QueryRow("SELECT count(*) FROM collection_plan_gates").Scan(&n); e != nil || n != 1 {
		t.Fatal("draft gate lost", n, e)
	}
	if _, e = target.Import(context.Background(), newOwner, archive, nil); e == nil {
		t.Fatal("overwrote existing installation")
	}
}
func TestInvalidBundleLeavesTargetEmpty(t *testing.T) {
	source, owner := fixture(t)
	populate(t, source, owner)
	archive := filepath.Join(t.TempDir(), "original.zip")
	f, _ := os.Create(archive)
	if e := source.Export(context.Background(), f); e != nil {
		t.Fatal(e)
	}
	f.Close()
	root, m, e := bundle.Read(archive, "content")
	if e != nil {
		t.Fatal(e)
	}
	defer os.RemoveAll(root)
	raw, _ := os.ReadFile(filepath.Join(root, "data.json"))
	var data Data
	json.Unmarshal(raw, &data)
	data.Media = nil
	raw, _ = json.Marshal(data)
	os.WriteFile(filepath.Join(root, "data.json"), raw, 0600)
	paths := []string{}
	for _, v := range m.Files {
		paths = append(paths, v.Path)
	}
	bad := filepath.Join(t.TempDir(), "bad.zip")
	f, _ = os.Create(bad)
	if e = bundle.Write(f, "content", root, paths); e != nil {
		t.Fatal(e)
	}
	f.Close()
	target, newOwner := fixture(t)
	if _, e = target.Import(context.Background(), newOwner, bad, nil); e == nil {
		t.Fatal("missing media accepted")
	}
	var n int
	target.DB.QueryRow("SELECT count(*) FROM content_items").Scan(&n)
	if n != 0 {
		t.Fatal("partial import")
	}
}

func TestHistoricalSlugReuseRoundTrip(t *testing.T) {
	source, owner := fixture(t)
	cs := &content.Service{DB: source.DB, Files: source.Files}
	zero := int64(0)
	in := content.DraftInput{ExpectedVersion: &zero, Kind: "article", SourceLocale: "en", Slug: "reused", Title: "First", Markdown: "Body"}
	first, e := cs.Save(context.Background(), owner, "first", "en", "", in)
	if e != nil {
		t.Fatal(e)
	}
	in.ExpectedVersion = &first.Version
	in.Slug = "current"
	if _, e = cs.Save(context.Background(), owner, "first", "en", "", in); e != nil {
		t.Fatal(e)
	}
	in.ExpectedVersion = &zero
	in.Slug = "reused"
	in.Title = "Second"
	if _, e = cs.Save(context.Background(), owner, "second", "en", "", in); e != nil {
		t.Fatal(e)
	}
	var b bytes.Buffer
	if e = source.Export(context.Background(), &b); e != nil {
		t.Fatal(e)
	}
	archive := filepath.Join(t.TempDir(), "content.zip")
	if e = os.WriteFile(archive, b.Bytes(), 0600); e != nil {
		t.Fatal(e)
	}
	target, targetOwner := fixture(t)
	if _, e = target.Import(context.Background(), targetOwner, archive, nil); e != nil {
		t.Fatal(e)
	}
	var slug string
	if e = target.DB.QueryRow("SELECT v.slug FROM content_versions v JOIN content_items i ON i.id=v.item_id WHERE i.key='first'").Scan(&slug); e != nil || slug != "current" {
		t.Fatalf("current slug %s: %v", slug, e)
	}
	var n int
	target.DB.QueryRow("SELECT count(*) FROM revisions WHERE slug='reused'").Scan(&n)
	if n != 2 {
		t.Fatalf("historical slugs lost: %d", n)
	}
}
