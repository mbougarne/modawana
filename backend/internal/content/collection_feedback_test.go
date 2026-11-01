package content

import (
	"context"
	"errors"
	"testing"
)

func TestCollectionFieldFeedbackAndLocalizedTitles(t *testing.T) {
	s, _ := fixture(t)
	ctx := context.Background()
	in := CollectionInput{ExpectedVersion: number(0), Kind: "learning", Slug: "bad slug", Title: "Readable title"}
	_, err := s.SaveCollection(ctx, "opaque-key", "en", in)
	var field *TypeChangeError
	if !errors.As(err, &field) || field.Path != "slug" || field.Code != "collection_slug" {
		t.Fatal("unrelated collection advice", err)
	}
	in.Slug = "shared"
	r, err := s.SaveCollection(ctx, "opaque-key", "en", in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveCollection(ctx, "another-key", "en", in); !errors.As(err, &field) || field.Code != "collection_slug_taken" {
		t.Fatal("missing duplicate slug reason", err)
	}
	in.ExpectedVersion = number(r.Version)
	in.Title = "Titre français"
	in.Slug = "titre"
	if _, err = s.SaveCollection(ctx, "opaque-key", "fr", in); err != nil {
		t.Fatal(err)
	}
	rows, _, err := s.Collections(ctx, "", "fr")
	if err != nil || len(rows) != 1 || rows[0].Title != "Titre français" {
		t.Fatal(rows, err)
	}
	rows, _, err = s.Collections(ctx, "", "ar")
	if err != nil || rows[0].Title != "Readable title" {
		t.Fatal("owner title fallback", rows, err)
	}
	in.ExpectedVersion = number(0)
	in.Slug = "fresh"
	in.Title = ""
	if _, err = s.SaveCollection(ctx, "new-key", "en", in); !errors.As(err, &field) || field.Code != "collection_title" {
		t.Fatal("missing title reason", err)
	}
}
