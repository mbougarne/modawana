package content

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/mbougarne/modawana/backend/internal/access"
	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/learning"
	"strings"
	"testing"
	"time"
)

func quizExample() *learning.Definition {
	return &learning.Definition{PassMark: 1, Questions: []learning.PrivateQuestion{{Question: learning.Question{ID: "q", Type: "single", Text: "Question", Options: []learning.Option{{ID: "a", Text: "A"}, {ID: "b", Text: "B"}}}, Correct: []string{"a"}, Explanation: "Private answer"}}}
}
func TestQuizBindingsPublicationTranslationAndRestore(t *testing.T) {
	s, o := fixture(t)
	ctx := context.Background()
	in := input()
	in.Quiz = quizExample()
	a, e := s.Save(ctx, o, "quiz", "en", "", in)
	if e != nil {
		t.Fatal(e)
	}
	d, e := s.Draft(ctx, "quiz", "en")
	if e != nil || d.Revision.Quiz == nil {
		t.Fatal(d, e)
	}
	quizID := d.Revision.Quiz.AssessmentID
	p, e := s.ChangePublication(ctx, o, "quiz", "en", "", "publish", Action{ExpectedVersion: number(a.Version), RevisionID: a.RevisionID})
	if e != nil {
		t.Fatal(e)
	}
	public, e := s.Public(ctx, "en", "first", access.Principal{})
	b, _ := json.Marshal(public)
	if e != nil || strings.Contains(string(b), "Private answer") || strings.Contains(string(b), "correct") {
		t.Fatal(string(b), e)
	}
	in.ExpectedVersion = number(p.Version)
	in.Quiz.Questions[0].Correct = []string{"b"}
	draft, e := s.Save(ctx, o, "quiz", "en", "", in)
	if e != nil {
		t.Fatal(e)
	}
	old, e := learning.Read(ctx, s.DB, a.RevisionID)
	if e != nil || old.AssessmentID != quizID || old.Questions[0].Correct[0] != "a" {
		t.Fatal(old, e)
	}
	translated := input()
	translated.Slug = "francais"
	translated.SourceRevisionID = a.RevisionID
	translated.Quiz = old
	translated.Quiz.Questions[0].Text = "Question française"
	tr, e := s.Save(ctx, o, "quiz", "fr", "", translated)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ChangePublication(ctx, o, "quiz", "fr", "", "publish", Action{ExpectedVersion: number(tr.Version), RevisionID: tr.RevisionID}); !errors.Is(e, ErrReview) {
		t.Fatal(e)
	}
	review, e := s.ChangePublication(ctx, o, "quiz", "fr", "", "review", Action{ExpectedVersion: number(tr.Version), RevisionID: tr.RevisionID, SourceRevisionID: a.RevisionID})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ChangePublication(ctx, o, "quiz", "fr", "", "publish", Action{ExpectedVersion: number(review.Version), RevisionID: tr.RevisionID}); e != nil {
		t.Fatal(e)
	}
	translated.ExpectedVersion = number(3)
	translated.Quiz.PassMark = 2
	if _, e = s.Save(ctx, o, "quiz", "fr", "", translated); e == nil {
		t.Fatal("divergent threshold accepted")
	}
	restored, e := s.Restore(ctx, o, "quiz", "en", "", Action{ExpectedVersion: number(draft.Version), RevisionID: a.RevisionID})
	if e != nil {
		t.Fatal(e)
	}
	restoredQuiz, e := learning.Read(ctx, s.DB, restored.RevisionID)
	if e != nil || restoredQuiz.AssessmentID != quizID {
		t.Fatal(restoredQuiz, e)
	}
	result, e := s.PreviewQuiz(ctx, "quiz", "en", learning.Submission{Version: learning.Version{Quiz: quizID, Content: restored.RevisionID}, Answers: map[string][]string{"q": {"a"}}})
	if e != nil || !result.Passed {
		t.Fatal(result, e)
	}
	if _, e = s.DB.Exec("UPDATE assessments SET definition_json='{}' WHERE id=?", quizID); e == nil {
		t.Fatal("mutable assessment")
	}
}

