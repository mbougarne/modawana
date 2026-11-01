package content

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/mbougarne/modawana/backend/internal/access"
	"github.com/mbougarne/modawana/backend/internal/db"
)

func TestOwnerSaveAtomicallyChangesTypeAndText(t *testing.T) {
	s, owner := fixture(t)
	ctx := context.Background()
	c := conversionCollection(t, s, "course", "learning")
	in := input()
	r, err := s.Save(ctx, owner, "first", "en", "", in)
	if err != nil {
		t.Fatal(err)
	}
	pub, err := s.ChangePublication(ctx, owner, "first", "en", "", "publish", Action{ExpectedVersion: number(r.Version), RevisionID: r.RevisionID})
	if err != nil {
		t.Fatal(err)
	}
	in.ExpectedVersion = number(pub.Version)
	in.Kind = "lesson"
	in.CollectionKey = "course"
	in.ExpectedCollectionVersion = number(c.Version)
	in.Position = 0
	in.Title = "Edited during type change"
	in.Markdown = "New working body"
	saved, err := s.Save(ctx, owner, "first", "en", "inline-change", in)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := s.Save(ctx, owner, "first", "en", "inline-change", in)
	if err != nil || retry != saved {
		t.Fatal("save retry changed result", err)
	}
	d, err := s.Draft(ctx, "first", "en")
	if err != nil || d.Kind != "lesson" || d.Revision.Title != in.Title || d.Revision.Markdown != in.Markdown || d.PublishedID != r.RevisionID || d.Version != pub.Version+1 {
		t.Fatal("partial inline save", d, err)
	}
	old, err := s.revision(ctx, s.DB, r.RevisionID, func() string { v, _ := loadVersion(ctx, s.DB, "first", "en"); return v.ID }())
	if err != nil || old.Markdown == in.Markdown {
		t.Fatal("published revision overwritten", err)
	}
	var events int
	s.DB.QueryRow("SELECT count(*) FROM content_type_events").Scan(&events)
	if events != 1 {
		t.Fatal("save duplicated conversion audit", events)
	}
}
func TestFailedOwnerSaveRollsBackTypeAndCollection(t *testing.T) {
	s, owner := fixture(t)
	ctx := context.Background()
	c := conversionCollection(t, s, "course", "learning")
	in := input()
	r, err := s.Save(ctx, owner, "first", "en", "", in)
	if err != nil {
		t.Fatal(err)
	}
	in.ExpectedVersion = number(r.Version)
	in.Kind = "lesson"
	in.CollectionKey = "course"
	in.ExpectedCollectionVersion = number(c.Version)
	in.CoverMediaID = strings.Repeat("a", 32)
	if _, err = s.Save(ctx, owner, "first", "en", "bad", in); err == nil {
		t.Fatal("missing media accepted")
	}
	d, _ := s.Draft(ctx, "first", "en")
	collection, _ := s.Collection(ctx, "course")
	if d.Kind != "article" || d.Version != r.Version || d.Revision.ID != r.RevisionID || collection.Version != c.Version {
		t.Fatal("failed save partly changed state")
	}
	var n int
	s.DB.QueryRow("SELECT count(*) FROM content_type_events").Scan(&n)
	if n != 0 {
		t.Fatal("failed save kept event")
	}
	s.DB.QueryRow("SELECT count(*) FROM content_type_conversion_guard").Scan(&n)
	if n != 0 {
		t.Fatal("failed save kept guard")
	}
	in.CoverMediaID = ""
	in.ExpectedCollectionVersion = number(c.Version + 1)
	if _, err = s.Save(ctx, owner, "first", "en", "", in); !errors.Is(err, db.ErrVersionConflict) {
		t.Fatal("stale collection not rejected", err)
	}
}
func TestPublisherCannotChangeTypeViaDraftSave(t *testing.T) {
	s, owner := fixture(t)
	ctx := context.Background()
	c := conversionCollection(t, s, "course", "learning")
	in := input()
	r, err := s.Save(ctx, owner, "first", "en", "", in)
	if err != nil {
		t.Fatal(err)
	}
	in.ExpectedVersion = number(r.Version)
	in.Kind = "lesson"
	in.CollectionKey = "course"
	in.ExpectedCollectionVersion = number(c.Version)
	scoped := access.WithMutation(ctx, access.Mutation{Actor: "publisher:fixture", Check: func(_ *sql.Tx, _ string) error { return nil }})
	_, err = s.Save(scoped, owner, "first", "en", "", in)
	var issue *TypeChangeError
	if !errors.As(err, &issue) || issue.Code != "conversion_owner" {
		t.Fatal("publisher changed existing type", err)
	}
}
