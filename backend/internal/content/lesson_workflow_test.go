package content

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/mbougarne/modawana/backend/internal/access"
	"github.com/mbougarne/modawana/backend/internal/db"
)

func TestLessonAssignmentPlanPublicationAndPublicVisibility(t *testing.T) {
	s, owner := fixture(t)
	ctx := context.Background()
	s.Membership = s.AttachMember
	s.PublicationHook = s.CheckPublicationPlan
	s.Access.Gates = s.LessonAccess
	if _, err := s.SaveCollection(ctx, "path", "en", CollectionInput{ExpectedVersion: number(0), Kind: "learning", Slug: "path", Title: "Learning path", Visible: true}); err != nil {
		t.Fatal(err)
	}
	in := input()
	in.Kind = "lesson"
	in.Slug = "first"
	initial, err := s.Save(ctx, owner, "first", "en", "", in)
	if err != nil {
		t.Fatal(err)
	}
	draft, err := s.Draft(ctx, "first", "en")
	if err != nil || draft.CollectionKey != "" {
		t.Fatal(draft, err)
	}
	in.ExpectedVersion = number(initial.Version)
	in.CollectionKey = "path"
	in.Position = 0
	saved, err := s.Save(ctx, owner, "first", "en", "attach", in)
	if err != nil {
		t.Fatal(err)
	}
	draft, err = s.Draft(ctx, "first", "en")
	if err != nil || draft.CollectionKey != "path" || draft.Position != 1 {
		t.Fatal("assignment missing on reopen", draft, err)
	}
	second := input()
	second.Kind = "lesson"
	second.Slug = "second"
	second.CollectionKey = "path"
	if _, err = s.Save(ctx, owner, "second", "en", "", second); err != nil {
		t.Fatal("automatic append", err)
	}
	c, err := s.Collection(ctx, "path")
	if err != nil || c.DraftPlanID != "" || len(c.DraftMembers) != 0 || len(c.AssignedMembers) != 2 || c.AssignedMembers[0].Key != "first" || c.AssignedMembers[1].Key != "second" {
		t.Fatal("initial plan choices", c, err)
	}
	if _, err = s.PublicCollection(ctx, "en", "path", access.Principal{}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("private plan exposed", err)
	}
	plan, err := s.SavePlan(ctx, owner, "path", PlanInput{ExpectedVersion: number(c.Version), Members: c.AssignedMembers})
	if err != nil {
		t.Fatal(err)
	}
	action := Action{ExpectedVersion: number(saved.Version), RevisionID: saved.RevisionID, Plan: &ApplyPlanInput{ExpectedVersion: number(c.Version), PlanID: plan.PlanID}}
	if _, err = s.ChangePublication(ctx, owner, "first", "en", "", "publish", action); !errors.Is(err, db.ErrVersionConflict) {
		t.Fatal("stale plan accepted", err)
	}
	draft, err = s.Draft(ctx, "first", "en")
	if err != nil || draft.PublishedID != "" {
		t.Fatal("failed activation partially published", draft, err)
	}
	action.Plan.ExpectedVersion = number(plan.Version)
	if _, err = s.ChangePublication(ctx, owner, "first", "en", "", "publish", action); err != nil {
		t.Fatal(err)
	}
	public, err := s.PublicCollection(ctx, "en", "path", access.Principal{})
	if err != nil || public.PublishedCount != 1 || public.Total != 1 || public.Items[0].Key != "first" {
		t.Fatal("published path missing or private second lesson leaked", public, err)
	}
	list, err := s.PublicCollections(ctx, "en", "learning", "", 20)
	if err != nil || len(list.Items) != 1 || list.Items[0].Key != "path" {
		t.Fatal("home/learn listing missing", list, err)
	}
	if _, err = s.Public(ctx, "en", "first", access.Principal{}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Public(ctx, "en", "second", access.Principal{}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("draft body exposed", err)
	}
	// Re-saving text retains the global assignment; position zero must not append it again.
	draft, _ = s.Draft(ctx, "first", "en")
	in.ExpectedVersion = number(draft.Version)
	if _, err = s.Save(ctx, owner, "first", "en", "resave", in); err != nil {
		t.Fatal(err)
	}
	after, _ := s.Collection(ctx, "path")
	if len(after.AssignedMembers) != 2 {
		t.Fatal("duplicate attachment")
	}
}
