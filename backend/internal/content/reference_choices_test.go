package content

import (
	"context"
	"fmt"
	"testing"
)

func TestReferenceChoicesSearchAllRowsWithFiveItemLimit(t *testing.T) {
	s, _ := fixture(t)
	ctx := context.Background()
	for i := 0; i < 8; i++ {
		key := fmt.Sprintf("path-%d", i)
		title := fmt.Sprintf("Course %d", i)
		if i == 7 {
			title = "Needle course"
		}
		if _, err := s.SaveCollection(ctx, key, "en", CollectionInput{ExpectedVersion: number(0), Kind: "learning", Title: title, Slug: key}); err != nil {
			t.Fatal(err)
		}
	}
	conversionCollection(t, s, "guide", "docs")
	first, err := s.ReferenceChoices(ctx, "en", "learning", "", "")
	if err != nil || len(first.Items) != 5 || !first.More {
		t.Fatal(first, err)
	}
	found, err := s.ReferenceChoices(ctx, "en", "learning", "Needle", "")
	if err != nil || len(found.Items) != 1 || found.Items[0].Key != "path-7" || found.Items[0].Version < 1 {
		t.Fatal("search only inspected initial page", found, err)
	}
	guide, err := s.ReferenceChoices(ctx, "en", "docs", "", "")
	if err != nil || len(guide.Items) != 1 || guide.Items[0].Key != "guide" {
		t.Fatal("kind filter failed", guide, err)
	}
}
func TestContentReferenceChoicesUseExistingCollectionAndLocale(t *testing.T) {
	s, owner := fixture(t)
	ctx := context.Background()
	s.Membership = s.AttachMember
	for _, key := range []string{"one", "two"} {
		conversionCollection(t, s, key, "learning")
		in := input()
		in.Kind = "lesson"
		in.CollectionKey = key
		in.Position = 1
		in.Slug = key
		if _, err := s.Save(ctx, owner, key, "en", "", in); err != nil {
			t.Fatal(err)
		}
	}
	out, err := s.ReferenceChoices(ctx, "en", "lesson", "", "two")
	if err != nil || len(out.Items) != 1 || out.Items[0].Key != "two" {
		t.Fatal("offered a member from another path", out, err)
	}
	out, err = s.ReferenceChoices(ctx, "ar", "lesson", "", "two")
	if err != nil || len(out.Items) != 0 {
		t.Fatal("cross-locale fallback", out, err)
	}
}

func TestCollectionPickerOffersUnassignedArticlesAndLessonsExcludingSelections(t *testing.T) {
	s, owner := fixture(t)
	ctx := context.Background()
	s.Membership = s.AttachMember
	conversionCollection(t, s, "course", "learning")
	conversionCollection(t, s, "other", "learning")
	for i := 0; i < 8; i++ {
		in := input()
		in.Slug = fmt.Sprintf("article-%d", i)
		if _, err := s.Save(ctx, owner, in.Slug, "en", "", in); err != nil {
			t.Fatal(err)
		}
	}
	for _, key := range []string{"assigned", "unassigned", "foreign"} {
		in := input()
		in.Kind = "lesson"
		in.Slug = key
		if key == "assigned" {
			in.CollectionKey = "course"
		}
		if key == "foreign" {
			in.CollectionKey = "other"
		}
		if _, err := s.Save(ctx, owner, key, "en", "", in); err != nil {
			t.Fatal(err)
		}
	}
	out, err := s.ReferenceChoices(ctx, "en", "learning_item", "", "course", "article-0", "article-1", "article-2", "article-3", "article-4")
	if err != nil || len(out.Items) != 5 || out.More {
		t.Fatal("selection exclusions applied after limit", out, err)
	}
	seen := map[string]bool{}
	for _, v := range out.Items {
		seen[v.Key] = true
	}
	if !seen["article-5"] || !seen["article-7"] || !seen["assigned"] || !seen["unassigned"] || seen["foreign"] {
		t.Fatal("picker hid second eligible item", seen)
	}
	out, err = s.ReferenceChoices(ctx, "ar", "learning_item", "", "course")
	if err != nil || len(out.Items) != 0 {
		t.Fatal("locale filter", out, err)
	}
}
