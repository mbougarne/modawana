package content

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"

	"github.com/mbougarne/modawana/backend/internal/access"
	"github.com/mbougarne/modawana/backend/internal/learning"
	"github.com/mbougarne/modawana/backend/internal/media"
)

func TestMarkdownRejectsNetworkPathAndBackslashLinks(t *testing.T) {
	for _, destination := range []string{"//evil.example/x", `/\evil.example/x`, "/%5cevil.example/x", "/%2f%2fevil.example/x"} {
		in := input()
		in.Markdown = "[link](" + destination + ")"
		if _, e := refs(in); !errors.Is(e, ErrInvalid) {
			t.Fatalf("accepted %q: %v", destination, e)
		}
	}
	for _, destination := range []string{"/en/articles/first", "#section", "https://example.test/path", "mailto:owner@example.test"} {
		in := input()
		in.Markdown = "[link](" + destination + ")"
		if _, e := refs(in); e != nil {
			t.Fatalf("rejected %q: %v", destination, e)
		}
	}
}

func TestImportedHistoryKeepsSourceOrderAcrossTimestampTies(t *testing.T) {
	s, owner := fixture(t)
	ctx := context.Background()
	in := input()
	var version int64
	for i := 0; i < 30; i++ {
		in.ExpectedVersion = number(version)
		in.Title = fmt.Sprintf("Revision %02d", i)
		r, e := s.StagePortableRevision(ctx, owner, "history", "en", in)
		if e != nil {
			t.Fatal(e)
		}
		version = r.Version
	}
	history, _, e := s.History(ctx, "history", "en", "")
	if e != nil || len(history) != 30 {
		t.Fatal(len(history), e)
	}
	for i, entry := range history {
		if entry.Title != fmt.Sprintf("Revision %02d", 29-i) {
			t.Fatalf("history reordered: %+v", history)
		}
	}
	in.ExpectedVersion = number(version)
	in.Title = "After import"
	if _, e = s.Save(ctx, owner, "history", "en", "", in); e != nil {
		t.Fatal(e)
	}
	history, _, e = s.History(ctx, "history", "en", "")
	if e != nil || history[0].Title != "After import" {
		t.Fatal("new edit sorted behind import", history, e)
	}
}

func TestAtomicPlanPublicationStillValidatesTranslatedGateQuiz(t *testing.T) {
	s, owner, _ := gatedFixture(t)
	ctx := context.Background()
	source, e := s.Draft(ctx, "first", "en")
	if e != nil {
		t.Fatal(e)
	}
	in := input()
	in.Kind, in.CollectionKey, in.Position = "lesson", "path", 1
	in.SourceRevisionID = source.PublishedID
	tr, e := s.Save(ctx, owner, "first", "fr", "", in)
	if e != nil {
		t.Fatal(e)
	}
	review, e := s.ChangePublication(ctx, owner, "first", "fr", "", "review", Action{ExpectedVersion: number(tr.Version), RevisionID: tr.RevisionID, SourceRevisionID: source.PublishedID})
	if e != nil {
		t.Fatal(e)
	}
	c, e := s.Collection(ctx, "path")
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.ChangePublication(ctx, owner, "first", "fr", "", "publish", Action{ExpectedVersion: number(review.Version), RevisionID: tr.RevisionID, Plan: &ApplyPlanInput{ExpectedVersion: number(c.Version), PlanID: c.DraftPlanID}})
	if !errors.Is(e, learning.ErrReview) {
		t.Fatal("translation bypassed gate quiz", e)
	}
	unchanged, e := s.Draft(ctx, "first", "fr")
	if e != nil || unchanged.PublishedID != "" || unchanged.Version != review.Version {
		t.Fatal("partial publication", unchanged, e)
	}
	after, e := s.Collection(ctx, "path")
	if e != nil || after.Version != c.Version || after.ActivePlanID != c.ActivePlanID {
		t.Fatal("partial plan activation", after, e)
	}
}

