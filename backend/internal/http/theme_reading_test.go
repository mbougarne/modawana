package httpserver

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mbougarne/modawana/backend/internal/auth"
	"github.com/mbougarne/modawana/backend/internal/config"
	"github.com/mbougarne/modawana/backend/internal/content"
	"github.com/mbougarne/modawana/backend/internal/learning"
)

func TestThemedReadersPreserveAccessAndLearning(t *testing.T) {
	ctx := t.Context()
	c, owner := newThemeContent(t)
	c.Membership, c.PublicationHook, c.Access.Gates = c.AttachMember, c.CheckPublicationPlan, c.LessonAccess
	zero := int64(0)
	const locale = "en"
	for _, item := range []struct{ key, kind, title string }{
		{"course", "learning", "Published course"}, {"guide", "docs", "Published guide"},
		{"inactive-guide", "docs", "Unpublished outline"},
	} {
		if _, err := c.SaveCollection(ctx, item.key, locale, content.CollectionInput{
			ExpectedVersion: &zero, Kind: item.kind, Slug: item.key, Title: item.title, Visible: true,
		}); err != nil {
			t.Fatal(err)
		}
	}
	quiz := &learning.Definition{PassMark: 1, ShowExplanations: true, Questions: []learning.PrivateQuestion{{
		Question: learning.Question{ID: "q", Text: "Select the answer", Type: "single", Options: []learning.Option{
			{ID: "a", Text: "First option"}, {ID: "b", Text: "Second option"},
		}},
		Correct: []string{"a"}, Explanation: "PRIVATE EXPLANATION",
	}}}
	for _, item := range []struct {
		key, kind, collection, title, body string
		position                           int
		published                          bool
		quiz                               *learning.Definition
	}{
		{"intro", "lesson", "course", "First lesson", "Intro body", 1, true, quiz},
		{"next", "lesson", "course", "Second lesson", "PROTECTED BODY", 2, true, nil},
		{"planned", "lesson", "course", "PRIVATE SCHEDULED TITLE", "PRIVATE FUTURE BODY", 3, false, nil},
		{"hidden", "lesson", "course", "PRIVATE HIDDEN TITLE", "PRIVATE DRAFT BODY", 4, false, nil},
		{"guide-doc", "doc", "guide", "Guide document", "Guide body", 1, true, nil},
		{"standalone", "doc", "inactive-guide", "Standalone document", "Standalone body", 1, true, nil},
		{"interactive", "article", "", "Interactive article", "Article body", 0, true, quiz},
	} {
		saved, err := c.Save(ctx, owner, item.key, locale, "", content.DraftInput{
			ExpectedVersion: &zero, Kind: item.kind, SourceLocale: locale, Slug: item.key, Title: item.title,
			Markdown: item.body, Summary: "Safe summary", CollectionKey: item.collection, Position: item.position, Quiz: item.quiz,
		})
		if err != nil {
			t.Fatal(item.key, err)
		}
		action := content.Action{ExpectedVersion: &saved.Version, RevisionID: saved.RevisionID}
		if item.quiz != nil {
			if err := c.ReviewQuiz(ctx, owner, item.key, locale, action); err != nil {
				t.Fatal(err)
			}
		}
		if item.published {
			if _, err := c.ChangePublication(ctx, owner, item.key, locale, "", "publish", action); err != nil {
				t.Fatal(item.key, err)
			}
		}
	}
	for _, course := range []struct {
		key     string
		members []content.PlanMember
	}{
		{"course", []content.PlanMember{{Key: "intro"}, {Key: "next"}, {Key: "planned", Announcements: map[string]string{"en": "Next month"}}, {Key: "hidden"}}},
		{"guide", []content.PlanMember{{Key: "guide-doc"}}},
	} {
		collection, err := c.Collection(ctx, course.key)
		if err != nil {
			t.Fatal(err)
		}
		plan, err := c.SavePlan(ctx, owner, course.key, content.PlanInput{ExpectedVersion: &collection.Version, Members: course.members})
		if err != nil {
			t.Fatal(err)
		}
		if course.key == "course" {
			draft, err := c.Draft(ctx, "intro", locale)
			if err != nil {
				t.Fatal(err)
			}
			plan, err = c.SaveQuizGate(ctx, owner, "intro", locale, content.QuizGateInput{
				ExpectedVersion: &plan.Version, PlanID: plan.PlanID, AssessmentID: draft.Revision.Quiz.AssessmentID, Enabled: true,
			})
			if err != nil {
				t.Fatal(err)
			}
		}
		if _, err := c.ApplyPlan(ctx, course.key, content.ApplyPlanInput{ExpectedVersion: &plan.Version, PlanID: plan.PlanID}); err != nil {
			t.Fatal(err)
		}
	}
	assets, err := LoadAssets(buildThemeFixture(t, true))
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{BaseURL: "https://example.test"}
	identities := learning.NewIdentity(c.DB, 180, 10)
	service := auth.New(c.DB, cfg)
	domain := &Domain{Content: c, Learning: identities, Auth: service}
	h := New(Options{Assets: assets, Domain: domain, Auth: service, Config: cfg})
	request := func(method, path, token string, data any) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, cfg.BaseURL+path, nil)
		if data != nil {
			raw, err := json.Marshal(data)
			if err != nil {
				t.Fatal(err)
			}
			r = httptest.NewRequest(method, cfg.BaseURL+path, bytes.NewReader(raw))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Origin", cfg.BaseURL)
			r.Header.Set("X-CSRF-Token", learning.LearningCSRF(token))
		}
		if token != "" {
			r.AddCookie(&http.Cookie{Name: learningCookie, Value: token})
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	assertHTML := func(path, token string, status int, contains, absent []string) *httptest.ResponseRecorder {
		t.Helper()
		w := request(http.MethodGet, path, token, nil)
		html := w.Body.String()
		for _, part := range append(contains, `data-rendered="server"`) {
			if !strings.Contains(html, part) {
				t.Fatalf("%s missing %q: %s", path, part, html)
			}
		}
		for _, part := range append(absent, `id="root"`, scriptName, "PRIVATE ") {
			if strings.Contains(html, part) {
				t.Fatalf("%s contains %q", path, part)
			}
		}
		if w.Code != status || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(path, w.Code, w.Header())
		}
		return w
	}
	assertHTML("/en/learn", "", 200, []string{"Published course", readerScript}, nil)
	assertHTML("/en/learn/course", "", 200, []string{"Next month", "0 of 2 complete"}, []string{`href="/en/learn/course/next"`, "PROTECTED BODY"})
	assertHTML("/en/learn/course/intro", "", 200, []string{"Intro body", readerScript, `data-reader-learning`}, []string{"First option", "PROTECTED BODY"})
	assertHTML("/en/articles/interactive", "", 200, []string{"Article body", readerScript}, []string{"First option"})
	for path, body := range map[string]string{
		"/en/docs?doc=standalone": "Standalone body", "/en/doc/standalone": "Standalone body",
		"/en/docs/guide": "Guide document", "/en/docs/guide/guide-doc": "Guide body",
	} {
		assertHTML(path, "", 200, []string{body}, []string{readerScript})
	}
	assertHTML("/en/docs?doc=interactive", "", 200, []string{`role="alert"`}, []string{"Article body"})
	const lockedPath = "/en/learn/course/next"
	locked := assertHTML(lockedPath, "", 403, []string{"Safe summary", `name="robots" content="noindex"`, `href="/en/learn/course/intro"`},
		[]string{"PROTECTED BODY", `data-reader-learning`, `property="og:image"`, `rel="canonical"`})
	if locked.Header().Get("X-Robots-Tag") != "noindex" {
		t.Fatal(locked.Header())
	}
	if head := request(http.MethodHead, lockedPath, "", nil); head.Code != 403 || head.Body.Len() != 0 {
		t.Fatal(head.Code, head.Body.String())
	}
	var count int
	if err := c.DB.QueryRowContext(ctx, "SELECT count(*) FROM learning_identities").Scan(&count); err != nil || count != 0 {
		t.Fatal("reader GET created an identity", count, err)
	}
	token, _, err := identities.Create(ctx, "reading-test")
	if err != nil {
		t.Fatal(err)
	}
	readQuiz := request(http.MethodGet, "/api/v1/learning/content/intro/en/quiz", token, nil)
	var state content.ReaderLearning
	if err := json.Unmarshal(readQuiz.Body.Bytes(), &state); err != nil || readQuiz.Code != 200 || state.Quiz == nil ||
		strings.Contains(readQuiz.Body.String(), "PRIVATE EXPLANATION") {
		t.Fatal(readQuiz.Code, readQuiz.Body.String(), err)
	}
	attempt := request(http.MethodPost, "/api/v1/learning/content/intro/en/attempts", token, learning.Submission{
		Version: state.Version, Answers: map[string][]string{"q": {"a"}}, IdempotencyKey: "stage4-pass",
	})
	if attempt.Code != 200 || !strings.Contains(attempt.Body.String(), `"passed":true`) {
		t.Fatal(attempt.Code, attempt.Body.String())
	}
	assertHTML("/en/learn/course/intro", token, 200, []string{"1 of 2 complete", `href="/en/learn/course/next"`}, nil)
	assertHTML(lockedPath, token, 200, []string{"PROTECTED BODY", `data-reader-learning`}, nil)
	assertHTML(lockedPath, "", 403, []string{"Safe summary"}, []string{"PROTECTED BODY"})
	readState := request(http.MethodGet, "/api/v1/learning/content/next/en/quiz", token, nil)
	if err := json.Unmarshal(readState.Body.Bytes(), &state); err != nil || readState.Code != 200 || !state.CanMarkRead {
		t.Fatal(readState.Code, readState.Body.String(), err)
	}
	recorded := request(http.MethodPost, "/api/v1/learning/content/next/en/read", token, map[string]any{"version": state.Version})
	if recorded.Code != 200 {
		t.Fatal(recorded.Code, recorded.Body.String())
	}
	assertHTML("/en/learn/course", token, 200, []string{"2 of 2 complete"}, nil)
	reset := request(http.MethodDelete, "/api/v1/learning/progress", token, struct{}{})
	if reset.Code != 200 {
		t.Fatal(reset.Code, reset.Body.String())
	}
	assertHTML(lockedPath, token, 403, []string{"Safe summary"}, []string{"PROTECTED BODY"})
	fallback := New(Options{Assets: assets, Domain: domain, Auth: service, Config: config.Config{BaseURL: cfg.BaseURL, ForceSPA: true}})
	if w := get(t, fallback, http.MethodGet, lockedPath); w.Code != 403 || !strings.Contains(w.Body.String(), `id="root"`) || strings.Contains(w.Body.String(), "PROTECTED BODY") {
		t.Fatal("rollback changed the locked response", w.Code)
	}
}
