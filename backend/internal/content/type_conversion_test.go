package content

import (
	"context"
	"errors"
	"github.com/mbougarne/modawana/backend/internal/access"
	"github.com/mbougarne/modawana/backend/internal/db"
	"sync"
	"testing"
)

func conversionCollection(t *testing.T, s *Service, key, kind string) CollectionResult {
	t.Helper()
	c, e := s.SaveCollection(context.Background(), key, "en", CollectionInput{ExpectedVersion: number(0), Kind: kind, Slug: key, Title: key, Visible: true})
	if e != nil {
		t.Fatal(e)
	}
	return c
}
func TestPublishedTypeConversionRetainsAllLocalesAndHistory(t *testing.T) {
	s, owner := fixture(t)
	ctx := context.Background()
	s.Membership = s.AttachMember
	s.PublicationHook = s.CheckPublicationPlan
	s.Access.Gates = s.LessonAccess
	c := conversionCollection(t, s, "course", "learning")
	in := input()
	r, e := s.Save(ctx, owner, "first", "en", "", in)
	if e != nil {
		t.Fatal(e)
	}
	pub, e := s.ChangePublication(ctx, owner, "first", "en", "", "publish", Action{ExpectedVersion: number(r.Version), RevisionID: r.RevisionID})
	if e != nil {
		t.Fatal(e)
	}
	translated := input()
	translated.SourceRevisionID = r.RevisionID
	translated.Slug = "premier"
	translated.Title = "Premier"
	if _, e = s.Save(ctx, owner, "first", "fr", "", translated); e != nil {
		t.Fatal(e)
	}
	before, _ := s.Draft(ctx, "first", "en")
	beforeFR, _ := s.Draft(ctx, "first", "fr")
	change := TypeChange{ExpectedVersion: number(pub.Version), FromKind: "article", Kind: "lesson", CollectionKey: "course", ExpectedCollectionVersion: number(c.Version)}
	converted, e := s.ChangeType(ctx, owner, "first", "en", "convert-first", change)
	if e != nil {
		t.Fatal(e)
	}
	again, e := s.ChangeType(ctx, owner, "first", "en", "convert-first", change)
	if e != nil || again != converted {
		t.Fatal("retry duplicated conversion", again, e)
	}
	after, _ := s.Draft(ctx, "first", "en")
	afterFR, _ := s.Draft(ctx, "first", "fr")
	if after.Kind != "lesson" || after.Key != before.Key || after.PublishedID != before.PublishedID || after.Revision.ID != before.Revision.ID || after.Revision.Markdown != before.Revision.Markdown || after.Version != before.Version+1 || afterFR.Kind != "lesson" || afterFR.Version != beforeFR.Version+1 || afterFR.Revision.ID != beforeFR.Revision.ID {
		t.Fatal("conversion lost content", after)
	}
	history, _, e := s.History(ctx, "first", "en", "")
	if e != nil || len(history) != 1 || history[0].ID != r.RevisionID {
		t.Fatal("history changed", e)
	}
	if page, e := s.ResolvePage(ctx, "/en/articles/first"); e == nil && page.Content != nil && page.Content.Markdown != "" {
		t.Fatal("unplanned lesson leaked")
	}
	collection, _ := s.Collection(ctx, "course")
	plan, e := s.SavePlan(ctx, owner, "course", PlanInput{ExpectedVersion: number(collection.Version), Members: []PlanMember{{Key: "first"}}})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ApplyPlan(ctx, "course", ApplyPlanInput{ExpectedVersion: number(plan.Version), PlanID: plan.PlanID}); e != nil {
		t.Fatal(e)
	}
	page, e := s.ResolvePage(ctx, "/en/articles/first")
	if e != nil || page.URL != "/en/learn/course/first" || page.Content.Markdown != in.Markdown {
		t.Fatal("legacy link failed", page, e)
	}
	_, e = s.ChangeType(ctx, owner, "first", "en", "", TypeChange{ExpectedVersion: number(after.Version), FromKind: "lesson", Kind: "article"})
	var issue *TypeChangeError
	if !errors.As(e, &issue) || issue.Code != "conversion_plan" {
		t.Fatal("active plan converted", e)
	}
	if _, e = s.DB.Exec("UPDATE content_items SET kind='article' WHERE key='first'"); e == nil {
		t.Fatal("unguarded SQL changed kind")
	}
	var guards, events int
	s.DB.QueryRow("SELECT count(*) FROM content_type_conversion_guard").Scan(&guards)
	s.DB.QueryRow("SELECT count(*) FROM content_type_events").Scan(&events)
	if guards != 0 || events != 1 {
		t.Fatal("guard/audit incorrect", guards, events)
	}
}
func TestTypeConversionRollbackAndConcurrentVersions(t *testing.T) {
	s, owner := fixture(t)
	ctx := context.Background()
	c := conversionCollection(t, s, "course", "learning")
	r, e := s.Save(ctx, owner, "first", "en", "", input())
	if e != nil {
		t.Fatal(e)
	}
	bad := TypeChange{ExpectedVersion: number(r.Version), FromKind: "article", Kind: "doc", CollectionKey: "course", ExpectedCollectionVersion: number(c.Version)}
	if _, e = s.ChangeType(ctx, owner, "first", "en", "", bad); e == nil {
		t.Fatal("wrong collection accepted")
	}
	before, _ := s.Draft(ctx, "first", "en")
	if before.Kind != "article" || before.Version != r.Version {
		t.Fatal("failed conversion mutated item")
	}
	good := bad
	good.Kind = "lesson"
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := s.ChangeType(ctx, owner, "first", "en", "", good); results <- e }()
	}
	wg.Wait()
	close(results)
	successes, conflicts := 0, 0
	for e := range results {
		if e == nil {
			successes++
		} else if errors.Is(e, db.ErrVersionConflict) {
			conflicts++
		} else {
			t.Fatal(e)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatal("race", successes, conflicts)
	}
	var guards int
	s.DB.QueryRow("SELECT count(*) FROM content_type_conversion_guard").Scan(&guards)
	if guards != 0 {
		t.Fatal("guard escaped transaction")
	}
}
func TestDocumentConversionRejectsQuizAndSupportsUnplannedReverse(t *testing.T) {
	s, owner := fixture(t)
	ctx := context.Background()
	s.Membership = s.AttachMember
	guide := conversionCollection(t, s, "guide", "docs")
	in := input()
	in.Quiz = quizExample()
	r, e := s.Save(ctx, owner, "quiz", "en", "", in)
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.ChangeType(ctx, owner, "quiz", "en", "", TypeChange{ExpectedVersion: number(r.Version), FromKind: "article", Kind: "doc", CollectionKey: "guide", ExpectedCollectionVersion: number(guide.Version)})
	var issue *TypeChangeError
	if !errors.As(e, &issue) || issue.Code != "conversion_quiz" {
		t.Fatal("quiz removed", e)
	}
	in = input()
	in.Slug = "plain"
	r, e = s.Save(ctx, owner, "plain", "en", "", in)
	if e != nil {
		t.Fatal(e)
	}
	changed, e := s.ChangeType(ctx, owner, "plain", "en", "", TypeChange{ExpectedVersion: number(r.Version), FromKind: "article", Kind: "doc", CollectionKey: "guide", ExpectedCollectionVersion: number(guide.Version)})
	if e != nil {
		t.Fatal(e)
	}

	published, e := s.ChangePublication(ctx, owner, "plain", "en", "", "publish", Action{ExpectedVersion: number(changed.Version), RevisionID: r.RevisionID})
	if e != nil {
		t.Fatal(e)
	}
	changed.Version = published.Version
	profile, _ := db.NewID()
	profileRevision, _ := db.NewID()
	if _, e = s.DB.Exec("INSERT INTO config_documents(id,kind) VALUES(?,'profile')", profile); e != nil {
		t.Fatal(e)
	}
	if _, e = s.DB.Exec("INSERT INTO config_revisions(id,document_id,snapshot_json,schema_version,colour_algorithm_version,created_by) VALUES(?,?,?,1,1,?)", profileRevision, profile, `{"about_key":"plain"}`, owner); e != nil {
		t.Fatal(e)
	}
	if _, e = s.DB.Exec("UPDATE config_documents SET applied_revision_id=? WHERE id=?", profileRevision, profile); e != nil {
		t.Fatal(e)
	}
	_, e = s.ChangeType(ctx, owner, "plain", "en", "", TypeChange{ExpectedVersion: number(changed.Version), FromKind: "doc", Kind: "article"})
	if !errors.As(e, &issue) || issue.Code != "conversion_about" {
		t.Fatal("selected About doc converted", e)
	}
	if _, e = s.DB.Exec("UPDATE config_documents SET applied_revision_id=NULL WHERE id=?", profile); e != nil {
		t.Fatal(e)
	}
	back, e := s.ChangeType(ctx, owner, "plain", "en", "", TypeChange{ExpectedVersion: number(changed.Version), FromKind: "doc", Kind: "article"})
	if e != nil {
		t.Fatal(e)
	}
	d, _ := s.Draft(ctx, "plain", "en")
	if d.Kind != "article" || d.Revision.ID != r.RevisionID || back.Version != r.Version+3 {
		t.Fatal("reverse lost draft")
	}
}
func TestConvertedArticleAliasCannotBypassLearningGates(t *testing.T) {
	s, owner, reader := gatedFixture(t)
	ctx := access.WithPrincipal(context.Background(), reader)
	in := input()
	in.Slug = "extra"
	r, e := s.Save(ctx, owner, "extra", "en", "", in)
	if e != nil {
		t.Fatal(e)
	}
	pub, e := s.ChangePublication(ctx, owner, "extra", "en", "", "publish", Action{ExpectedVersion: number(r.Version), RevisionID: r.RevisionID})
	if e != nil {
		t.Fatal(e)
	}
	c, _ := s.Collection(ctx, "path")
	_, e = s.ChangeType(ctx, owner, "extra", "en", "", TypeChange{ExpectedVersion: number(pub.Version), FromKind: "article", Kind: "lesson", CollectionKey: "path", ExpectedCollectionVersion: number(c.Version)})
	if e != nil {
		t.Fatal(e)
	}
	c, _ = s.Collection(ctx, "path")
	members := append(c.DraftMembers, PlanMember{Key: "extra"})
	p, e := s.SavePlan(ctx, owner, "path", PlanInput{ExpectedVersion: number(c.Version), Members: members})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ApplyPlan(ctx, "path", ApplyPlanInput{ExpectedVersion: number(p.Version), PlanID: p.PlanID}); e != nil {
		t.Fatal(e)
	}
	page, e := s.ResolvePage(ctx, "/en/articles/extra")
	if !errors.Is(e, ErrLocked) || page.Content == nil || page.Content.Markdown != "" || len(page.Content.Media) != 0 {
		t.Fatal("alias bypassed gates", page, e)
	}
}