func TestHiddenLearningLocaleDeniesContentQuizCompletionAndMedia(t *testing.T) {
	s, owner, learner := gatedFixture(t)
	ctx := context.Background()
	grantFixture(t, s, learner, "first")
	// Attach a private upload to a new published first-lesson revision.
	m, e := media.New(s.DB, s.Files, s.Access).Upload(ctx, owner, "captions.vtt", "text/vtt", "content", bytes.NewBufferString("WEBVTT\n\n00:00.000 --> 00:01.000\nHello\n"))
	if e != nil {
		t.Fatal(e)
	}
	old, e := s.Draft(ctx, "first", "en")
	if e != nil {
		t.Fatal(e)
	}
	in := input()
	in.Kind, in.CollectionKey, in.Position = "lesson", "path", 1
	in.ExpectedVersion, in.Quiz = number(old.Version), old.Revision.Quiz
	in.Markdown = "[captions](/media/" + m.ID + ")"
	r, e := s.Save(ctx, owner, "first", "en", "", in)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ReviewQuiz(ctx, owner, "first", "en", Action{ExpectedVersion: number(r.Version), RevisionID: r.RevisionID}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ChangePublication(ctx, owner, "first", "en", "", "publish", Action{ExpectedVersion: number(r.Version), RevisionID: r.RevisionID}); e != nil {
		t.Fatal(e)
	}
	if d, e := s.Access.Resolve(ctx, access.Request{Principal: learner, Surface: access.Media, ResourceID: m.ID}); e != nil || !d.AllowsBody() {
		t.Fatal("fixture media closed", d, e)
	}
	state, e := s.ReaderQuiz(ctx, "second", "en", learner)
	if e != nil {
		t.Fatal(e)
	}
	quiz, e := s.ReaderQuiz(ctx, "first", "en", learner)
	if e != nil {
		t.Fatal(e)
	}
	attempt := learning.Submission{Version: quiz.Version, Answers: map[string][]string{"q": {"a"}}, IdempotencyKey: "hidden-attempt-key"}
	if _, e = s.SubmitQuiz(ctx, "first", "en", learner, attempt); e != nil {
		t.Fatal(e)
	}
	c, e := s.Collection(ctx, "path")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.SaveCollection(ctx, "path", "en", CollectionInput{ExpectedVersion: number(c.Version), Kind: "learning", Slug: "path", Title: "Path", Visible: false}); e != nil {
		t.Fatal(e)
	}
	for _, p := range []access.Principal{{}, learner} {
		if _, e = s.Public(ctx, "en", "first", p); !errors.Is(e, sql.ErrNoRows) {
			t.Fatal("hidden body", e)
		}
		if _, e = s.ReaderQuiz(ctx, "first", "en", p); !errors.Is(e, sql.ErrNoRows) {
			t.Fatal("hidden quiz", e)
		}
		if d, e := s.Access.Resolve(ctx, access.Request{Principal: p, Surface: access.Media, ResourceID: m.ID}); e != nil || d.AllowsBody() {
			t.Fatal("hidden media", d, e)
		}
		page, e := s.Discover(ctx, Filter{Locale: "en"}, p)
		if e != nil || page.Total != 0 || len(page.Items) != 0 {
			t.Fatal("hidden discovery", page, e)
		}
	}
	if _, e = s.MarkRead(ctx, "second", "en", learner, state.Version); !errors.Is(e, sql.ErrNoRows) {
		t.Fatal("hidden completion", e)
	}
	if _, e = s.SubmitQuiz(ctx, "first", "en", learner, attempt); !errors.Is(e, sql.ErrNoRows) {
		t.Fatal("hidden submission", e)
	}
}

func TestPublishedDocBookmarksSurviveGuideVisibilityAndPlanChanges(t *testing.T) {
	s, owner := fixture(t)
	ctx := context.Background()
	s.Membership, s.PublicationHook = s.AttachMember, s.CheckPublicationPlan
	if _, e := s.SaveCollection(ctx, "guide", "en", CollectionInput{ExpectedVersion: number(0), Kind: "docs", Slug: "guide", Title: "Guide", Visible: true}); e != nil {
		t.Fatal(e)
	}
	in := input()
	in.Kind, in.CollectionKey, in.Position = "doc", "guide", 1
	r, e := s.Save(ctx, owner, "doc", "en", "", in)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ChangePublication(ctx, owner, "doc", "en", "", "publish", Action{ExpectedVersion: number(r.Version), RevisionID: r.RevisionID}); e != nil {
		t.Fatal(e)
	}
	if page, e := s.ResolvePage(ctx, "/en/doc/first"); e != nil || page.URL != "/en/doc/first" {
		t.Fatal(page, e)
	}
	c, e := s.Collection(ctx, "guide")
	if e != nil {
		t.Fatal(e)
	}
	plan, e := s.SavePlan(ctx, owner, "guide", PlanInput{ExpectedVersion: number(c.Version), Members: []PlanMember{{Key: "doc"}}})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ApplyPlan(ctx, "guide", ApplyPlanInput{ExpectedVersion: number(plan.Version), PlanID: plan.PlanID}); e != nil {
		t.Fatal(e)
	}
	if page, e := s.ResolvePage(ctx, "/en/doc/first"); e != nil || page.URL != "/en/docs/guide/first" {
		t.Fatal("standalone bookmark lost", page, e)
	}
	c, e = s.Collection(ctx, "guide")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.SaveCollection(ctx, "guide", "en", CollectionInput{ExpectedVersion: number(c.Version), Kind: "docs", Slug: "guide", Title: "Guide", Visible: false}); e != nil {
		t.Fatal(e)
	}
	if page, e := s.ResolvePage(ctx, "/en/docs/guide/first"); e != nil || page.URL != "/en/doc/first" {
		t.Fatal("scoped bookmark lost", page, e)
	}
	if _, e = s.ResolvePage(ctx, "/en/docs/wrong/first"); !errors.Is(e, sql.ErrNoRows) {
		t.Fatal("unrelated guide accepted", e)
	}
}

func TestPublishedLessonRequiresAppliedMembership(t *testing.T) {
	s, owner, learner := gatedFixture(t)
	ctx := context.Background()
	grantFixture(t, s, learner, "first")
	grantFixture(t, s, learner, "third")
	in := input()
	in.Kind, in.CollectionKey, in.Position, in.Slug = "lesson", "path", 5, "fifth"
	draft, err := s.Save(ctx, owner, "fifth", "en", "", in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ChangePublication(ctx, owner, "fifth", "en", "", "publish", Action{ExpectedVersion: number(draft.Version), RevisionID: draft.RevisionID}); err != nil {
		t.Fatal(err)
	}
	c, err := s.Collection(ctx, "path")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := s.SavePlan(ctx, owner, "path", PlanInput{ExpectedVersion: number(c.Version), Members: []PlanMember{{Key: "first"}, {Key: "second"}, {Key: "third"}, {Key: "fourth"}, {Key: "fifth"}}})
	if err != nil {
		t.Fatal(err)
	}
	out, err := s.Public(ctx, "en", "fifth", learner)
	if !errors.Is(err, sql.ErrNoRows) || out.Markdown != "" || len(out.Media) != 0 || out.Title != "" {
		t.Fatal("draft-only plan membership exposed lesson", out, err)
	}
	if _, err = s.ApplyPlan(ctx, "path", ApplyPlanInput{ExpectedVersion: number(plan.Version), PlanID: plan.PlanID}); err != nil {
		t.Fatal(err)
	}
	if out, err = s.Public(ctx, "en", "fifth", learner); err != nil || out.Markdown != in.Markdown {
		t.Fatal("applied membership did not expose earned lesson", out, err)
	}
}
