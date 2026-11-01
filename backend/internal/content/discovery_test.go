package content

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/mbougarne/modawana/backend/internal/access"
	"github.com/mbougarne/modawana/backend/internal/db"
)

func TestDiscoveryFiltersCountsAndPagination(t *testing.T) {
	s, o := fixture(t)
	ctx := context.Background()
	term := TaxonomyInput{ExpectedVersion: number(0), Label: "Systems", Slug: "systems"}
	c, e := s.SaveTerm(ctx, "category", "systems", "en", term)
	if e != nil {
		t.Fatal(e)
	}
	term.Label = "Go"
	term.Slug = "go"
	if _, e = s.SaveTerm(ctx, "tag", "go", "en", term); e != nil {
		t.Fatal(e)
	}
	term.ExpectedVersion = number(1)
	term.Label = "الأنظمة"
	term.Slug = "الأنظمة"
	if _, e = s.SaveTerm(ctx, "category", "systems", "ar", term); e != nil {
		t.Fatal(e)
	}
	if _, e = s.SaveTerm(ctx, "category", "systems", "en", term); !errors.Is(e, db.ErrVersionConflict) {
		t.Fatal(e)
	}
	if c.ID == "" {
		t.Fatal("missing stable id")
	}
	for n := 0; n < 4; n++ {
		in := input()
		in.Slug = fmt.Sprintf("article-%d", n)
		in.Title = "Go memory"
		in.CategoryKey = "systems"
		if n < 3 {
			in.TagKeys = []string{"go"}
		}
		r, e := s.Save(ctx, o, in.Slug, "en", "", in)
		if e != nil {
			t.Fatal(e)
		}
		if n != 2 {
			if _, e = s.ChangePublication(ctx, o, in.Slug, "en", "", "publish", Action{ExpectedVersion: number(1), RevisionID: r.RevisionID}); e != nil {
				t.Fatal(e)
			}
		}
		if n == 0 {
			in.SourceRevisionID = r.RevisionID
			in.Title = "private translated title"
			in.Slug = "خاص"
			in.TagKeys = nil
			if _, e = s.Save(ctx, o, "article-0", "ar", "", in); e != nil {
				t.Fatal(e)
			}
		}
	}
	f := Filter{Locale: "en", Kind: "article", Category: "systems", Tag: "go", Query: "memory", Limit: 1}
	page, e := s.Discover(ctx, f, access.Principal{})
	if e != nil || page.Total != 2 || len(page.Items) != 1 || page.Next == "" {
		t.Fatal(page, e)
	}
	f.Cursor = page.Next
	page2, e := s.Discover(ctx, f, access.Principal{})
	if e != nil || page2.Total != 2 || len(page2.Items) != 1 || page2.Items[0].Key == page.Items[0].Key || page2.Next != "" {
		t.Fatal(page2, e)
	}
	f.Tag = "missing"
	if _, e = s.Discover(ctx, f, access.Principal{}); !errors.Is(e, ErrInvalid) {
		t.Fatal("cursor filter mismatch", e)
	}
	f.Cursor = ""
	empty, e := s.Discover(ctx, f, access.Principal{})
	if e != nil || empty.Total != 0 || len(empty.Items) != 0 {
		t.Fatal(empty, e)
	}
	terms, e := s.Taxonomy(ctx, "en", false)
	if e != nil || len(terms) != 2 {
		t.Fatal(terms, e)
	}
	for _, v := range terms {
		want := 3
		if v.Kind == "tag" {
			want = 2
		}
		if v.Count != want || v.Version != 0 {
			t.Fatal(v)
		}
	}
	ar, e := s.Taxonomy(ctx, "ar", false)
	if e != nil || len(ar) != 0 {
		t.Fatal("unpublished taxonomy leaked", ar, e)
	}
	page, e = s.Discover(ctx, Filter{Locale: "ar", Query: "private"}, access.Principal{})
	if e != nil || page.Total != 0 {
		t.Fatal("unpublished title leaked", page, e)
	}
}
