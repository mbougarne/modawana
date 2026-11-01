package content

import (
	"context"
	"testing"
)

func TestOwnerListStatusAndPublishedSlug(t *testing.T) {
	s, o := fixture(t)
	ctx := context.Background()
	r, e := s.Save(ctx, o, "owner-list", "en", "", input())
	if e != nil {
		t.Fatal(e)
	}
	check := func(status string, n int) {
		t.Helper()
		rows, _, e := s.OwnerList(ctx, "en", "article", status, "First", "")
		if e != nil || len(rows) != n {
			t.Fatal(rows, e)
		}
	}
	check("draft", 1)
	check("published", 0)
	p, e := s.ChangePublication(ctx, o, "owner-list", "en", "", "publish", Action{ExpectedVersion: number(r.Version), RevisionID: r.RevisionID})
	if e != nil {
		t.Fatal(e)
	}
	check("published", 1)
	in := input()
	in.ExpectedVersion = number(p.Version)
	in.Markdown = "Unpublished edit"
	r, e = s.Save(ctx, o, "owner-list", "en", "", in)
	if e != nil {
		t.Fatal(e)
	}
	check("changed", 1)
	_, e = s.ChangePublication(ctx, o, "owner-list", "en", "", "unpublish", Action{ExpectedVersion: number(r.Version), RevisionID: p.RevisionID})
	if e != nil {
		t.Fatal(e)
	}
	d, e := s.Draft(ctx, "owner-list", "en")
	if e != nil || !d.SlugLocked {
		t.Fatal(d, e)
	}
	if _, _, e = s.OwnerList(ctx, "xx", "", "", "", ""); e != ErrInvalid {
		t.Fatal(e)
	}
}
