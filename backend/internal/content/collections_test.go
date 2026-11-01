package content

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/mbougarne/modawana/backend/internal/access"
	"github.com/mbougarne/modawana/backend/internal/db"
)

func TestCollectionPlansAnnouncementsAndRoutes(t *testing.T) {
	s, o := fixture(t)
	ctx := context.Background()
	s.Membership = s.AttachMember
	s.PublicationHook = s.CheckPublicationPlan
	s.Access.Gates = s.LessonAccess
	c, e := s.SaveCollection(ctx, "course", "en", CollectionInput{ExpectedVersion: number(0), Kind: "learning", Slug: "course", Title: "Course", Visible: true})
	if e != nil {
		t.Fatal(e)
	}
	revisions := []Result{}
	for i, key := range []string{"one", "secret-future", "hidden"} {
		in := input()
		in.Kind = "lesson"
		in.Slug = key
		in.Title = "PRIVATE DRAFT TITLE"
		in.CollectionKey = "course"
		in.Position = i + 1
		r, e := s.Save(ctx, o, key, "en", "", in)
		if e != nil {
			t.Fatal(e)
		}
		revisions = append(revisions, r)
	}
	if _, e = s.PublicCollection(ctx, "en", "course", access.Principal{}); !errors.Is(e, sql.ErrNoRows) {
		t.Fatal(e)
	}
	state, e := s.Collection(ctx, "course")
	if e != nil {
		t.Fatal(e)
	}
	members := []PlanMember{{Key: "one"}, {Key: "secret-future", Announcements: map[string]string{"en": "Next topic"}}, {Key: "hidden"}}
	plan, e := s.SavePlan(ctx, o, "course", PlanInput{ExpectedVersion: number(state.Version), Members: members})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.PublicCollection(ctx, "en", "course", access.Principal{}); !errors.Is(e, sql.ErrNoRows) {
		t.Fatal("draft plan public", e)
	}
	active, e := s.ApplyPlan(ctx, "course", ApplyPlanInput{ExpectedVersion: number(plan.Version), PlanID: plan.PlanID})
	if e != nil {
		t.Fatal(e)
	}
	pub, e := s.PublicCollection(ctx, "en", "course", access.Principal{})
	if e != nil || pub.Total != 1 || pub.PublishedCount != 0 || pub.AnnouncedCount != 1 || pub.Items[0].URL != "" || pub.Items[0].Key != "" {
		t.Fatal(pub, e)
	}
	b, _ := json.Marshal(pub)
	if strings.Contains(string(b), "PRIVATE") || strings.Contains(string(b), "secret-future") || strings.Contains(string(b), "hidden") {
		t.Fatal("private data leaked", string(b))
	}
	if _, e = s.Public(ctx, "en", "secret-future", access.Principal{}); !errors.Is(e, sql.ErrNoRows) {
		t.Fatal("soon route accessible", e)
	}
	if _, e = s.ChangePublication(ctx, o, "one", "en", "", "publish", Action{ExpectedVersion: number(1), RevisionID: revisions[0].RevisionID}); e != nil {
		t.Fatal(e)
	}
	pub, e = s.PublicCollection(ctx, "en", "course", access.Principal{})
	if e != nil || pub.Total != 2 || pub.PublishedCount != 1 || pub.Items[0].State != "open" {
		t.Fatal(pub, e)
	}
	reordered := []PlanMember{members[1], members[0], members[2]}
	plan2, e := s.SavePlan(ctx, o, "course", PlanInput{ExpectedVersion: number(active.Version), Members: reordered})
	if e != nil {
		t.Fatal(e)
	}
	pub, _ = s.PublicCollection(ctx, "en", "course", access.Principal{})
	if pub.Items[0].State != "open" {
		t.Fatal("draft reorder applied")
	}
	if _, e = s.ApplyPlan(ctx, "course", ApplyPlanInput{ExpectedVersion: number(active.Version), PlanID: plan2.PlanID}); !errors.Is(e, db.ErrVersionConflict) {
		t.Fatal(e)
	}
	applied, e := s.ApplyPlan(ctx, "course", ApplyPlanInput{ExpectedVersion: number(plan2.Version), PlanID: plan2.PlanID})
	if e != nil {
		t.Fatal(e)
	}
	pub, _ = s.PublicCollection(ctx, "en", "course", access.Principal{})
	if pub.Items[0].State != "soon" || pub.Items[1].Slug != "one" {
		t.Fatal(pub)
	}
	if _, e = s.SaveCollection(ctx, "course", "en", CollectionInput{ExpectedVersion: number(applied.Version), Kind: "learning", Slug: "changed", Title: "Course", Visible: true}); !errors.Is(e, ErrInvalid) {
		t.Fatal("route changed", e)
	}
	if _, e = s.SavePlan(ctx, o, "course", PlanInput{ExpectedVersion: number(applied.Version), Members: members[1:]}); !errors.Is(e, ErrInvalid) {
		t.Fatal("published membership removed", e)
	}
	if _, e = s.PublicCollection(ctx, "fr", "course", access.Principal{}); !errors.Is(e, sql.ErrNoRows) {
		t.Fatal("locale leaked", e)
	}
	_, e = s.SaveCollection(ctx, "other", "en", CollectionInput{ExpectedVersion: number(0), Kind: "learning", Slug: "other", Title: "Other"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.SavePlan(ctx, o, "other", PlanInput{ExpectedVersion: number(1), Members: []PlanMember{{Key: "one"}}}); !errors.Is(e, ErrInvalid) {
		t.Fatal("move accepted", e)
	}
	// Simulate a later gate-aware release. This release cannot overwrite its plan.
	future, _ := db.NewID()
	if _, e = s.DB.Exec("INSERT INTO collection_plans(id,collection_id,created_by,gate_policy_version) VALUES(?,?,?,2)", future, c.ID, o); e != nil {
		t.Fatal(e)
	}
	s.DB.Exec("INSERT INTO collection_plan_members(plan_id,item_id,position,announcements_json) SELECT ?,item_id,position,announcements_json FROM collection_plan_members WHERE plan_id=?", future, plan2.PlanID)
	s.DB.Exec("UPDATE collection_plan_state SET active_plan_id=? WHERE collection_id=?", future, c.ID)
	if _, e = s.Public(ctx, "en", "one", access.Principal{}); !errors.Is(e, ErrLocked) {
		t.Fatal("future gate bypass", e)
	}
	if _, e = s.SavePlan(ctx, o, "course", PlanInput{ExpectedVersion: number(applied.Version), Members: members}); !errors.Is(e, ErrInvalid) {
		t.Fatal("future gate dropped", e)
	}
	if _, e = s.ApplyPlan(ctx, "course", ApplyPlanInput{ExpectedVersion: number(applied.Version), PlanID: plan2.PlanID}); !errors.Is(e, ErrInvalid) {
		t.Fatal("future active gate replaced", e)
	}
}
