package content

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/mbougarne/modawana/backend/internal/access"
	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/media"
)

func TestPublicationReviewAndStaleTranslations(t *testing.T) {
	s, o := fixture(t)
	ctx := context.Background()
	in := input()
	r, e := s.Save(ctx, o, "first", "en", "", in)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Public(ctx, "en", "first", access.Principal{}); !errors.Is(e, sql.ErrNoRows) {
		t.Fatal("draft public", e)
	}
	p, e := s.ChangePublication(ctx, o, "first", "en", "publish", "publish", Action{ExpectedVersion: number(1), RevisionID: r.RevisionID})
	if e != nil || p.Version != 2 {
		t.Fatal(p, e)
	}
	if again, e := s.ChangePublication(ctx, o, "first", "en", "publish", "publish", Action{ExpectedVersion: number(1), RevisionID: r.RevisionID}); e != nil || again != p {
		t.Fatal(again, e)
	}
	ar := input()
	ar.Slug = "الأول"
	ar.SourceRevisionID = r.RevisionID
	ar.Markdown = "# ترجمة"
	ar.Title = "الأول"
	rAr, e := s.Save(ctx, o, "first", "ar", "", ar)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ChangePublication(ctx, o, "first", "ar", "", "publish", Action{ExpectedVersion: number(1), RevisionID: rAr.RevisionID}); !errors.Is(e, ErrReview) {
		t.Fatal("unreviewed published", e)
	}
	review, e := s.ChangePublication(ctx, o, "first", "ar", "", "review", Action{ExpectedVersion: number(1), RevisionID: rAr.RevisionID, SourceRevisionID: r.RevisionID})
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.ChangePublication(ctx, o, "first", "ar", "", "publish", Action{ExpectedVersion: number(review.Version), RevisionID: rAr.RevisionID})
	if e != nil {
		t.Fatal(e)
	}
	pub, e := s.Public(ctx, "ar", "الأول", access.Principal{})
	if e != nil || pub.Markdown != ar.Markdown || pub.Stale || len(pub.Alternatives) != 2 {
		t.Fatal(pub, e)
	}
	in.ExpectedVersion = number(2)
	in.Markdown = "# New original"
	newR, e := s.Save(ctx, o, "first", "en", "", in)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ChangePublication(ctx, o, "first", "en", "", "publish", Action{ExpectedVersion: number(3), RevisionID: newR.RevisionID}); e != nil {
		t.Fatal(e)
	}
	pub, e = s.Public(ctx, "ar", "الأول", access.Principal{})
	if e != nil || !pub.Stale || pub.Markdown != ar.Markdown {
		t.Fatal(pub, e)
	}
	if _, e = s.ChangePublication(ctx, o, "first", "ar", "", "publish", Action{ExpectedVersion: number(3), RevisionID: rAr.RevisionID}); !errors.Is(e, ErrReview) {
		t.Fatal("stale publication", e)
	}
	if _, e = s.ChangePublication(ctx, o, "first", "en", "", "unpublish", Action{ExpectedVersion: number(4), RevisionID: newR.RevisionID}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Public(ctx, "en", "first", access.Principal{}); !errors.Is(e, sql.ErrNoRows) {
		t.Fatal("unpublished public", e)
	}
	alternatives, e := s.Alternatives(ctx, "first")
	if e != nil || len(alternatives) != 1 || alternatives[0].Locale != "ar" {
		t.Fatal(alternatives, e)
	}
}
func TestPublicationMediaAndFailClosedLesson(t *testing.T) {
	s, o := fixture(t)
	ctx := context.Background()
	mservice := media.New(s.DB, s.Files, s.Access)
	m, e := mservice.Upload(ctx, o, "track.vtt", "text/vtt", "content", strings.NewReader("WEBVTT\n\n00:00.000 --> 00:01.000\nHello\n"))
	if e != nil {
		t.Fatal(e)
	}
	in := input()
	in.Kind = "lesson"
	in.Markdown = "[Captions](" + m.URL + ")"
	r, e := s.Save(ctx, o, "lesson", "en", "", in)
	if e != nil {
		t.Fatal(e)
	}
	if e = mservice.Delete(ctx, m.ID); !errors.Is(e, media.ErrReferenced) {
		t.Fatal(e)
	}
	_, e = s.ChangePublication(ctx, o, "lesson", "en", "", "publish", Action{ExpectedVersion: number(1), RevisionID: r.RevisionID})
	if e != nil {
		t.Fatal(e)
	}
	out, e := s.Public(ctx, "en", "first", access.Principal{})
	// An unassigned lesson is hidden, not a navigable locked lesson.
	if !errors.Is(e, sql.ErrNoRows) || out.Markdown != "" || len(out.Media) > 0 {
		t.Fatal("unassigned lesson leaked", out, e)
	}
	d, e := s.Access.Resolve(ctx, access.Request{Surface: access.Media, ResourceID: m.ID})
	if e != nil || d.AllowsBody() {
		t.Fatal("locked-only media public", e)
	}
	// A deliberate open-article reuse permits the same media; removing that live
	// reference restores privacy without deleting retained history.
	in.Kind = "article"
	in.Slug = "open"
	r2, e := s.Save(ctx, o, "open", "en", "", in)
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.ChangePublication(ctx, o, "open", "en", "", "publish", Action{ExpectedVersion: number(1), RevisionID: r2.RevisionID})
	if e != nil {
		t.Fatal(e)
	}
	d, e = s.Access.Resolve(ctx, access.Request{Surface: access.Media, ResourceID: m.ID})
	if e != nil || !d.AllowsBody() {
		t.Fatal(e)
	}
	s.PublicationHook = func(context.Context, *sql.Tx, string, string, bool) error { return db.ErrVersionConflict }
	if _, e = s.ChangePublication(ctx, o, "open", "en", "", "unpublish", Action{ExpectedVersion: number(2), RevisionID: r2.RevisionID}); !errors.Is(e, db.ErrVersionConflict) {
		t.Fatal("hook bypass", e)
	}
	s.PublicationHook = nil
	_, e = s.ChangePublication(ctx, o, "open", "en", "", "unpublish", Action{ExpectedVersion: number(2), RevisionID: r2.RevisionID})
	if e != nil {
		t.Fatal(e)
	}
	d, e = s.Access.Resolve(ctx, access.Request{Surface: access.Media, ResourceID: m.ID})
	if e != nil || d.AllowsBody() {
		t.Fatal("unpublish leak", e)
	}
	// Corrupt bytes cannot be published, but unpublishing must still be possible.
	_, e = s.DB.Exec("UPDATE media SET sha256=? WHERE id=?", strings.Repeat("0", 64), m.ID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ChangePublication(ctx, o, "lesson", "en", "", "publish", Action{ExpectedVersion: number(2), RevisionID: r.RevisionID}); e == nil {
		t.Fatal("corrupt published")
	}
	if _, e = s.ChangePublication(ctx, o, "lesson", "en", "", "unpublish", Action{ExpectedVersion: number(2), RevisionID: r.RevisionID}); e != nil {
		t.Fatal(e)
	}
}