func TestQuizGateDraftIsolationExactReviewAndConflict(t *testing.T) {
	s, o := fixture(t)
	ctx := context.Background()
	s.Membership = s.AttachMember
	s.PublicationHook = s.CheckPublicationPlan
	s.Access.Gates = s.LessonAccess
	_, e := s.SaveCollection(ctx, "gate-course", "en", CollectionInput{ExpectedVersion: number(0), Kind: "learning", Slug: "gate-course", Title: "Gate course", Visible: true})
	if e != nil {
		t.Fatal(e)
	}
	var source Result
	for i, key := range []string{"gate-source", "gate-target"} {
		in := input()
		in.Kind = "lesson"
		in.CollectionKey = "gate-course"
		in.Position = i + 1
		in.Slug = key
		if i == 0 {
			in.Quiz = quizExample()
		}
		r, e := s.Save(ctx, o, key, "en", "", in)
		if e != nil {
			t.Fatal(e)
		}
		if i == 0 {
			source = r
		}
	}
	c, e := s.Collection(ctx, "gate-course")
	if e != nil {
		t.Fatal(e)
	}
	p, e := s.SavePlan(ctx, o, "gate-course", PlanInput{ExpectedVersion: number(c.Version), Members: []PlanMember{{Key: "gate-source"}, {Key: "gate-target"}}})
	if e != nil {
		t.Fatal(e)
	}
	active, e := s.ApplyPlan(ctx, "gate-course", ApplyPlanInput{ExpectedVersion: number(p.Version), PlanID: p.PlanID})
	if e != nil {
		t.Fatal(e)
	}
	draft, e := s.Draft(ctx, "gate-source", "en")
	if e != nil {
		t.Fatal(e)
	}
	in := QuizGateInput{ExpectedVersion: number(active.Version), PlanID: p.PlanID, AssessmentID: draft.Revision.Quiz.AssessmentID, Enabled: true}
	if _, e = s.SaveQuizGate(ctx, o, "gate-source", "en", in); !errors.Is(e, learning.ErrReview) {
		t.Fatal(e)
	}
	if e = s.ReviewQuiz(ctx, o, "gate-source", "en", Action{ExpectedVersion: number(source.Version), RevisionID: source.RevisionID}); e != nil {
		t.Fatal(e)
	}
	gate, e := s.SaveQuizGate(ctx, o, "gate-source", "en", in)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.SaveQuizGate(ctx, o, "gate-source", "en", in); e == nil {
		t.Fatal("stale plan overwritten")
	}
	c, e = s.Collection(ctx, "gate-course")
	if e != nil || c.ActivePlanID != p.PlanID || c.DraftPlanID != gate.PlanID {
		t.Fatal(c, e)
	}
	if _, e = s.ApplyPlan(ctx, "gate-course", ApplyPlanInput{ExpectedVersion: number(gate.Version), PlanID: gate.PlanID}); e == nil {
		t.Fatal("unsupported gate activated")
	}
	removed, e := s.SaveQuizGate(ctx, o, "gate-source", "en", QuizGateInput{ExpectedVersion: number(gate.Version), PlanID: gate.PlanID, Enabled: false})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ApplyPlan(ctx, "gate-course", ApplyPlanInput{ExpectedVersion: number(removed.Version), PlanID: removed.PlanID}); e != nil {
		t.Fatal(e)
	}
}

func TestLearningAttemptRetentionAndResetCascade(t *testing.T) {
	s, o := fixture(t)
	ctx := context.Background()
	in := input()
	in.Quiz = quizExample()
	revision, e := s.Save(ctx, o, "retention", "en", "", in)
	if e != nil {
		t.Fatal(e)
	}
	definition, e := learning.Read(ctx, s.DB, revision.RevisionID)
	if e != nil {
		t.Fatal(e)
	}
	identities := learning.NewIdentity(s.DB, 180, 10)
	raw, identity, e := identities.Create(ctx, "test")
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 105; i++ {
		id, _ := db.NewID()
		hash := fmt.Sprintf("%064d", i)
		_, e = s.DB.Exec(`INSERT INTO learning_attempts(id,identity_id,assessment_id,revision_id,score,total,passed,feedback_json,idempotency_hash,request_hash,created_at) VALUES(?,?,?,?,1,1,1,'{}',?,?,?)`, id, identity.ID, definition.AssessmentID, revision.RevisionID, hash, hash, db.UTC(time.Now().Add(time.Duration(i)*time.Millisecond)))
		if e != nil {
			t.Fatal(e)
		}
	}
	var count int
	s.DB.QueryRow("SELECT count(*) FROM learning_attempts").Scan(&count)
	if count != 100 {
		t.Fatal(count)
	}
	if _, e = s.DB.Exec("UPDATE learning_attempts SET created_at=?", db.UTC(time.Now().Add(-31*24*time.Hour))); e != nil {
		t.Fatal(e)
	}
	if _, e = identities.Cleanup(ctx); e != nil {
		t.Fatal(e)
	}
	s.DB.QueryRow("SELECT count(*) FROM learning_attempts").Scan(&count)
	if count != 0 {
		t.Fatal(count)
	}
	if e = identities.Reset(ctx, raw); e != nil {
		t.Fatal(e)
	}
	s.DB.QueryRow("SELECT count(*) FROM learning_identities").Scan(&count)
	if count != 0 {
		t.Fatal(count)
	}
}
