package content

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/mbougarne/modawana/backend/internal/access"
	"github.com/mbougarne/modawana/backend/internal/db"
)

func bulkItem(t *testing.T, s *Service, key, locale, op string) BulkPublicationItem {
	t.Helper()
	d, err := s.Draft(context.Background(), key, locale)
	if err != nil {
		t.Fatal(err)
	}
	rev := d.Revision.ID
	if op == "unpublish" && d.PublishedID != "" {
		rev = d.PublishedID
	}
	return BulkPublicationItem{Key: key, Locale: locale, ExpectedVersion: number(d.Version), RevisionID: rev}
}
func saveBulkArticle(t *testing.T, s *Service, owner, key string) Result {
	t.Helper()
	in := input()
	in.Slug = key
	r, err := s.Save(context.Background(), owner, key, "en", "", in)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func TestBulkPublicationPreservesDraftsHistoryAndNoops(t *testing.T) {
	s, owner := fixture(t)
	ctx := context.Background()
	for _, key := range []string{"bulk-one", "bulk-two"} {
		saveBulkArticle(t, s, owner, key)
	}
	batch := BulkPublication{Operation: "publish", Items: []BulkPublicationItem{bulkItem(t, s, "bulk-one", "en", "publish"), bulkItem(t, s, "bulk-two", "en", "publish")}}
	out, err := s.ChangePublicationBulk(ctx, owner, batch)
	if err != nil || out.Changed != 2 {
		t.Fatal(out, err)
	}
	if _, err = s.ChangePublicationBulk(ctx, owner, batch); !errors.Is(err, db.ErrVersionConflict) {
		t.Fatal("stale replay", err)
	}
	noop := BulkPublication{Operation: "publish", Items: []BulkPublicationItem{bulkItem(t, s, "bulk-one", "en", "publish")}}
	if out, err = s.ChangePublicationBulk(ctx, owner, noop); err != nil || out.Unchanged != 1 || out.Changed != 0 {
		t.Fatal(out, err)
	}
	in := input()
	in.Slug = "bulk-one"
	in.ExpectedVersion = number(2)
	in.Markdown = "New saved draft, not yet published"
	edited, err := s.Save(ctx, owner, "bulk-one", "en", "", in)
	if err != nil {
		t.Fatal(err)
	}
	unpublish := BulkPublication{Operation: "unpublish", Items: []BulkPublicationItem{bulkItem(t, s, "bulk-one", "en", "unpublish"), bulkItem(t, s, "bulk-two", "en", "unpublish")}}
	if out, err = s.ChangePublicationBulk(ctx, owner, unpublish); err != nil || out.Changed != 2 {
		t.Fatal(out, err)
	}
	d, err := s.Draft(ctx, "bulk-one", "en")
	if err != nil || d.PublishedID != "" || d.Revision.ID != edited.RevisionID || d.Revision.Markdown != in.Markdown || !d.SlugLocked {
		t.Fatal(d, err)
	}
	history, _, err := s.History(ctx, "bulk-one", "en", "")
	if err != nil || len(history) != 2 {
		t.Fatal(history, err)
	}
	if _, err = s.Public(ctx, "en", "bulk-one", access.Principal{}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("unpublished still public", err)
	}
	unpublish.Items = []BulkPublicationItem{bulkItem(t, s, "bulk-one", "en", "unpublish")}
	if out, err = s.ChangePublicationBulk(ctx, owner, unpublish); err != nil || out.Unchanged != 1 {
		t.Fatal(out, err)
	}
}
func TestBulkPublicationRollsBackConflictReviewAndHookFailure(t *testing.T) {
	s, owner := fixture(t)
	ctx := context.Background()
	saveBulkArticle(t, s, owner, "bulk-first")
	saveBulkArticle(t, s, owner, "bulk-last")
	first := bulkItem(t, s, "bulk-first", "en", "publish")
	stale := bulkItem(t, s, "bulk-last", "en", "publish")
	stale.ExpectedVersion = number(99)
	checkPrivate := func() {
		t.Helper()
		d, err := s.Draft(ctx, "bulk-first", "en")
		if err != nil || d.Version != 1 || d.PublishedID != "" {
			t.Fatal("partial commit", d, err)
		}
	}
	if _, err := s.ChangePublicationBulk(ctx, owner, BulkPublication{Operation: "publish", Items: []BulkPublicationItem{first, stale}}); !errors.Is(err, db.ErrVersionConflict) {
		t.Fatal(err)
	}
	checkPrivate()
	source := saveBulkArticle(t, s, owner, "bulk-source")
	if _, err := s.ChangePublication(ctx, owner, "bulk-source", "en", "", "publish", Action{ExpectedVersion: number(source.Version), RevisionID: source.RevisionID}); err != nil {
		t.Fatal(err)
	}
	in := input()
	in.Slug = "bulk-source-fr"
	in.SourceRevisionID = source.RevisionID
	if _, err := s.Save(ctx, owner, "bulk-source", "fr", "", in); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ChangePublicationBulk(ctx, owner, BulkPublication{Operation: "publish", Items: []BulkPublicationItem{first, bulkItem(t, s, "bulk-source", "fr", "publish")}}); !errors.Is(err, ErrReview) {
		t.Fatal(err)
	}
	checkPrivate()
	calls := 0
	s.PublicationHook = func(context.Context, *sql.Tx, string, string, bool) error {
		calls++
		if calls == 2 {
			return ErrInvalid
		}
		return nil
	}
	if _, err := s.ChangePublicationBulk(ctx, owner, BulkPublication{Operation: "publish", Items: []BulkPublicationItem{first, bulkItem(t, s, "bulk-last", "en", "publish")}}); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	checkPrivate()
}
func TestBulkPublicationRejectsInvalidSelections(t *testing.T) {
	s, owner := fixture(t)
	saveBulkArticle(t, s, owner, "bulk-invalid")
	item := bulkItem(t, s, "bulk-invalid", "en", "publish")
	bad := item
	bad.RevisionID = "invalid"
	noVersion := item
	noVersion.ExpectedVersion = nil
	for _, batch := range []BulkPublication{{Operation: "review", Items: []BulkPublicationItem{item}}, {Operation: "publish"}, {Operation: "publish", Items: []BulkPublicationItem{item, item}}, {Operation: "publish", Items: []BulkPublicationItem{bad}}, {Operation: "publish", Items: []BulkPublicationItem{noVersion}}, {Operation: "publish", Items: make([]BulkPublicationItem, 501)}} {
		if _, err := s.ChangePublicationBulk(context.Background(), owner, batch); !errors.Is(err, ErrInvalid) {
			t.Fatal(err)
		}
	}
}
func TestBulkUnpublishKeepsLearningBoundariesClosed(t *testing.T) {
	s, owner, _ := gatedFixture(t)
	ctx := context.Background()
	_, err := s.ChangePublicationBulk(ctx, owner, BulkPublication{Operation: "unpublish", Items: []BulkPublicationItem{bulkItem(t, s, "first", "en", "unpublish"), bulkItem(t, s, "third", "en", "unpublish")}})
	if err != nil {
		t.Fatal(err)
	}
	out, err := s.Public(ctx, "en", "fourth", access.Principal{})
	if !errors.Is(err, ErrLocked) || out.Markdown != "" || len(out.Media) != 0 {
		t.Fatal("gate leaked", out, err)
	}
}
