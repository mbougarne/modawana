package content

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"

	"github.com/mbougarne/modawana/backend/internal/access"
	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/storage"
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
	if _, e = d.Exec("INSERT INTO owner(id) VALUES(?)", owner); e != nil {
		t.Fatal(e)
	}
	return &Service{DB: d, Files: f, Access: &access.Live{DB: d}}, owner
}
func number(n int64) *int64 { return &n }
func input() DraftInput {
	return DraftInput{ExpectedVersion: number(0), Kind: "article", SourceLocale: "en", Slug: "first", Title: "First", Summary: "Summary", Markdown: "# Hello\n\nBody"}
}
func TestDraftConflictsRetriesAndRestore(t *testing.T) {
	s, o := fixture(t)
	ctx := context.Background()
	in := input()
	first, e := s.Save(ctx, o, "first", "en", "one", in)
	if e != nil {
		t.Fatal(e)
	}
	again, e := s.Save(ctx, o, "first", "en", "one", in)
	if e != nil || first != again {
		t.Fatal(again, e)
	}
	in.Title = "Changed"
	if _, e = s.Save(ctx, o, "first", "en", "one", in); !errors.Is(e, ErrIdempotency) {
		t.Fatal(e)
	}
	if _, e = s.Save(ctx, o, "first", "en", "", in); !errors.Is(e, db.ErrVersionConflict) {
		t.Fatal(e)
	}
	in.ExpectedVersion = number(1)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := s.Save(ctx, o, "first", "en", "", in); results <- e }()
	}
	wg.Wait()
	close(results)
	ok, conflict := 0, 0
	for e := range results {
		if e == nil {
			ok++
		} else if errors.Is(e, db.ErrVersionConflict) {
			conflict++
		} else {
			t.Fatal(e)
		}
	}
	if ok != 1 || conflict != 1 {
		t.Fatal(ok, conflict)
	}
	draft, e := s.Draft(ctx, "first", "en")
	if e != nil || draft.Version != 2 || draft.Revision.Title != "Changed" {
		t.Fatal(draft, e)
	}
	_, e = s.DB.Exec("UPDATE content_versions SET published_revision_id=?,first_published_at='2026-10-24T12:00:00.000Z',last_published_at='2026-10-24T12:00:00.000Z'", first.RevisionID)
	if e != nil {
		t.Fatal(e)
	}
	restored, e := s.Restore(ctx, o, "first", "en", "restore", Action{ExpectedVersion: number(2), RevisionID: first.RevisionID})
	if e != nil || restored.RevisionID == first.RevisionID || restored.Version != 3 {
		t.Fatal(restored, e)
	}
	again, e = s.Restore(ctx, o, "first", "en", "restore", Action{ExpectedVersion: number(2), RevisionID: first.RevisionID})
	if e != nil || again != restored {
		t.Fatal(again, e)
	}
	draft, e = s.Draft(ctx, "first", "en")
	if e != nil || draft.PublishedID != first.RevisionID || draft.Revision.Title != "First" {
		t.Fatal(draft, e)
	}
	history, _, e := s.History(ctx, "first", "en", "")
	if e != nil || len(history) != 3 {
		t.Fatal(history, e)
	}
	var bodies int
	s.DB.QueryRow("SELECT count(*) FROM pragma_table_info('idempotency_records') WHERE name IN ('markdown','body','request_body')").Scan(&bodies)
	if bodies != 0 {
		t.Fatal("body stored in retry data")
	}
}
func TestValidationAndExactTranslationSource(t *testing.T) {
	s, o := fixture(t)
	ctx := context.Background()
	in := input()
	r, e := s.Save(ctx, o, "first", "en", "", in)
	if e != nil {
		t.Fatal(e)
	}
	ar := input()
	ar.Slug = "الأول"
	ar.SourceRevisionID = r.RevisionID
	ar.Markdown = "# مرحباً"
	if _, e = s.Save(ctx, o, "first", "ar", "", ar); e != nil {
		t.Fatal(e)
	}
	for _, change := range []func(*DraftInput){func(i *DraftInput) { i.ExpectedVersion = nil }, func(i *DraftInput) { i.Kind = "other" }, func(i *DraftInput) { i.Markdown = "![x](../../private)" }, func(i *DraftInput) { i.Markdown = "[x](javascript:alert(1))" }, func(i *DraftInput) { i.Markdown = "<script>alert(1)</script>" }, func(i *DraftInput) { i.Slug = "../bad" }, func(i *DraftInput) { i.Markdown = "![x](/media/00000000000000000000000000000000)" }} {
		v := input()
		change(&v)
		if _, e = s.Save(ctx, o, "invalid", "en", "", v); e == nil {
			t.Fatal("accepted invalid input")
		}
	}
	in.ExpectedVersion = number(1)
	in.Kind = "doc"
	if _, e = s.Save(ctx, o, "first", "en", "", in); !errors.Is(e, ErrInvalid) {
		t.Fatal("kind changed", e)
	}
}
