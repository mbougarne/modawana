package content

import (
	"context"
	"github.com/mbougarne/modawana/backend/internal/access"
	"testing"
)

func TestDuplicateDraftSlugsAreNumbered(t *testing.T) {
	s, o := fixture(t)
	ctx := context.Background()
	for n, key := range []string{"first", "second", "third"} {
		in := input()
		in.Slug = "same"
		if _, err := s.Save(ctx, o, key, "en", "", in); err != nil {
			t.Fatal(err)
		}
		var slug string
		s.DB.QueryRow("SELECT slug FROM content_versions v JOIN content_items i ON i.id=v.item_id WHERE i.key=?", key).Scan(&slug)
		want := []string{"same", "same-2", "same-3"}[n]
		if slug != want {
			t.Fatalf("got %s want %s", slug, want)
		}
	}
}

func TestRemoveCollectionPreservesContentWithoutUnlocking(t *testing.T) {
	s, o := fixture(t)
	ctx := context.Background()
	s.Membership = s.AttachMember
	c, err := s.SaveCollection(ctx, "remove-me", "en", CollectionInput{ExpectedVersion: number(0), Kind: "learning", Slug: "remove-me", Title: "Remove", Visible: true})
	if err != nil {
		t.Fatal(err)
	}
	in := input()
	in.Kind = "lesson"
	in.CollectionKey = "remove-me"
	in.Position = 1
	saved, err := s.Save(ctx, o, "retained", "en", "", in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ChangePublication(ctx, o, "retained", "en", "", "publish", Action{ExpectedVersion: number(saved.Version), RevisionID: saved.RevisionID}); err != nil {
		t.Fatal(err)
	}
	state, err := s.Collection(ctx, "remove-me")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.RemoveCollection(ctx, o, "remove-me", state.Version); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Public(ctx, "en", in.Slug, access.Principal{}); err == nil {
		t.Fatal("detached lesson became public")
	}
	var kind string
	var count int
	s.DB.QueryRow("SELECT kind FROM content_items WHERE key='retained'").Scan(&kind)
	s.DB.QueryRow("SELECT count(*) FROM revisions").Scan(&count)
	if kind != "article" || count != 1 {
		t.Fatal(kind, count)
	}
	list, _, err := s.Collections(ctx, "")
	if err != nil || len(list) != 0 {
		t.Fatal(list, err)
	}
	if _, err = s.PublicCollection(ctx, "en", "remove-me", access.Principal{}); err == nil {
		t.Fatal("removed collection public")
	}
	if _, err = s.DB.Exec("UPDATE collection_locales SET visible=1 WHERE collection_id=?", c.ID); err == nil {
		t.Fatal("removed collection restored")
	}
}
