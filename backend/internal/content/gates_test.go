package content

import (
	"context"
	"database/sql"
	"errors"
	"github.com/mbougarne/modawana/backend/internal/access"
	"github.com/mbougarne/modawana/backend/internal/learning"
	"testing"
)

func gatedFixture(t *testing.T) (*Service, string, access.Principal) {
	t.Helper()
	s, owner := fixture(t)
	ctx := context.Background()
	s.Membership = s.AttachMember
	s.PublicationHook = s.CheckPublicationPlan
	s.Access.Gates = s.LessonAccess
	if _, e := s.SaveCollection(ctx, "path", "en", CollectionInput{ExpectedVersion: number(0), Kind: "learning", Slug: "path", Title: "Path", Visible: true}); e != nil {
		t.Fatal(e)
	}
	members := []PlanMember{}
	for i, key := range []string{"first", "second", "third", "fourth"} {
		in := input()
		in.Kind = "lesson"
		in.CollectionKey = "path"
		in.Position = i + 1
		in.Slug = key
		if i == 0 || i == 2 {
			in.Quiz = quizExample()
		}
		r, e := s.Save(ctx, owner, key, "en", "", in)
		if e != nil {
			t.Fatal(e)
		}
		if in.Quiz != nil {
			if e = s.ReviewQuiz(ctx, owner, key, "en", Action{ExpectedVersion: number(r.Version), RevisionID: r.RevisionID}); e != nil {
				t.Fatal(e)
			}
		}
		if _, e = s.ChangePublication(ctx, owner, key, "en", "", "publish", Action{ExpectedVersion: number(r.Version), RevisionID: r.RevisionID}); e != nil {
			t.Fatal(e)
		}
		members = append(members, PlanMember{Key: key})
	}
	c, e := s.Collection(ctx, "path")
	if e != nil {
		t.Fatal(e)
	}
	p, e := s.SavePlan(ctx, owner, "path", PlanInput{ExpectedVersion: number(c.Version), Members: members})
	if e != nil {
		t.Fatal(e)
	}
	for _, key := range []string{"first", "third"} {
		d, e := s.Draft(ctx, key, "en")
		if e != nil {
			t.Fatal(e)
		}
		p, e = s.SaveQuizGate(ctx, owner, key, "en", QuizGateInput{ExpectedVersion: number(p.Version), PlanID: p.PlanID, Enabled: true, AssessmentID: d.Revision.Quiz.AssessmentID})
		if e != nil {
			t.Fatal(e)
		}
	}
	if _, e = s.ApplyPlan(ctx, "path", ApplyPlanInput{ExpectedVersion: number(p.Version), PlanID: p.PlanID}); e != nil {
		t.Fatal(e)
	}
	identities := learning.NewIdentity(s.DB, 180, 10)
	_, identity, e := identities.Create(ctx, "fixture")
	if e != nil {
		t.Fatal(e)
	}
	return s, owner, access.Principal{Audience: access.Learner, ID: identity.ID}
}
func grantFixture(t *testing.T, s *Service, p access.Principal, key string) {
	t.Helper()
	_, e := s.DB.Exec(`INSERT INTO learning_completion(identity_id,item_id,collection_id,epoch,method,assessment_id,revision_id,created_at) SELECT ?,i.id,m.collection_id,st.epoch,'quiz_pass',b.assessment_id,v.published_revision_id,strftime('%Y-%m-%dT%H:%M:%fZ','now') FROM content_items i JOIN content_versions v ON v.item_id=i.id AND v.locale='en' JOIN quiz_bindings b ON b.revision_id=v.published_revision_id JOIN collection_items m ON m.item_id=i.id JOIN collection_plan_state st ON st.collection_id=m.collection_id WHERE i.key=?`, p.ID, key)
	if e != nil {
		t.Fatal(e)
	}
}
func TestAllBoundariesReorderUnpublishAndEpoch(t *testing.T) {
	s, owner, p := gatedFixture(t)
	ctx := context.Background()
	for _, key := range []string{"second", "third", "fourth"} {
		out, e := s.Public(ctx, "en", key, p)
		if !errors.Is(e, ErrLocked) || out.Markdown != "" || len(out.Media) > 0 {
			t.Fatalf("%s leaked: %+v %v", key, out, e)
		}
	}
	grantFixture(t, s, p, "third")
	if _, e := s.Public(ctx, "en", "fourth", p); !errors.Is(e, ErrLocked) {
		t.Fatal("skipped earlier gate", e)
	}
	grantFixture(t, s, p, "first")
	if _, e := s.Public(ctx, "en", "fourth", p); e != nil {
		t.Fatal(e)
	}
	c, _ := s.Collection(ctx, "path")
	if _, e := s.SavePlan(ctx, owner, "path", PlanInput{ExpectedVersion: number(c.Version), Members: []PlanMember{{Key: "second"}, {Key: "first"}, {Key: "third"}, {Key: "fourth"}}}); !errors.Is(e, ErrInvalid) {
		t.Fatal("crossed gate", e)
	}
	draft, _ := s.Draft(ctx, "first", "en")
	if _, e := s.ChangePublication(ctx, owner, "first", "en", "", "unpublish", Action{ExpectedVersion: number(draft.Version), RevisionID: draft.PublishedID}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Public(ctx, "en", "fourth", access.Principal{}); !errors.Is(e, ErrLocked) {
		t.Fatal("unpublish opened gate", e)
	}
	if _, e := s.Public(ctx, "en", "fourth", p); e != nil {
		t.Fatal("earned pass lost", e)
	}
	if _, e := s.Public(ctx, "en", "first", p); !errors.Is(e, sql.ErrNoRows) {
		t.Fatal(e)
	}
	if _, e := s.ResetPathProgress(ctx, "path", c.Version, func(*sql.Tx) error { return nil }); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Public(ctx, "en", "fourth", p); !errors.Is(e, ErrLocked) {
		t.Fatal("epoch bypass", e)
	}
}

func TestReviewedAssessmentAndPlanPublishAtomically(t *testing.T) {
	s, owner, p := gatedFixture(t)
	ctx := context.Background()
	grantFixture(t, s, p, "first")
	old, e := s.Draft(ctx, "first", "en")
	if e != nil {
		t.Fatal(e)
	}
	in := input()
	in.Kind = "lesson"
	in.CollectionKey = "path"
	in.Position = 1
	in.Slug = "first"
	in.ExpectedVersion = number(old.Version)
	in.Quiz = quizExample()
	in.Quiz.Questions[0].Correct = []string{"b"}
	rev, e := s.Save(ctx, owner, "first", "en", "", in)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ChangePublication(ctx, owner, "first", "en", "", "publish", Action{ExpectedVersion: number(rev.Version), RevisionID: rev.RevisionID}); !errors.Is(e, learning.ErrReview) {
		t.Fatal("active quiz changed without plan", e)
	}
	if e = s.ReviewQuiz(ctx, owner, "first", "en", Action{ExpectedVersion: number(rev.Version), RevisionID: rev.RevisionID}); e != nil {
		t.Fatal(e)
	}
	d, _ := s.Draft(ctx, "first", "en")
	c, _ := s.Collection(ctx, "path")
	plan, e := s.SaveQuizGate(ctx, owner, "first", "en", QuizGateInput{ExpectedVersion: number(c.Version), PlanID: c.DraftPlanID, Enabled: true, AssessmentID: d.Revision.Quiz.AssessmentID})
	if e != nil {
		t.Fatal(e)
	}
	a := Action{ExpectedVersion: number(rev.Version), RevisionID: rev.RevisionID, Plan: &ApplyPlanInput{ExpectedVersion: number(plan.Version - 1), PlanID: plan.PlanID}}
	if _, e = s.ChangePublication(ctx, owner, "first", "en", "", "publish", a); e == nil {
		t.Fatal("stale plan accepted")
	}
	unchanged, _ := s.Draft(ctx, "first", "en")
	if unchanged.PublishedID != old.PublishedID {
		t.Fatal("partial publish")
	}
	a.Plan.ExpectedVersion = number(plan.Version)
	if _, e = s.ChangePublication(ctx, owner, "first", "en", "", "publish", a); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Public(ctx, "en", "second", p); e != nil {
		t.Fatal("ordinary correction erased pass", e)
	}
}

func TestFinalLessonGateProtectsFutureMembers(t *testing.T) {
	s, owner, p := gatedFixture(t)
	ctx := context.Background()
	old, e := s.Draft(ctx, "fourth", "en")
	if e != nil {
		t.Fatal(e)
	}
	in := input()
	in.Kind = "lesson"
	in.CollectionKey = "path"
	in.Position = 4
	in.Slug = "fourth"
	in.ExpectedVersion = number(old.Version)
	in.Quiz = quizExample()
	rev, e := s.Save(ctx, owner, "fourth", "en", "", in)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ReviewQuiz(ctx, owner, "fourth", "en", Action{ExpectedVersion: number(rev.Version), RevisionID: rev.RevisionID}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ChangePublication(ctx, owner, "fourth", "en", "", "publish", Action{ExpectedVersion: number(rev.Version), RevisionID: rev.RevisionID}); e != nil {
		t.Fatal(e)
	}
	draft, _ := s.Draft(ctx, "fourth", "en")
	c, _ := s.Collection(ctx, "path")
	plan, e := s.SaveQuizGate(ctx, owner, "fourth", "en", QuizGateInput{ExpectedVersion: number(c.Version), PlanID: c.DraftPlanID, Enabled: true, AssessmentID: draft.Revision.Quiz.AssessmentID})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ApplyPlan(ctx, "path", ApplyPlanInput{ExpectedVersion: number(plan.Version), PlanID: plan.PlanID}); e != nil {
		t.Fatal(e)
	}
	in.ExpectedVersion = number(0)
	in.Position = 5
	in.Slug = "fifth"
	in.Quiz = nil
	rev, e = s.Save(ctx, owner, "fifth", "en", "", in)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ChangePublication(ctx, owner, "fifth", "en", "", "publish", Action{ExpectedVersion: number(rev.Version), RevisionID: rev.RevisionID}); e != nil {
		t.Fatal(e)
	}
	c, _ = s.Collection(ctx, "path")
	plan, e = s.SavePlan(ctx, owner, "path", PlanInput{ExpectedVersion: number(c.Version), Members: []PlanMember{{Key: "first"}, {Key: "second"}, {Key: "third"}, {Key: "fourth"}, {Key: "fifth"}}})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ApplyPlan(ctx, "path", ApplyPlanInput{ExpectedVersion: number(plan.Version), PlanID: plan.PlanID}); e != nil {
		t.Fatal(e)
	}
	grantFixture(t, s, p, "first")
	grantFixture(t, s, p, "third")
	if _, e = s.Public(ctx, "en", "fifth", p); !errors.Is(e, ErrLocked) {
		t.Fatal("new member bypassed final gate", e)
	}
	grantFixture(t, s, p, "fourth")
	if _, e = s.Public(ctx, "en", "fifth", p); e != nil {
		t.Fatal(e)
	}
}
