package content

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/mbougarne/modawana/backend/internal/access"
	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/learning"
	"sync"
	"testing"
)

func TestAtomicGradingReplayCompletionAndRead(t *testing.T) {
	s, _, p := gatedFixture(t)
	ctx := context.Background()
	q, e := s.ReaderQuiz(ctx, "first", "en", p)
	if e != nil || q.Quiz == nil {
		t.Fatal(q, e)
	}
	in := learning.Submission{Version: q.Version, Answers: map[string][]string{"q": {"b"}}, IdempotencyKey: "fail"}
	r, e := s.SubmitQuiz(ctx, "first", "en", p, in)
	if e != nil || r.Passed || r.Completed {
		t.Fatal(r, e)
	}
	if _, e = s.MarkRead(ctx, "first", "en", p, q.Version); !errors.Is(e, ErrInvalid) {
		t.Fatal("quiz bypass", e)
	}
	if _, e = s.MarkRead(ctx, "fourth", "en", p, q.Version); !errors.Is(e, ErrLocked) {
		t.Fatal("locked read bypass", e)
	}
	in.Answers["q"] = []string{"a"}
	if _, e = s.SubmitQuiz(ctx, "first", "en", p, in); !errors.Is(e, ErrIdempotency) {
		t.Fatal("changed replay", e)
	}
	in.IdempotencyKey = "pass"
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, e := s.SubmitQuiz(ctx, "first", "en", p, in)
			if e == nil && (!r.Passed || !r.Completed || r.UnlockedHref != "/en/learn/path/second") {
				e = errors.New("wrong result")
			}
			errs <- e
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	var n int
	s.DB.QueryRow("SELECT count(*) FROM learning_attempts").Scan(&n)
	if n != 2 {
		t.Fatal("duplicate attempt", n)
	}
	s.DB.QueryRow("SELECT count(*) FROM learning_completion").Scan(&n)
	if n != 1 {
		t.Fatal("duplicate pass", n)
	}
	q, e = s.ReaderQuiz(ctx, "second", "en", p)
	if e != nil || !q.CanMarkRead {
		t.Fatal(q, e)
	}
	q, e = s.MarkRead(ctx, "second", "en", p, q.Version)
	if e != nil || !q.Completed {
		t.Fatal(q, e)
	}
	if _, e = s.Public(ctx, "en", "fourth", p); !errors.Is(e, ErrLocked) {
		t.Fatal("later gate skipped", e)
	}
	in.IdempotencyKey = "stale"
	in.Version.Path = "old"
	if _, e = s.SubmitQuiz(ctx, "first", "en", p, in); !errors.Is(e, db.ErrVersionConflict) {
		t.Fatal(e)
	}
	if _, e = s.SubmitQuiz(ctx, "first", "en", access.Principal{}, in); !errors.Is(e, learning.ErrIdentity) {
		t.Fatal(e)
	}
	if _, e = s.DB.Exec("DELETE FROM learning_attempts"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Public(ctx, "en", "third", p); e != nil {
		t.Fatal("retention erased pass", e)
	}
}
func TestOutdatedTranslationCannotGrantPass(t *testing.T) {
	s, o := fixture(t)
	ctx := context.Background()
	in := input()
	in.Quiz = quizExample()
	a, e := s.Save(ctx, o, "quiz", "en", "", in)
	if e != nil {
		t.Fatal(e)
	}
	pub, e := s.ChangePublication(ctx, o, "quiz", "en", "", "publish", Action{ExpectedVersion: number(a.Version), RevisionID: a.RevisionID})
	if e != nil {
		t.Fatal(e)
	}
	tr := in
	tr.Slug = "francais"
	tr.SourceRevisionID = a.RevisionID
	tr.Quiz, e = learning.Read(ctx, s.DB, a.RevisionID)
	if e != nil {
		t.Fatal(e)
	}
	b, e := s.Save(ctx, o, "quiz", "fr", "", tr)
	if e != nil {
		t.Fatal(e)
	}
	review, e := s.ChangePublication(ctx, o, "quiz", "fr", "", "review", Action{ExpectedVersion: number(b.Version), RevisionID: b.RevisionID, SourceRevisionID: a.RevisionID})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ChangePublication(ctx, o, "quiz", "fr", "", "publish", Action{ExpectedVersion: number(review.Version), RevisionID: b.RevisionID}); e != nil {
		t.Fatal(e)
	}
	q, e := s.ReaderQuiz(ctx, "quiz", "fr", access.Principal{})
	if e != nil || q.Quiz == nil {
		t.Fatal(q, e)
	}
	in.ExpectedVersion = number(pub.Version)
	in.Quiz.Questions[0].Correct = []string{"b"}
	changed, e := s.Save(ctx, o, "quiz", "en", "", in)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ChangePublication(ctx, o, "quiz", "en", "", "publish", Action{ExpectedVersion: number(changed.Version), RevisionID: changed.RevisionID}); e != nil {
		t.Fatal(e)
	}
	q, e = s.ReaderQuiz(ctx, "quiz", "fr", access.Principal{})
	if e != nil || q.Quiz != nil || !q.Unavailable {
		t.Fatal("stale quiz exposed", q, e)
	}
}

func TestPassSharedAcrossLocalesAndRetryRate(t *testing.T) {
	s, owner, p := gatedFixture(t)
	ctx := context.Background()
	source, _ := s.Draft(ctx, "second", "en")
	in := input()
	in.Kind = "lesson"
	in.CollectionKey = "path"
	in.Position = 2
	in.Slug = "second-ar"
	in.SourceRevisionID = source.PublishedID
	ar, e := s.Save(ctx, owner, "second", "ar", "", in)
	if e != nil {
		t.Fatal(e)
	}
	review, e := s.ChangePublication(ctx, owner, "second", "ar", "", "review", Action{ExpectedVersion: number(ar.Version), RevisionID: ar.RevisionID, SourceRevisionID: source.PublishedID})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ChangePublication(ctx, owner, "second", "ar", "", "publish", Action{ExpectedVersion: number(review.Version), RevisionID: ar.RevisionID}); e != nil {
		t.Fatal(e)
	}
	// Publication alone does not expose an absent path locale.
	if _, e = s.Public(ctx, "ar", "second-ar", p); !errors.Is(e, sql.ErrNoRows) {
		t.Fatal("lesson exposed without a visible path locale", e)
	}
	c, e := s.Collection(ctx, "path")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.SaveCollection(ctx, "path", "ar", CollectionInput{ExpectedVersion: number(c.Version), Kind: "learning", Slug: "path-ar", Title: "المسار", Visible: true}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Public(ctx, "ar", "second-ar", p); !errors.Is(e, ErrLocked) {
		t.Fatal(e)
	}
	q, _ := s.ReaderQuiz(ctx, "first", "en", p)
	for i := 0; i < 10; i++ {
		_, e = s.SubmitQuiz(ctx, "first", "en", p, learning.Submission{Version: q.Version, Answers: map[string][]string{"q": {"a"}}, IdempotencyKey: fmt.Sprint(i)})
		if e != nil {
			t.Fatal(e)
		}
	}
	if _, e = s.SubmitQuiz(ctx, "first", "en", p, learning.Submission{Version: q.Version, Answers: map[string][]string{"q": {"a"}}, IdempotencyKey: "eleven"}); !errors.Is(e, learning.ErrLimited) {
		t.Fatal(e)
	}
	if _, e = s.Public(ctx, "ar", "second-ar", p); e != nil {
		t.Fatal("locale pass lost", e)
	}
}
