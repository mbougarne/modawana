package content

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/mbougarne/modawana/backend/internal/access"
	"github.com/mbougarne/modawana/backend/internal/db"
)

func TestCollectionWorkspaceAddsMultiplePublishedArticlesInOneSave(t *testing.T) {
	s, owner := fixture(t)
	ctx := context.Background()
	s.Membership = s.AttachMember
	s.PublicationHook = s.CheckPublicationPlan
	s.Access.Gates = s.LessonAccess
	members := []WorkspaceMember{}
	published := map[string]string{}
	for _, key := range []string{"first", "second", "private"} {
		in := input()
		in.Slug = key
		in.Title = "Title " + key
		r, err := s.Save(ctx, owner, key, "en", "", in)
		if err != nil {
			t.Fatal(err)
		}
		if key != "private" {
			r, err = s.ChangePublication(ctx, owner, key, "en", "", "publish", Action{ExpectedVersion: number(r.Version), RevisionID: r.RevisionID})
			if err != nil {
				t.Fatal(err)
			}
			published[key] = r.RevisionID
		}
		members = append(members, WorkspaceMember{Key: key, ExpectedVersion: number(r.Version)})
	}
	in := CollectionWorkspaceInput{CollectionInput: CollectionInput{ExpectedVersion: number(0), Kind: "learning", Slug: "course", Title: "Course", Visible: true}, Members: members}
	saved, err := s.SaveCollectionWorkspace(ctx, owner, "course", "en", "one-save", in)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := s.SaveCollectionWorkspace(ctx, owner, "course", "en", "one-save", in)
	if err != nil || replay != saved {
		t.Fatal("retry", replay, saved, err)
	}
	c, err := s.Collection(ctx, "course")
	if err != nil || len(c.ActiveMembers) != 3 || c.ActivePlanID != saved.PlanID || c.MemberTitles["second"] != "Title second" {
		t.Fatal(c, err)
	}
	for key, id := range published {
		d, err := s.Draft(ctx, key, "en")
		if err != nil || d.Kind != "lesson" || d.PublishedID != id || d.Revision.ID != id || d.CollectionKey != "course" {
			t.Fatal("content replaced", d, err)
		}
	}
	list, err := s.PublicCollections(ctx, "en", "learning", "", 20)
	if err != nil || len(list.Items) != 1 || list.Items[0].PublishedCount != 2 || list.Items[0].Total != 2 {
		t.Fatal("second public article missing or private lesson leaked", list, err)
	}
	if _, err = s.Public(ctx, "en", "private", access.Principal{}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("draft published", err)
	}
	// Saving details again preserves all members without requiring copied content versions.
	in.ExpectedVersion = number(saved.Version)
	for i := range in.Members {
		in.Members[i].ExpectedVersion = nil
	}
	in.Title = "Renamed"
	if _, err = s.SaveCollectionWorkspace(ctx, owner, "course", "en", "rename", in); err != nil {
		t.Fatal(err)
	}
}

func TestCollectionWorkspaceFailureRollsBackMetadataConversionsAndActivation(t *testing.T) {
	s, owner := fixture(t)
	ctx := context.Background()
	for _, key := range []string{"first", "second"} {
		in := input()
		in.Slug = key
		if _, err := s.Save(ctx, owner, key, "en", "", in); err != nil {
			t.Fatal(err)
		}
	}
	in := CollectionWorkspaceInput{CollectionInput: CollectionInput{ExpectedVersion: number(0), Kind: "learning", Slug: "course", Title: "Course", Visible: true}, Members: []WorkspaceMember{{Key: "first", ExpectedVersion: number(1)}, {Key: "second", ExpectedVersion: number(99)}}}
	if _, err := s.SaveCollectionWorkspace(ctx, owner, "course", "en", "fail", in); !errors.Is(err, db.ErrVersionConflict) {
		t.Fatal(err)
	}
	if _, err := s.Collection(ctx, "course"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("partial collection", err)
	}
	d, err := s.Draft(ctx, "first", "en")
	if err != nil || d.Kind != "article" || d.Version != 1 || d.CollectionKey != "" {
		t.Fatal("partial conversion", d, err)
	}
	var n int
	s.DB.QueryRow("SELECT count(*) FROM content_type_events").Scan(&n)
	if n != 0 {
		t.Fatal("partial audit")
	}
}

func TestCollectionWorkspacePreservesGatesAndRejectsOtherPathMembers(t *testing.T) {
	s, owner, _ := gatedFixture(t)
	ctx := context.Background()
	c, err := s.Collection(ctx, "path")
	if err != nil {
		t.Fatal(err)
	}
	in := CollectionWorkspaceInput{CollectionInput: CollectionInput{ExpectedVersion: number(c.Version), Kind: "learning", Slug: "path", Title: "Updated path", Visible: true}}
	for _, m := range c.DraftMembers {
		in.Members = append(in.Members, WorkspaceMember{Key: m.Key, Announcements: m.Announcements})
	}
	r, err := s.SaveCollectionWorkspace(ctx, owner, "path", "en", "gates", in)
	if err != nil {
		t.Fatal(err)
	}
	var gates int
	s.DB.QueryRow("SELECT count(*) FROM collection_plan_gates WHERE plan_id=?", r.PlanID).Scan(&gates)
	if gates != 2 {
		t.Fatal("gates dropped", gates)
	}
	foreign := CollectionWorkspaceInput{CollectionInput: CollectionInput{ExpectedVersion: number(0), Kind: "learning", Slug: "foreign", Title: "Other"}, Members: []WorkspaceMember{{Key: "first"}}}
	if _, err = s.SaveCollectionWorkspace(ctx, owner, "foreign", "en", "foreign", foreign); err == nil {
		t.Fatal("moved gated lesson")
	}
	if _, err = s.Collection(ctx, "foreign"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("partial foreign collection", err)
	}
	// Existing publication cannot disappear through collection removal.
	in.ExpectedVersion = number(r.Version)
	in.Members = in.Members[1:]
	if _, err = s.SaveCollectionWorkspace(ctx, owner, "path", "en", "remove", in); err == nil {
		t.Fatal("removed published lesson")
	}
	after, _ := s.Collection(ctx, "path")
	if after.ActivePlanID != r.PlanID {
		t.Fatal("failed save changed active plan")
	}
}
